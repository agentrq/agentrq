# A2UI charts

A `Chart` draws numbers as the workspace analytics pages do. It is in our catalog: `createSurface`'s `catalogId` is `https://agentrq.com/a2ui/v1_0/catalogs/agentrq/catalog.json`, which has every basic component too.

- `variant`: `line` (a value over time, the default), `bar` (values side by side), or `heatmap`: a value per day, labels `YYYY-MM-DD`, up to 366 days; or per hour, `YYYY-MM-DD HH:00`, up to 31 days.
- `data`: `[{"label", "value"}]` in order, values zero or more, at most 744; or `{"@path": "/points"}` in the data model.
- `label`: what a value counts, such as `Tasks`.
- `color`: `tasks` (the default) or `messages`, the analytics tones in both themes; or a `#rrggbb` used as given.
- `text` still says the numbers that matter: Slack lists the points and links the card.

```json
{"action":"reply","payload":{"taskId":"0kGogHmaqvZ","text":"12 PRs merged this week, 5 on Tuesday.","ui":[{"version":"1.0","createSurface":{"surfaceId":"prs","catalogId":"https://agentrq.com/a2ui/v1_0/catalogs/agentrq/catalog.json"}},{"version":"1.0","updateComponents":{"surfaceId":"prs","components":[{"id":"root","component":"Card","child":"col"},{"id":"col","component":"Column","children":["title","chart"]},{"id":"title","component":"Text","text":"## PRs merged"},{"id":"chart","component":"Chart","variant":"bar","label":"PRs","data":[{"label":"Mon","value":2},{"label":"Tue","value":5},{"label":"Wed","value":1},{"label":"Thu","value":4}]}]}}]}}
```
