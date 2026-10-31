# The workflows tool

A workflow is the graph events and triggers add up to: a start event, and steps that react to it and to each other.

- `list` — List the workflows defined for this account
- `create` — Create an empty workflow around a start event
- `get` — Get a workflow by ID
- `update` — Revise name, description, start event or canvas layout. Only the fields sent are changed
- `delete` — Delete a workflow and its steps. The events it named are left alone
- `createStep` — Add a step: the event it reacts to, the workspace and task it creates, and the event it emits on completion
- `listSteps` — List a workflow's steps
- `deleteStep` — Remove one step, leaving the rest of the graph in place
- `listTasks` — List the tasks a workflow has spawned
- `getText` — Read the whole graph as an indented document. Steps naming a deleted event or workspace are left out so it always parses
- `replaceFromText` — Replace the entire graph with a document. Names are resolved before anything is written, so an unknown one leaves the workflow untouched

Text mode is two spaces per level, and every list line is `- agent:<workspace>` or `- event:<name>`, alternating:

```
workflow: new_feature
event: code_changed
- agent:doc
  - event:doc_updated
- agent:blog
```
