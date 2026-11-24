# pano

Markdown docs every workspace of the account shares. There is no search: pass a doc's `id` in the task or event that needs it. Your workspace (the parent, for a fork) is the author.

- `save`: with `title` and `markdown`, creates a doc and returns its `id` and `version`; with an `id` and the `version` you read, replaces it. Each of its `files` must be linked from the markdown by filename, as `![alt](chart.png)` or `[spec](spec.pdf)`, or the save is refused.
Examples:
```json
{ "action": "save", "payload": { "title": "Release plan", "markdown": "# Release plan\n\n- [ ] Tag v0.9.20" } }
```

- `edit`: `ops` on the `version` you read: `append`, `replace` a text that occurs once, or `replaceSection`; all or none.
Examples:
```json
{ "action": "edit", "payload": { "id": "0kGpezGeuxt", "version": 3, "ops": [{ "op": "replace", "oldString": "- [ ] Tag v0.9.20", "newString": "- [x] Tag v0.9.20" }] } }
```

- `read`: `includeComments`, `includeAttachments`; `outline` or `section` for part of a long doc.
Examples:
```json
{ "action": "read", "payload": { "id": "0kGpezGeuxt", "section": "Rollout" } }
```

- `comment`: adds a comment to a doc.
Examples:
```json
{ "action": "comment", "payload": { "id": "0kGpezGeuxt", "comment": "Done on our side." } }
```

- `attach`: adds `files` the markdown need not link.
Examples:
```json
{ "action": "attach", "payload": { "id": "0kGpezGeuxt", "files": [{ "filename": "notes.txt", "mimeType": "text/plain", "data": "aGVsbG8=" }] } }
```

- `delete`: deletes a doc.
Examples:
```json
{ "action": "delete", "payload": { "id": "0kGpezGeuxt" } }
```

From a shell, `npx -y @agentrq/agentrq-ws@latest pano save --title T --markdown @doc.md --file chart.png` and `pano read <id> --out DIR` move docs through disk instead of your context.
