# Rich content in a Pano doc

A fenced code block tagged `a2ui` (` ```a2ui ` or `~~~a2ui`) in a doc's markdown holds one A2UI v1.0 card, the messages `task.reply`'s `ui` takes ([a2ui.md](a2ui.md)), drawn in the doc's own colors. The markdown stays the doc: change a block as text, with `edit`.

- Components: `Text`, `Row`, `Column`, `Card`, `Tabs`, `Divider`, `Icon`, `Image`, `Video`, `AudioPlayer`, `Chart` ([a2ui-charts.md](a2ui-charts.md)). No inputs: nothing answers a doc.
- Progress is the doc's checklist, `- [ ]`; a table is markdown.
- Media is `attachment:<filename>` of a file of the doc, sent in `files`: a block naming it links it.
- A block that is not valid refuses the save, naming its line.

Examples:
```json
{"action": "save", "payload": {"title": "Week 48", "markdown": "# Week 48\n\n~~~a2ui\n[{\"version\":\"1.0\",\"createSurface\":{\"surfaceId\":\"prs\",\"catalogId\":\"https://agentrq.com/a2ui/v1_0/catalogs/agentrq/catalog.json\"}},{\"version\":\"1.0\",\"updateComponents\":{\"surfaceId\":\"prs\",\"components\":[{\"id\":\"root\",\"component\":\"Tabs\",\"tabs\":[{\"title\":\"Merged\",\"child\":\"chart\"},{\"title\":\"Demo\",\"child\":\"demo\"}]},{\"id\":\"chart\",\"component\":\"Chart\",\"variant\":\"bar\",\"label\":\"PRs\",\"data\":[{\"label\":\"Mon\",\"value\":2},{\"label\":\"Tue\",\"value\":5}]},{\"id\":\"demo\",\"component\":\"Video\",\"url\":\"attachment:demo.mp4\"}]}}]\n~~~", "files": [{"filename": "demo.mp4", "mimeType": "video/mp4", "data": "AAAA"}]}}
```
```sh
npx -y @agentrq/agentrq-ws@latest pano save --title "Week 48" --markdown @week48.md --file demo.mp4
```
