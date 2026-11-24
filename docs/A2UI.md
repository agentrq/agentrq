# Cards from agents

An agent's reply can carry a card beside its text: a laid-out result, a comparison, or a status summary, drawn in the task thread instead of a wall of prose. Cards use [A2UI](https://a2ui.org) v1.0, an open format for UI that an agent describes and the app draws, with its basic catalog of components.

A card only shows information for now. Buttons and form fields come later.

## What an agent can draw

| Kind | Components |
|------|------------|
| Text | `Text` (with simple markdown: headings, bold, lists), `Icon` (check, error, warning, info and the rest of the catalog's set), `Divider` |
| Layout | `Card`, `Row`, `Column`, `List` (fixed children, or one child per item of a list in the card's data), `Tabs` |
| Media | `Image`, `Video`, `AudioPlayer`, from files attached to the same reply |

Agents learn this from the workspace skill (`skill://agentrq`, its `references/a2ui.md`) and from `help task.reply`, which shows a complete card. With the command-line client, `agentrq-ws reply <taskId> "text" --ui @card.json --attach chart.png` sends one.

## The text is always there

A reply with a card still needs its text, and the text should say everything the card does. Slack, push notifications, email, the desktop app's notifications and other agents reading the thread show only the text. If a stored card ever fails to load, the thread shows the text in its place.

## Media comes only from your own storage

A card cannot show a picture, video or sound from a link. Its media must be a file attached to the same reply, named as `attachment:<filename>`, and AgentRQ swaps in the stored file's link before saving the card. So a card never makes your browser fetch an address the agent chose, and a card you open later shows the same file it showed when it was posted.

## When a card is refused

The server checks every card against the A2UI v1.0 schemas before it is posted. A card that fails, such as one with an unknown component, a missing `root`, a link as an image, or a button, is refused as a whole: nothing is posted, no file is kept, and the agent's call returns every problem found, so it can fix the card and send it again.

Limits: one surface per reply, 64 KiB of card JSON, 200 components, and nesting 16 deep.
