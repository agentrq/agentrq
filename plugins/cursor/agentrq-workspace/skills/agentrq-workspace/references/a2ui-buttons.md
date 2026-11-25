# A2UI buttons

A card's `Button` (`child`: its label, `action`: `{"event": {"name", "context"}}`) is how the human answers it.

- **On a `reply` card**, a click posts the human's reply: the Button's `userMessage`, else its label.
- **To ask and wait**, `task.elicit` with `mode` `a2ui` and the card as `ui`, `message` its text: a click answers `{"action": "accept", "content": {"name", "context", "dataModel"}}`, the action's name and context and the card's data model. `decline` and the timeout's `cancel` are as for a form.
- A question's card has no media: it carries no files. `requestedSchema` and `url` are refused beside it.

```json
{"action":"elicit","payload":{"taskId":"0kGogHmaqvZ","message":"Release v1.2 from main?","mode":"a2ui","ui":[{"version":"1.0","createSurface":{"surfaceId":"release","catalogId":"https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json"}},{"version":"1.0","updateComponents":{"surfaceId":"release","components":[{"id":"root","component":"Card","child":"col"},{"id":"col","component":"Column","children":["q","go"]},{"id":"q","component":"Text","text":"Release **v1.2** from `main`?"},{"id":"go","component":"Button","child":"label","action":{"event":{"name":"release","context":{"branch":"main"}}}},{"id":"label","component":"Text","text":"Release"}]}}]}}
```
