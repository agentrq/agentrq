# AgentRQ desktop on Tauri (preview)

Electron is still the default desktop app. This is the Tauri build beside it;
see `docs/agents/desktop-tauri.md` for the rules of the code.

## Running the verify scripts

`scripts/verify-e2e.mjs`, `verify-connection.mjs` and `verify-markdown-links.mjs`
are the Tauri twins of the Electron scripts of the same names in
`desktop/scripts/`, with the same checks. They run the real **debug build**
under `tauri-driver` (WebDriver), so nothing in the shell is stood in for,
except what must not happen on your machine (the system opener, see
`scripts/lib/shell-stub.mjs`).

You need `tauri-driver` (`cargo install tauri-driver --locked`), Go (the
scratch backend is built from `backend/`, read only, into a temp folder), and a
native WebDriver:

| OS | Native driver | Notes |
| --- | --- | --- |
| Linux | `WebKitWebDriver`, from the `webkit2gtk-driver` package | Run under Xvfb and a session bus. `xclip` lets the clipboard check read back what the app copied. |
| Windows | `msedgedriver` matching the WebView2 runtime; set `VERIFY_NATIVE_DRIVER` to its path | The shell-call assertions are skipped: there is no opener to stand in for. |
| macOS | none | WKWebView has no WebDriver. Those rows of the parity checklist are manual. |

```sh
cd desktop-tauri
npm ci
npm run build:debug      # the app the scripts launch; rebuild after a change
npm run verify           # all three, or: verify:e2e, verify:connection, verify:markdown-links

# Linux, headless:
xvfb-run -a -s "-screen 0 1400x900x24" dbus-run-session -- npm run verify
```

Each run starts the app on a data folder of its own, so it never touches your
profiles, and exits non-zero if a check fails. A check that cannot run is
printed as `○ ... (skipped)` with the reason; the reasons are in the scripts as
`TODO(...)` or `skip(...)`.

| Variable | Meaning |
| --- | --- |
| `AGENTRQ_SERVER_URL`, `AGENTRQ_ROOT_TOKEN` | Use a server you started, as the Electron scripts do, instead of a scratch one. |
| `VERIFY_SERVER_LOGS` | Show the scratch backend's stderr. |
| `VERIFY_DRIVER_PORT` | `tauri-driver`'s port (4444). |
| `VERIFY_NATIVE_DRIVER` | The native driver's path, when it is not on `PATH`. |
