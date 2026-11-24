# A2UI cards

> Read before changing the `ui` of `task reply`, `service/a2ui`, a card's actions, or how a card is shown.

- **`text` stays required beside `ui`**: Slack, push, email, the desktop notification and agents reading the thread show only the text. A card without it reaches none of them.
- **A card's media is only `attachment:<filename>` of a file in the same reply**, resolved to our stored link before it is stored. A link or a binding would make the page fetch whatever the agent named.
- **Our own implementation, no `@a2ui/*` packages**: the human's decision (a 14 MB pre-1.0 dependency for one renderer). Agents reading a thread get `"ui": true`, never the card's JSON.
- **`frontend/src/a2ui/validate.js` re-checks a stored card** (old or hand-edited rows); refused → show the text. It cannot know the bucket's host, so it takes any absolute `http(s):` media link besides `/storage/` and the attachment route: a server without TLS (local, a LAN install) builds `http:` public links, and refusing them showed only the text. `openUrl` opens nothing unless the call comes from a click (`userActivated`): a value drawn on the page must never navigate.
- **The renderer (`components/a2ui/`) draws media only from our own relative routes, or a link equal to one of the same message's attachments**, never any `https:` link. A Text draws no image or link: a bound value would otherwise make the page fetch it. `A2uiNode` picks a component when it draws, never from a map built at load: the containers import it back.
- **A Button's click is the person's reply** (`POST …/a2ui/actions`), its metadata `{"a2uiAction": …}` and **never a `type`**: typed metadata reads as no conversation (awaiting reply, the agent's queue). The card is marked `answered` by a conditional update (`AnswerMessage`), so of two clicks at once only one posts.
