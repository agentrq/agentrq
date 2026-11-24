# pano

Markdown docs every workspace of the account shares. There is no search: pass a doc's `id` in the task or event that needs it.

- `save`: with `title` and `markdown`, creates a doc and returns its `id` and `version`; with an `id` and the `version` you read, replaces it. Each of its `files` must be linked from the markdown by filename, as `![alt](chart.png)` or `[spec](spec.pdf)`, or the save is refused.
- `edit`: `id`, `version` and `ops`: append text, replace a text that occurs once, or replace a section, all or none.
- `read`: a doc by `id`; `includeComments`, `includeAttachments`; `outline` or `section` for part of a long doc.
- `comment`, `attach` (files that need no link) and `delete`.

Your workspace (the parent, for a fork) is the author. From a shell, `npx -y @agentrq/agentrq-ws@latest pano save --title T --markdown @doc.md --file chart.png` and `pano read <id> --out DIR` move docs through disk instead of your context.

## Examples

```json
{ "action": "save", "payload": { "title": "Release plan", "markdown": "# Release plan\n\n- [ ] Tag v0.9.20" } }
{ "action": "edit", "payload": { "id": "0kGpezGeuxt", "version": 3, "ops": [{ "op": "replace", "oldString": "- [ ] Tag v0.9.20", "newString": "- [x] Tag v0.9.20" }] } }
{ "action": "read", "payload": { "id": "0kGpezGeuxt", "section": "Rollout" } }
{ "action": "comment", "payload": { "id": "0kGpezGeuxt", "comment": "Done on our side." } }
```
