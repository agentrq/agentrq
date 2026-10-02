# Desktop app (`desktop/`)

> Read before changing anything under `desktop/` — or any frontend code, since the desktop build renders all of it. Full detail is in [`desktop/README.md`](../../desktop/README.md); user-facing docs are [`docs/DESKTOP.md`](../DESKTOP.md).

The desktop app renders the *same* Vue application as the browser. Both call
`createAgentRQApp({ history, platform })` from `frontend/src/app.js`, so there is
exactly one route table — **never add a route anywhere else**, or the two builds
drift apart silently.

## The app:// proxy — do not reintroduce cross-origin API calls

The frontend addresses the API with same-origin **relative** URLs (`src/api.js`)
and authenticates with the `at` cookie. Backend CORS is `AllowOrigins: "*"` with
no `AllowCredentials`, so a renderer on its own origin could never attach that
cookie.

The desktop renderer is therefore served from a privileged `app://` scheme, and
`desktop/src/main/protocol.js` forwards `/api`, `/mcp` and `/.well-known` to the
configured server from the main process, where Electron's session cookie jar
holds the credentials. **The renderer only ever sees same-origin traffic.**

Consequences worth knowing before changing anything here:

- **Never introduce an absolute API URL in frontend code.** It would work in the
  browser and break the desktop app, where it becomes a cross-origin request
  with no cookie.
- **Never relax backend CORS to accommodate the desktop app.** It does not need
  it, and doing so widens the attack surface of every deployment.
- **The proxy does not carry WebSockets, and the CSP has to know.** Electron's
  handler intercepts HTTP and not upgrades, so the one socket the app opens —
  the terminal — is addressed absolutely and needs the server's ws/wss origin
  in `connect-src`. `'self'` refuses it, and Chromium refuses it by making the
  `WebSocket` constructor *throw*, so nothing connects and nothing reports why.
  That is the whole story of the desktop terminal never working; see
  [the daemon note](machines-and-daemon.md). Anything else the renderer needs
  to reach still belongs behind the proxy, not in the policy.
- **Give every await in the main process a deadline, and make it a race.** The
  handler's promise is what the renderer's `fetch` waits on, so an await that
  never settles is a frozen app rather than an error — and a crashed network
  service abandons its requests instead of failing them. An `AbortSignal` is not
  enough: the request that hangs is the one that will not answer an abort.
- Desktop-only capabilities reach the renderer through the narrow `window.agentrq`
  bridge in `desktop/src/preload/`. Components branch on `usePlatformStore()`,
  never on user-agent sniffing or probing for `window.agentrq`.
- **The side panel is a `<webview>`, and `will-attach-webview` is its boundary.**
  `desktop/src/main/side-panel/` overwrites every guest's preferences there —
  own partition (`persist:panel-<profile>`, never the profile's, so no guest
  ever carries the `at` cookie), sandbox, no Node, our preload — whatever the
  element asked for. Not a `WebContentsView`: a native view is drawn over the
  page, so the app's dialogs, menus and bottom-right toasts would vanish
  behind it. While the resize handle is dragged the webview needs
  `pointer-events: none`, or the guest swallows the pointer and the drag stalls.
  Popups stay *enabled* in the guest: disabled, `target=_blank` never reaches the
  window-open handler that refuses it, and the link silently does nothing.
  An Extension's panel page (`agentrq-ext://<name>/…`) is served from its declared
  folder only, and its bridge is routed by `event.senderFrame.url` — never by the
  message — or one Extension's page could reach another.
- **Links leave for the real browser; the AgentRQ sign-in may not.** `classifyLink`
  keeps auth URLs on the configured server in-app because the `at` cookie has to
  land in this profile's jar — sent out, sign-in "succeeds" and the app stays
  signed out.
- **Never make a `file:` URL followable.** `classifyLink` blocks the scheme on
  purpose — message bodies are agent-written, and a followed `file:` link is how
  one reaches the machine. Rendered markdown therefore strips the href and
  parks the URL in `data-file-url` (`frontend/src/utils/markdown.js`); clicking
  it is a bridge request answered by `desktop/src/main/files.js`, which opens
  only file types that are read rather than run and reveals everything else in
  the file manager.
- **The update installer writes to a file, never a pipe.** It quits this app
  mid-install, and a pipe with no reader would kill it there; the same file is
  what the update banner's progress bar is read from.
- **Copying from the renderer goes through the shell on desktop.**
  `navigator.clipboard.writeText` throws in a window that is not frontmost, so
  `writeClipboard` prefers `window.agentrq.clipboard` and falls back to the
  browser API only where there is no shell.

## A profile is named by its account, and the account has to be remembered

The switcher shows who a profile is signed in as, because a list of profiles
called "Default" and "Work" does not say which account you are about to switch
to. That answer is fetched live, and the live answer is absent constantly: the
session is good for a day, the lookup gives up after four seconds, and the app
starts each time knowing nothing.

So there are two accounts on a profile and the difference is the point.
`identity` is the live one — signed in *right now* — and `account` is the
stored one, who the profile belongs to. `rememberAccount` writes the second
whenever a lookup succeeds and, crucially, **does nothing when a lookup fails**.
Writing "could not say" into the record is what turned the switcher into a list
of profiles all reading "Default": every row fell back to its label, and every
label was the same word. For the same reason an unnamed new profile is numbered
rather than being called "Default" like the first one.

Two profiles on one account cannot be refused when a profile is added — it has
no account yet, and the sign-in happens on a web page the shell does not drive
— so `duplicateOf` catches it once the account is known and the switcher says
so, beside the action that fixes it.

## Five traps that are invisible in source

- **Tailwind scans from the build root.** The desktop build's Vite root is
  `desktop/src/renderer`, so a class used only in a file under `desktop/` is
  silently dropped from the stylesheet — the DOM looks right and the app renders
  unstyled. `frontend/src/style.css` declares `@source './'` to fix this, and
  desktop-only *views* live in `frontend/src/desktop/` for the same reason.
  Moving them into `desktop/` breaks their styling with no error.
- **macOS hides the title bar, so the page owns the window.** The window is
  created with `titleBarStyle: 'hiddenInset'`, and a window with no title bar
  cannot be dragged until the page declares a region with `-webkit-app-region:
  drag` — the `.app-drag` class. The traffic lights are also drawn over the
  top-left of the page, so that same strip reserves their space. Both are gated
  on `platformStore.isMacDesktop`; making page content draggable on Windows or
  Linux would only remove text selection. The strip is `DesktopTitleBar.vue`,
  which also shows the page title, the profile switcher (so the sidebar's
  account menu leaves profiles out on macOS) and a window menu; anything
  clickable in it needs `.app-no-drag`, or the drag region swallows the click.
- **macOS only routes a URL scheme an app declares in its bundle.** Calling
  `app.setAsDefaultProtocolClient()` is enough for Windows and Linux, but the
  `protocols` entry in `desktop/electron-builder.yml` is what makes
  `agentrq://` links work on a packaged macOS build.
- **`sandbox_extension_issue_file failed ... Operation not permitted` is macOS
  talking, not Electron.** It is libsandbox refusing the unpackaged dev bundle;
  Chromium logs nothing here and carries on, and a packaged build never sees it.
  `desktop/scripts/mac-sandbox-preflight.mjs` speaks up only for the two causes
  that *are* ours — a quarantined or a gutted `node_modules/electron`.
- **The Linux app icon is two mechanisms, and packaging supplies one.** The
  single `icon:` in `electron-builder.yml` is the whole story on Windows (it is
  compiled into the `.exe`) and macOS (it is the `.icns` in the bundle), which
  is why a Linux-only icon bug is invisible on both. On Linux nothing tells a
  *running window* what to show, so every `BrowserWindow` is handed the icon
  explicitly via `desktop/src/main/app-icon.js` — and separately, the dock only
  shows it if the window can be matched to its installed launcher entry by app
  id, which is what `desktopName` in `desktop/package.json` and
  `linux.syncDesktopName` exist to make agree. Note that Linux is the one
  platform where electron-builder names things after `package.json`'s `name`
  rather than the product name, so the two must be `agentrq-desktop`, not
  `AgentRQ`. Changing either without the other silently returns the dock to a
  generic icon; `desktop/test/app-icon.test.js` is the check. The *installed*
  icon is a third thing again: electron-builder does not resize a PNG to build
  a freedesktop icon set, so `linux.icon` points at `desktop/resources/icons`,
  a committed set rendered from the app's SVG by `npm run icons`. Change the
  mark and re-run that, or a test fails on the recorded source hash.

Full detail, including the verification scripts, is in `desktop/README.md`.
User-facing documentation is `docs/DESKTOP.md`.

