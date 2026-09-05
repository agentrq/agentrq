# Feature proposals

Something AgentRQ should do that it doesn't do yet.

**File name:** `<YYYYMMDD>-<slug>.md` — e.g. `20260905-per-workspace-quiet-hours.md`

**How to submit:** a PR adding one `.md` file to this folder. See
[CONTRIBUTING.md](../../CONTRIBUTING.md).

## What belongs here

A new capability, or a change to how an existing one behaves — a filter on the
task board, a notification channel, a new MCP tool, a setting that isn't there.
If a user could notice the difference, it's a feature.

If it changes how the system is *built* rather than what it does — the storage
engine, the transport between agent and server, how a whole layer is
structured — it's an ADR. Put it in [`../adr`](../adr) instead.

Not sure which? Pick either. We'll move it if it's in the wrong place, and
that costs nobody anything.

## Writing one

Informal is fine and preferred. Tell us what you're running into and what you'd
like instead, the way you'd explain it to a coworker. Three sentences is a
perfectly good proposal.

**Please don't have AI expand a small idea into a formal document.** We are
reading for the idea. A generated proposal with sections for "Motivation" and
"Alternatives Considered" is harder to read than the two paragraphs it was
grown from, and we'd rather have the two paragraphs.

You don't need to propose an implementation, and you don't need to write one.
If we agree on the change, we'll build it.
