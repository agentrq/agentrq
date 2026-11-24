# A2UI cards

> Read before changing the `ui` of `task reply`, `service/a2ui`, or how a card is shown.

- **`text` stays required beside `ui`**: Slack, push, email, the desktop notification and agents reading the thread show only the text. A card without it reaches none of them.
- **A card's media is only `attachment:<filename>` of a file in the same reply**, resolved to our stored link before it is stored. A link or a binding would make the page fetch whatever the agent named.
- **Our own implementation, no `@a2ui/*` packages**: the human's decision (a 14 MB pre-1.0 dependency for one renderer). Agents reading a thread get `"ui": true`, never the card's JSON.
