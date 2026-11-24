# A turn ends at the usage footer, not at a reply

> Read before changing the task composer, the native ACP turn handling (`controller/acpv1`), or anything that decides whether an agent is busy.

Native ACP gives a task one session and a session one turn at a time, and
chains a second delivery behind the first (`runTurn` in `controller/acpv1`).
A message posted while the agent is working is therefore queued there,
not read — so the composer does not post it. Mid-turn, Send holds the message
in the browser instead (`useQueuedMessages.js`, kept in `localStorage`), where
it stays editable, and Stop appears alongside Send rather than replacing it.

**The queue leaves one message per turn, head first.** Sending the rest
alongside it would chain them behind the turn it starts, and would take away
the point of queuing: a message still waiting is still editable.

Which makes "when does a turn end" load-bearing — it is also when the next
queued message is sent — and the obvious answer is wrong. **A reply does not
end a turn.** The server's own instructions tell agents to report progress with
`task.reply` every few steps, so a reply arriving is usually the agent talking while
it works; taking one as the end posts a queued message into the running turn,
which is the bug this exists to fix.

What ends a turn is the **usage footer**. The native engine posts it at the
end of every delivery (`markTurnEnd` in `controller/acpv1`), after the reply
and any note — a turn cancelled by Stop, or skipped by one, included — and
empty when the agent reports no usage, which draws no ring. **Every way a turn
ends must post it**: a turn with nothing to end it locked the composer (#1347)
and left the working banner on (#1350).

The server keeps one footer per task and revises it in place, so its place in
the list is where it was first written. Its `reportedAt` is what says a
revision came after the person's last message.

**Send Now on a queued message never posts it mid-turn.** It moves the message
to the head and stops the turn, and the turn's end sends it. Posted at once, the
engine chains it behind the cancelled turn, and that turn's end then sends the
next queued message into it.

The classic gateway sends the footer only when the agent reported usage, and
ends an early stop with a note instead, so its notes end a turn too
(`STOP_REASON_NOTE`). For a task that has never seen a footer — and only then
— a plain reply is taken as the end. The rule lives in `useAgentTurn.js`, with
the cases as tests.

A queue also has no turn-end to wait for if the turn finished while the page
was closed, so the task view sends its head once after loading too; that send
starts a turn, and the rest follow it as usual.

Everything is gated on `agentSupportsStop`: Claude Code speaking MCP directly
does not chain turns and cannot be stopped, so there is nothing to queue
behind and Send posts immediately, as it always has.

