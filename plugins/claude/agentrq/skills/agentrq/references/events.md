# The events tool

An event is a named signal a workspace publishes; a trigger is a standing instruction to create a task somewhere when it fires. Together they are how one workspace's finished work starts another's.

- `list` — List the events defined for this account
- `create` — Define a named signal (`^[a-z][a-z0-9_]{0,128}$`) and what a publisher should put in its payload
- `get` — Get an event by ID
- `update` — Revise an event's payload guidelines. Its name is fixed once created
- `delete` — Delete an event. Its triggers stop firing
- `createTrigger` — When this event fires, create a task in this workspace. `{{EVENT_PAYLOAD}}` and `{{EVENT_FAQ}}` are substituted in the body only; `emitEventId` chains a second event to the task's completion
- `listTriggers` — List the triggers attached to an event
- `getTrigger` — Get an event trigger by ID
- `updateTrigger` — Rewrite a trigger. Every field is written as given, so send the ones to keep as well
- `deleteTrigger` — Delete a trigger, leaving its event in place
- `listTasks` — List the tasks an event has spawned, to see whether a wired-up system is running

A workflow ([workflows.md](workflows.md)) is the graph these pieces add up to.
