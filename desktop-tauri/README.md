# AgentRQ desktop on Tauri (preview)

Electron is still the default desktop app. This is the Tauri build beside it,
with the same UI: its renderer is `desktop/src/renderer`, built from
`frontend/src`, and a `window.agentrq` bridge of its own stands in for
Electron's preload. The rules of the code are in
[`docs/agents/desktop-tauri.md`](../docs/agents/desktop-tauri.md); what is
checked on which OS is in
[the parity checklist](../docs/agents/desktop-tauri-parity.md); the user's view
is the "Preview: Tauri build" section of [`docs/DESKTOP.md`](../docs/DESKTOP.md#preview-tauri-build).

## Layout

| Folder | What it holds |
| --- | --- |
| `core/` | `agentrq-desktop-core`: the pure logic, ported from `desktop/src/main` with its tests. No Tauri dependency. |
| `src-tauri/` | The app: commands, the `app` scheme and its proxy, profiles, the side panel, the menu and the shell. It depends on `core/` and does the wiring. |
| `src/bridge/` | `window.agentrq` for the main webview, and the `fetch` and `EventSource` the `app` scheme needs. |
| `src/guest/` | The side-panel guest's init script. |
| `src/renderer/` | The renderer entry; everything else comes from `desktop/src/renderer` and `frontend/src`. |
| `test/` | Vitest: the bridge, its parity with Electron's preload, the guest and the screenshot pixel check. |
| `scripts/` | The verify scripts (below) and the icon generator. |

## Setting up

### Prerequisites per OS

- **Linux**: install the webview and build packages matching `.github/workflows/desktop-tauri.yml`:
  ```sh
  sudo apt-get install -y libwebkit2gtk-4.1-dev libayatana-appindicator3-dev librsvg2-dev libxdo-dev webkit2gtk-driver xvfb xclip dbus-x11 python3-gi
  ```
  The first four (`libwebkit2gtk-4.1-dev libayatana-appindicator3-dev librsvg2-dev libxdo-dev`) are required to build and run the app. `webkit2gtk-driver` (`WebKitWebDriver`), `xvfb`, `xclip`, `dbus-x11`, and `python3-gi` are required to run the headless verify scripts under `tauri-driver`.
- **macOS**: Xcode Command Line Tools (`xcode-select --install`).
- **Windows**: WebView2 runtime (pre-installed on Windows 10 and 11) and MSVC C++ Build Tools.
- **Node.js and Rust**: Node 26 and npm; stable Rust (`rustup default stable`) with components `rustfmt, clippy, llvm-tools-preview`.
- **Tools**:
  - `cargo install cargo-llvm-cov` (for `npm run coverage:rust`)
  - `cargo install tauri-driver --locked --version 2.1.0` (for `npm run verify`)
  - Go toolchain (for `npm run verify`, which builds a temporary scratch backend from `backend/`)

### Installing dependencies

```sh
(cd frontend && npm ci)  # the renderer is built from frontend/src
cd desktop-tauri
npm ci
```

## Running it

```sh
make desktop-tauri-dev   # or, in desktop-tauri/: npm run dev
```

That starts the renderer's Vite on port 5175 and `tauri dev` with it. It does
not start a server: run `make dev` in another terminal for one on
`http://localhost:3000`, or pick any server on the connection screen.
`AGENTRQ_SERVER_URL` locks the connection to one server, as in Electron.

The auto-updater is off in dev. The app keeps its data in its own folder, named
after its identifier `com.agentrq.desktop.tauri` (on Linux
`~/.local/share/com.agentrq.desktop.tauri/`), never in Electron's, so the two
can be installed and run side by side.

## Building it

```sh
make desktop-tauri       # or: npm run build
```

`npm run build` builds the renderer and then the bundles for this OS: `.deb`
and `.AppImage` on Linux, `.dmg` on macOS, an NSIS `-setup.exe` on Windows,
under `src-tauri/target/release/bundle/`. `npm run build:debug` builds the
debug app the verify scripts launch, with no bundle.

## Tests and the coverage gates

```sh
npm test                 # vitest
npm run test:coverage    # vitest with the 100% gate
npm run test:rust        # cargo test, core/ then src-tauri/
npm run coverage:rust    # cargo llvm-cov with the 100% line gate
```

`src-tauri` needs the built renderer (`npm run build:renderer`), because
`tauri::generate_context!` reads it at compile time. `coverage:rust` needs
`cargo-llvm-cov` (`cargo install cargo-llvm-cov`).

The gates, all in CI (the `Desktop App (Tauri)` workflow, on Linux, macOS and
Windows):

- **JS: 100% of lines, functions, branches and statements** over `src/`, except
  the renderer entry and the guest's init script (`vitest.config.mjs` says why;
  the init script's text is run by `test/panel-init.test.js`).
- **Rust: 100% of lines** over `core/`, and over `src-tauri/` except the wiring:
  `lib.rs`, `main.rs`, `test_server.rs`, `chrome.rs`, `app_menu.rs`, the side
  panel's per-OS files and `handlers.rs`, and the authorisation window's
  `handlers.rs`. The list is the `--ignore-filename-regex` in `package.json`; a
  new rule goes in a gated file with its test, never in that list.
- **`cargo fmt --check` and `cargo clippy -D warnings`** on both crates, and on
  `src-tauri` once more as a release build, the configuration that ships.
- **Versions in lockstep**: `node desktop/scripts/check-versions.mjs` holds
  `package.json`, both `Cargo.toml` files and `tauri.conf.json` to the rest of
  the repo. `node desktop/scripts/copyright.mjs` checks every file's header.
- **Screenshots**: CI starts the release build in light and dark on each OS and
  checks the window's background colour (`test/e2e/pixel.mjs`). The pictures
  are uploaded as `chrome-shots-<OS>`.

## Running the verify scripts

`scripts/verify-e2e.mjs`, `verify-connection.mjs`, `verify-markdown-links.mjs`,
`verify-side-panel.mjs`, `verify-webmcp.mjs`, and `verify-ui-telemetry.mjs`
are the Tauri twins of the Electron scripts of the same names in
`desktop/scripts/`, with the same checks. They run the real **debug build**
under `tauri-driver` (WebDriver), so nothing in the shell is stood in for,
except what must not happen on your machine (the system opener and the file
manager, see `scripts/lib/shell-stub.mjs`).

You need `tauri-driver` (`cargo install tauri-driver --locked --version 2.1.0`, the version CI uses), Go (the
scratch backend is built from `backend/`, read only, into a temp folder), and a
native WebDriver:

| OS | Native driver | Notes |
| --- | --- | --- |
| Linux | `WebKitWebDriver`, from the `webkit2gtk-driver` package | Run under Xvfb and a session bus. `xclip` lets the clipboard check read back what the app copied; `dbus-daemon` and `python3-gi` give the markdown-links check a private bus with a stand-in file manager (without them the reveal check is skipped, with the reason). |
| Windows | `msedgedriver` matching the WebView2 runtime; `VERIFY_NATIVE_DRIVER` is its path | Not in CI: on a runner msedgedriver answers "DevToolsActivePort file doesn't exist" and the app never starts. Untried by hand; the shell-call assertions would be skipped, with no opener to stand in for. |
| macOS | none | WKWebView has no WebDriver. Those rows of the parity checklist are manual. |

```sh
cd desktop-tauri
npm ci
npm run build:debug      # the app the scripts launch; rebuild after a change
npm run verify           # all verify scripts, or individual: npm run verify:e2e, etc.

# Linux, headless:
xvfb-run -a -s "-screen 0 1400x900x24" dbus-run-session -- npm run verify
```

`npm run build:debug` points the updater at an unreachable endpoint (see
`scripts/verify.tauri.conf.json`, used by that script only), so a run never
contacts GitHub and never finds an update. The release config is untouched.

Each run starts the app on a data folder of its own, so it never touches your
profiles, and exits non-zero if a check fails. `npm run verify` runs every
script and fails if any did.

**Close your own AgentRQ (Tauri) first.** The app is single-instance, keyed on
the session bus: a launch while yours is open hands over to it and exits, and
the run fails with no session. The markdown-links check is safe, since it runs
the app on a private bus; run the other two under `dbus-run-session` as above,
or quit the app. A check that cannot run is
printed as `○ ... (skipped)` with the reason; the reasons are in the scripts as
`TODO(...)` or `skip(...)`.

| Variable | Meaning |
| --- | --- |
| `AGENTRQ_SERVER_URL`, `AGENTRQ_ROOT_TOKEN` | Use a server you started, as the Electron scripts do, instead of a scratch one. |
| `VERIFY_SERVER_LOGS` | Show the scratch backend's stderr. |
| `VERIFY_DRIVER_PORT` | `tauri-driver`'s port. A free one is picked when unset. |
| `VERIFY_SERVER_BIN` | A built backend to start instead of building one (`npm run verify` sets it). |
| `VERIFY_NATIVE_DRIVER` | The native driver's path, when it is not on `PATH`. |
