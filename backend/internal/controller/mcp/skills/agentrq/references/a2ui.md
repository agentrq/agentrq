# A2UI cards

`task.reply`'s `ui` is a card shown with `text`: A2UI v1.0 messages for one surface. Draw one when layout beats prose.

- **`text` stays required** and says all the card says: push, email, agents see only it.
- **Media only as `attachment:<filename>`** of a file in the same reply's `attachments`; no links.
- Messages: `createSurface` (`surfaceId`, `catalogId` `https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json`); `updateComponents`, a flat list, root's `id` `root`; optional `updateDataModel` (`path`, `value`), read with `{"@path": "/x"}`.
- A refused `ui` posts nothing and says why.

Components: `Text` (`text`, markdown; `variant` caption), `Image` (`url`), `Icon` (`name`: check, error, warning…), `Video`, `AudioPlayer`, `Row`, `Column`, `List` (`children`: ids, or `{"path": "/rows", "componentId": "row"}` per item, bound relative: `{"@path": "name"}`), `Card` (`child`), `Tabs` (`tabs`: `{title, child}`), `Divider`, `Button`: [a2ui-buttons.md](a2ui-buttons.md), `Chart`, `Progress`: [a2ui-charts.md](a2ui-charts.md).

Patterns:
- Result: Card → Column → Text `## Title`, Text, `Chart`.
- Comparison: Card → List over `/rows` → Row → Text name, value.
- Status: Card → Column → `Progress`, a Row per check: Icon `check`/`error`, Text.

```json
{"action":"reply","payload":{"taskId":"0kGogHmaqvZ","text":"Build passed: 412 tests.","attachments":[{"filename":"chart.png","mimeType":"image/png","data":"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="}],"ui":[{"version":"1.0","createSurface":{"surfaceId":"build","catalogId":"https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json"}},{"version":"1.0","updateComponents":{"surfaceId":"build","components":[{"id":"root","component":"Card","child":"col"},{"id":"col","component":"Column","children":["title","chart"]},{"id":"title","component":"Text","text":"## Build passed"},{"id":"chart","component":"Image","url":"attachment:chart.png"}]}}]}}
```
