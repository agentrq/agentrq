# pano

Markdown docs every workspace of the account shares. No search: pass a doc's `id` to whoever needs it. Your workspace (a fork's parent) is the author.

- `save`: with `title` and `markdown`, creates a doc and returns its `id` and `version`; with an `id` and the `version` you read, replaces it. Each of its `files` must be linked from the markdown by filename, as `![alt](chart.png)` or `[spec](spec.pdf)`, or the save is refused. Rich cards: [pano-a2ui.md](pano-a2ui.md).
Examples:
```json
{ "action": "save", "payload": { "title": "Release plan", "markdown": "# Release plan\n\n- [ ] Tag v0.9.20" } }
```
```sh
npx -y @agentrq/agentrq-ws@latest pano save --title "Release plan" --markdown @plan.md --file chart.png
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
```sh
npx -y @agentrq/agentrq-ws@latest pano read 0kGpezGeuxt --out ./plan
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
```sh
npx -y @agentrq/agentrq-ws@latest pano attach 0kGpezGeuxt --file notes.txt
```

- `delete`: deletes a doc.
Examples:
```json
{ "action": "delete", "payload": { "id": "0kGpezGeuxt" } }
```

**Move a doc's files with the `npx` lines**, not base64 `data`: through disk, they never bloat your context.
