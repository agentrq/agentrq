# AgentRQ for Chrome

AgentRQ in the toolbar popup at mobile width (440px, an iPhone 17 Pro Max), or
opened full size in a tab when you want the room.

## Install

1. Download the `agentrq-chrome-extension` artifact from the latest
   [Chrome Plugin workflow run](https://github.com/agentrq/agentrq/actions/workflows/plugin-chrome.yml)
   and unzip it, or use this `plugins/chrome/` folder from a checkout.
2. Open `chrome://extensions`, turn on **Developer mode**, click **Load unpacked**
   and pick the folder.
3. Pin AgentRQ to the toolbar.

## Use

| To | Do |
|---|---|
| Open AgentRQ | Click the toolbar button, or press <kbd>Alt</kbd>+<kbd>Shift</kbd>+<kbd>A</kbd> |
| Open full size, in a tab | **Full size** in the header, <kbd>Alt</kbd>+<kbd>Shift</kbd>+<kbd>F</kbd>, or right-click the button → **Open full size** |
| Use a self-hosted server | Right-click → **Options** |
| Let your agents use a website's tools | Turn on detection in the popup, then **Share** a site that lights up the icon |
| Stop sharing a website | **Stop sharing** in the popup, or **Stop** in Options → **Shared websites** |

Chrome stops a popup at 800×600 and closes it when you click elsewhere, so the
popup is 440×600; full size is the way to more.

**Signing in happens in a tab.** Google and GitHub will not show their sign-in
inside another page, so a signed-out popup offers **Sign in**, which opens a
tab. Once signed in there, the popup is signed in too.

## Websites' tools

Some websites offer tools to AI agents through
[WebMCP](https://github.com/webmachinelearning/webmcp). With detection on, the
toolbar icon shows a green count on a site that offers some, and the popup
offers to share that site with one of your workspaces. Its agents can then list
the site's tools and call them in your own signed-in Chrome; any tool the site
does not mark read-only waits for your approval in the task first. A shared
site's tab that is closed is reopened in the background when a call needs it.

Detection needs access to every site, and it is off until you turn it on. It
only notices what a page registers with Chrome's own `document.modelContext`
(the extension never adds one), and only in the top frame. Nothing is sent to
AgentRQ until you share a site, and then only that site's tools, its last
address, and the results of the calls your agents make.

**It needs WebMCP turned on in the browser.** WebMCP is still experimental: in
Chrome, Brave and other Chromium browsers, open `chrome://flags` (`brave://flags`
in Brave), search for **WebMCP**, enable it and relaunch, or start the browser
with `--enable-features=WebMCPTesting`. Without it no site's tools are detected.

## How it works

The popup frames your server's own web app, so everything the web app does
works here, with the same session as your browser tabs.

The web app is not bundled into the extension because it would then run on the
extension's own origin, where the `at` sign-in cookie never goes: the problem
the desktop app needs its `app://` proxy for.

**Why it asks to read app.agentrq.com.** Inside the popup the app is a frame in
an extension page, and Chrome sends it the sign-in cookie only when the
extension has access to that site. Without it the app shows as signed out
however signed in the browser is. A self-hosted server's access is asked for
when you save it in Options, and given back when you switch away. The other
permissions are `contextMenus` for the right-click menu, `storage` for your
settings and shared sites, and `scripting` and `tabs` for site tools: to add
the detection script to pages once you allow it, and to find, and reopen, the
tab a call needs.

## Publishing

[`PUBLISHING.md`](PUBLISHING.md) covers the Chrome Web Store: the first
submission by hand, then a `chrome-v<version>` tag for every release after.
The listing text and images are in [`store/`](store/LISTING.md).

## Develop

```sh
npm test         # node:test, no dependencies
npm run test:ci  # the same, with the 100% coverage gate CI enforces
```

After editing, click the reload arrow on the extension's card in
`chrome://extensions`. Right-click inside the popup → **Inspect** for its
DevTools.
