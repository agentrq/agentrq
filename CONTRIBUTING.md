# Contributing to AgentRQ

Contributions come in three forms. There is no fourth.

| Your intent | What to do |
|---|---|
| **Bug report or security issue** | [Open a GitHub issue](https://github.com/agentrq/agentrq/issues/new). No PR. |
| **Feature request** | Add a proposal at `proposals/feature/<YYYYMMDD>-<slug>.md` |
| **Architectural change** | Add a proposal at `proposals/adr/<YYYYMMDD>-<slug>.md` |

Both kinds of proposal are submitted the same way: a pull request that adds one
`.md` file to the relevant folder.

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
