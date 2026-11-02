# Chrome Web Store listing

Everything the Developer Dashboard asks for, ready to paste. The images are
beside this file.

## Store listing tab

**Name** — from the manifest: `AgentRQ`

**Summary** (from the manifest, at most 132 characters):

> Task manager for AI agents: orchestrate many agents at once and open Claude Code terminal right in the extension.

**Description:**

```
AgentRQ is a task manager for AI agents. Give your agents tasks, orchestrate many of them at once, and open the Claude Code terminal an agent is running in, all from a popup on your Chrome toolbar.

TASK MANAGER FOR AI AGENTS
• Create tasks and assign them to an agent or to yourself, one-off or on a schedule.
• See every task by status: not started, ongoing, blocked, done.
• Answer the questions agents are blocked on, and approve the steps they ask permission for.

AGENT ORCHESTRATION
• Run many agents at once, each in its own workspace, and see at a glance which are working and which are waiting on you.
• Chain work between agents: one workspace's finished task can start the next task in another.

LET YOUR AGENTS USE WEBMCP TOOLS
• Websites can offer tools to AI agents with WebMCP. Turn on detection and the AgentRQ icon lights up on a site that does.
• Share that site with a workspace in one click, and its agents can call the site's tools in your own signed-in browser. Anything the site does not mark read-only waits for your approval.
• Stop sharing from the popup or Options at any time.
• Needs WebMCP turned on in your browser: search for "WebMCP" in chrome://flags (brave://flags in Brave), enable it and relaunch.

CLAUDE CODE TERMINALS IN THE EXTENSION
• Open the live Claude Code terminal an agent is working in, right in the popup, to watch it or take over. Works for agents on any machine you have connected with agentrqd, AgentRQ's agent runner.

CONTROL AGENTS WITH AGENT CLIENT PROTOCOL (ACP)
• Claude Code, Codex and Antigravity all in a single control plane.

ONE CLICK FROM YOUR TOOLBAR
• A mobile-sized popup that is always there; "Full size" or Alt+Shift+F opens AgentRQ in a tab when you want the room.
• Uses your existing AgentRQ sign-in, in light or dark.
• Works with app.agentrq.com, or your own self-hosted AgentRQ server (set it in Options).

Keyboard shortcuts (change them at chrome://extensions/shortcuts):
• Alt+Shift+A: open AgentRQ
• Alt+Shift+F: open AgentRQ full size

Open source: https://github.com/agentrq/agentrq
```

**Category** — Productivity → Workflow & Planning

**Language** — English

**Graphic assets:**

| Field | File |
|---|---|
| Store icon (128×128) | `../icons/128.png` |
| Screenshots (1280×800) | `screenshot-1-claude-code-webmcp.png`, `screenshot-2-popup.png`, `screenshot-3-dark.png`, `screenshot-4-full-size.png` |
| Small promo tile (440×280) | `promo-small-440x280.png` |
| Marquee promo tile (1400×560) | `promo-marquee-1400x560.png` |

**Official URL** — `https://agentrq.com` (only offered once the domain is verified in Google Search Console for the publisher account; skip it otherwise)

**Homepage URL** — `https://agentrq.com`

**Support URL** — `https://github.com/agentrq/agentrq/issues`

## Privacy practices tab

**Single purpose:**

```
Shows the user's AgentRQ workspace (a task manager for AI agents) in the toolbar popup, where they can manage tasks, orchestrate their agents and open an agent's Claude Code terminal, opens AgentRQ full size in a tab, and lets the user give their agents the WebMCP tools of a website they choose to share.
```

**Permission justifications:**

`contextMenus`

```
Adds one item, "Open full size", to the right-click menu of the extension's toolbar button. It opens AgentRQ in a normal browser tab. The extension adds nothing to web pages' own context menus.
```

`scripting`

```
Only after the user turns on website-tool detection (which grants the optional access to all sites), registers one content script that notices when a page registers WebMCP tools with the browser's own document.modelContext API, so the toolbar icon can show that the site offers tools. It is unregistered when that access is removed. The extension injects nothing else and changes no page.
```

`storage`

```
Stores the address of the user's AgentRQ server (app.agentrq.com by default, or a self-hosted server entered in Options), so the popup knows which server to show, and the list of websites the user has shared with an AgentRQ workspace, with each one's last address and tool list, so sharing survives a browser restart.
```

`tabs`

```
Finds the tab of a shared website that an agent's tool call should run in, reads the address of the active tab so the popup can offer to share the site it is showing, and reopens a closed shared website in a background tab when a call needs it. Tab addresses are not stored or sent anywhere except the last address of a site the user has shared.
```

**Host permission** (the dashboard asks once, for all of them):

```
The popup shows the AgentRQ web app from app.agentrq.com in a frame. Chrome sends the user's existing AgentRQ sign-in cookie to that frame only when the extension has access to the site; without it the app would always appear signed out. The extension also calls app.agentrq.com/api/v1/auth/user to check whether the user is signed in, so it can offer sign-in in a tab (Google and GitHub sign-in pages cannot load inside a frame). It reads only the response status, never its content, and accesses no other site. Access to a self-hosted AgentRQ server is optional: it is requested only when the user saves that server's address in Options, only for that one origin, and removed when they switch away.

Access to all sites is optional too, and off until the user turns on website-tool detection in the popup or Options. With it the extension runs a script in each page's top frame that only detects whether the page registers WebMCP tools (document.modelContext.registerTool), and shows a count on the toolbar icon. Nothing about any site is sent until the user shares that site with an AgentRQ workspace; then only that one site's tool list, its last address, and the results of tool calls the user's agents make are sent to the user's own AgentRQ server. Turning detection off in Options removes the access.
```

**Are you using remote code?** — **No, I am not using remote code.**

```
All of the extension's JavaScript is in the package. The popup shows the AgentRQ website in an iframe, as a web page, and does not load or run any code in the extension's own context.
```

**Data usage** — the popup *is* AgentRQ's interface, so disclose what AgentRQ
collects through it; the store reads it that way, and under-disclosure is what
gets a listing rejected. Tick:

- **Personally identifiable information**: name and email from Google or GitHub sign-in
- **Personal communications**: the messages exchanged with agents in tasks
- **User activity**: the web app's interface usage telemetry (shortcuts, searches, copies)
- **Website content**: the tool list, and tool call results, of a website the user shares with a workspace
- **Web history**: the last address visited on a shared website, which is where a call reopens it

Leave the rest unticked. Authentication information in particular: the browser
keeps the sign-in cookie, and the extension's code never reads it; its sign-in
check looks only at the HTTP status. Detection itself sends nothing: a site is
collected from only once the user shares it. The privacy policy must cover the
five ticked kinds, because the store checks that the two agree.

Tick all three certifications:

- I do not sell or transfer user data to third parties, outside of the approved use cases
- I do not use or transfer user data for purposes that are unrelated to my item's single purpose
- I do not use or transfer user data to determine creditworthiness or for lending purposes

**Privacy policy URL** — `https://agentrq.com/privacy`

## Distribution tab

- **Payments** — Free
- **Visibility** — Public (or Unlisted to share it by link before announcing it)
- **Regions** — All regions
