# Slash commands: running the agent's own commands from a task

Some agents come with commands of their own — `/init`, `/compact`, `/review`
and whatever else they have been built with. When your workspace is connected
through the [ACP gateway](https://github.com/agentrq/acp-gateway), those
commands are offered in the reply box.

Type `/` in a task and the agent's commands appear above the composer. Keep
typing to narrow the list, then press Enter or Tab to choose one — or click it.
Arrow keys move the highlight, Escape puts the menu away.

The same menu is in the description when you create a task, so a task can *be* a
command. Cmd ⌘ + Enter creates the task from there without reaching for the
button.

Nothing to install and nothing to configure. If the agent has commands, the
menu is there; if it has none, nothing changes.

---

## Where the list comes from

The agent, not AgentRQ. An ACP agent announces what it accepts once its session
is up, and revises the list whenever its context makes different commands
relevant — so the menu can gain or lose entries while you are working, and the
descriptions can change under you. That is the agent talking, and the menu
follows it live rather than waiting for a page reload.

Two consequences worth knowing:

- **A freshly connected workspace has no commands yet.** The agent's session
  starts when it takes its first task, and it advertises its commands then. An
  idle workspace with an attached agent may show nothing until there is work.
- **An agent can withdraw them.** If the list goes away, so does the menu.
  Better that than offering commands that would be refused.

Only agents speaking ACP announce commands at all. Every other agent — Claude
Code connected directly, anything on plain MCP — simply has no menu, and the
reply box behaves exactly as it always has.

## How a command reaches the agent

A command is not a special kind of message. ACP runs one as ordinary prompt
text, and the agent recognises it by the command sitting at the *start* of what
it receives.

That is why an ordinary reply and a command are delivered differently. Your
replies normally reach the agent wrapped in a short line naming the task, which
is what lets an agent reading a stream of notifications know what it is
answering. A command cannot carry that wrapper — with anything in front of it,
`/compact` is a sentence mentioning a command rather than a command — so a
reply that starts with one is sent exactly as you typed it.

**The text has to name a command the agent actually advertised.** This is what
keeps a message like `/Users/me/notes.txt is out of date` an ordinary message:
it starts with a slash, but nothing offered it, so it is delivered with its
context intact like any other reply. Only names from the menu are treated as
commands.

Attachments still travel with the message either way. They are listed on their
own lines after it, so they never come between the command and the start of the
prompt.

**A task whose body is a command is delivered the same way, with one
difference.** A task notification also carries the line naming the task, and
that line is the only place the agent learns the task's ID — which it needs to
move the task on or reply to it. So the command goes first and the naming line
follows it. The trade-off is worth knowing: everything after a command's name is
that command's argument, so the naming line lands there too. Putting it first
instead would stop the command being a command, and leaving it out would leave
the agent unable to report back; of the three, a slightly noisy argument is the
one that still works.

## If the menu does not appear

- **Check the agent is connected.** The workspace header shows it.
- **Check the agent has run something.** Commands arrive with the agent's
  session, which starts with its first task.
- **Check what you are connected through.** The menu needs an ACP agent behind
  the gateway; see the gateway's README for which agents those are.
- **Check the gateway's version.** Forwarding commands was added to
  `@agentrq/acp-gateway` after 0.2.12; an older gateway never sends them, and
  everything else keeps working as before.
