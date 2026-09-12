# Contributing to AgentRQ

Contributions to this repository come in three forms. There is no fourth.

Building an **extension** is not a contribution to this repository and needs
nothing from us — [skip to that](#extensions-need-none-of-this) if that is what
you are here for.

| Your intent | What to do |
|---|---|
| **Bug report or security issue** | [Open a GitHub issue](https://github.com/agentrq/agentrq/issues/new). No PR. |
| **Feature request** | Add a proposal at `proposals/feature/<YYYYMMDD>-<slug>.md` |
| **Architectural change** | Add a proposal at `proposals/adr/<YYYYMMDD>-<slug>.md` |

Both kinds of proposal are submitted the same way: a pull request that adds one
`.md` file to the relevant folder.

## Extensions need none of this

The three forms above are how you change *this* repository. Building an
**extension** is not one of them, because it does not touch this repository at
all — and that is the point. You need no permission, no proposal and no reply
from us. There is no review, no approval queue, no registry to be admitted to
and no fee.

Publishing one is three things:

1. Add the **`agentrq-extension`** topic to your GitHub repository. That is the
   whole of discovery — the desktop app searches for that topic, and anything
   carrying it shows up in everyone's Extensions screen.
2. Put an `agentrq-extension.json` manifest at the root.
3. Cut a release with your built asset attached, and put its SHA-256 in the
   manifest.

It is an ordinary Node module. It can add pages, context-menu actions, keyboard
shortcuts, scheduled work, and renderers for fenced code blocks — and it can
call the same MCP tools the app itself uses, with whatever permissions the
person installing it agrees to.

**[docs/EXTENSIONS.md](https://github.com/agentrq/agentrq/blob/main/docs/EXTENSIONS.md)**
is the whole of it: what the manifest holds, what each surface can do, how
permissions are asked for, and how to develop one against a running app without
publishing anything.

Three worked examples live in [`examples/extensions/`](examples/extensions) —
`task-stats`, `standup` and `digest` — and
[agentrq/mermaid-agentrq](https://github.com/agentrq/mermaid-agentrq) is a real
published one, small enough to read in a sitting.

The flip side of no gatekeeper is worth stating, since it is your users who
carry it: nothing in the catalogue is reviewed and there is no sandbox, so an
extension runs with full access to the machine of whoever installs it. The app
says so on the screen and asks before installing anything. Write yours as though
somebody is going to read it, because somebody should.

## Why we work this way

Given that coding agents write most of the underlying code now, we'd prefer
feature and ADR PRs in the form of human-written text. This can be quite
informal too — just run your idea by us the same way you would a coworker or a
friend, say, over Discord. If we're aligned on the change, we're happy to burn
our tokens on the underlying implementation.

**Please do not have AI artificially expand what you'd like to do into a formal
proposal.** A few honest paragraphs beat a generated document with headings for
"Motivation", "Non-Goals" and "Alternatives Considered". We are reading for the
idea, not the format.

## Bugs

Just open an issue. We appreciate this a lot, and will credit you as reporter.

## Writing a proposal

The file name is `<YYYYMMDD>-<slug>.md` — the date you opened it, then a short
hyphenated name. For example, `20260905-per-workspace-quiet-hours.md`.

There is no template. Say what you want and why; that is the whole ask. If it
helps to know what we tend to wonder about while reading:

- What problem are you hitting, and what do you do about it today?
- Roughly what should happen instead?
- Anything that would obviously break, if you can see it from where you sit?

Skip any of those that don't apply. A three-sentence proposal is a perfectly
good proposal.

See [`proposals/feature/README.md`](proposals/feature/README.md) and
[`proposals/adr/README.md`](proposals/adr/README.md) for which folder yours
belongs in.

## What happens next

We read it and reply on the PR. If we're aligned, we merge the proposal and
pick up the implementation ourselves — you do not need to write the code. If
we're not, we'll say so on the PR and why; the proposal still stays useful as a
record of something we considered.
