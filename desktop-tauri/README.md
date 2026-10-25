# AgentRQ desktop on Tauri (preview)

Electron is still the default desktop app. This is the Tauri build beside it;
see `docs/agents/desktop-tauri.md` for the rules of the code.

## Running the verify scripts

`scripts/verify-e2e.mjs`, `verify-connection.mjs` and `verify-markdown-links.mjs`
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
npm run verify           # all three, or: verify:e2e, verify:connection, verify:markdown-links

# Linux, headless:
xvfb-run -a -s "-screen 0 1400x900x24" dbus-run-session -- npm run verify
```

`npm run build:debug` points the updater at an unreachable endpoint (see
`scripts/verify.tauri.conf.json`, used by that script only), so a run never
contacts GitHub and never finds an update. The release config is untouched.

Each run starts the app on a data folder of its own, so it never touches your
profiles, and exits non-zero if a check fails. `npm run verify` runs all three
and fails if any did.

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
