# Architecture decision records

A change to how AgentRQ is built, rather than to what it does.

**File name:** `<YYYYMMDD>-<slug>.md` — e.g. `20260905-move-attachments-to-object-storage.md`

**How to submit:** a PR adding one `.md` file to this folder. See
[CONTRIBUTING.md](../../CONTRIBUTING.md).

## What belongs here

Decisions that are expensive to reverse and that later work has to build on:
swapping a storage engine, changing how the agent and the server talk, adding
or removing a layer, a new dependency that everything ends up importing.

The test is roughly: *would someone reading the code in a year wonder why it
was done this way?* If yes, it wants a record.

If it's a capability a user would notice, it's a feature — put it in
[`../feature`](../feature) instead. Not sure which? Pick either; we'll move it
if it's in the wrong place.

## Writing one

Same as anywhere else here: informal, human-written, as long as it needs to be
and no longer. **Please don't have AI inflate it into a formal document.**

What's actually useful in an ADR, if you have it:

- What forced the decision — the constraint, the limit you hit, the thing that
  stopped working.
- What you'd do about it.
- What you gave up by choosing that. This is the part future readers want most,
  and the part that's hardest to reconstruct later.

A record that says "we did X because Y, and it costs us Z" has done its job.

## A note on records that didn't happen

Merged does not mean shipped. A proposal we agreed with is still a record of
what we thought at the time, so an ADR that was superseded stays in place — add
a new one that says what changed and link back to it, rather than editing the
old file into agreement with the present.
