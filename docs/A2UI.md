# Cards from agents

An agent's reply can carry a card beside its text: a laid-out result, a comparison, or a status summary, drawn in the task thread instead of a wall of prose. Cards use [A2UI](https://a2ui.org) v1.0, an open format for UI that an agent describes and the app draws, with its basic catalog of components.

A card can also ask: buttons, and fields you fill in before you press one. An agent can even ask its question as a card and wait for your answer.

## What an agent can draw

| Kind | Components |
|------|------------|
| Text | `Text` (with simple markdown: headings, bold, lists), `Icon` (check, error, warning, info and the rest of the catalog's set), `Divider` |
| Layout | `Card`, `Row`, `Column`, `List` (fixed children, or one child per item of a list in the card's data), `Tabs` |
| Media | `Image`, `Video`, `AudioPlayer`, from files attached to the same reply |
| Inputs | `Button`, `TextField` (a line, a paragraph, a number or a password), `CheckBox`, `ChoicePicker` (one or several options, as chips or a list, with a search), `Slider`, `DateTimeInput`, `Modal` (a button that opens more in a dialog) |

Agents learn this from the workspace skill (`skill://agentrq`, its `references/a2ui.md`) and from `help task.reply`, which shows a complete card. With the command-line client, `agentrq-ws reply <taskId> "text" --ui @card.json --attach chart.png` sends one.

## Answering a card

What you type or pick stays in the card until you press a button. A field's own checks (required, a length, a pattern) hold back a button until they pass, and say why under it.

Pressing a button answers the card, once:

- On a reply's card, the press is posted as your reply, in the button's words, and the agent reads it like any other reply, with the button's name and details.
- On a question asked as a card (`elicit` with `mode: "a2ui"`), the agent gets the button's name, its details and everything you entered.

Then the card stays in the thread read-only, the button you pressed marked, and a question shows what you entered. A card in an archived workspace, on a teammate's task, or a question the agent stopped waiting for takes no answer. A button that opens a link opens only `http` and `https` links, in a new tab, and only when you press it.

## A card that updates

A reply whose card starts with no `createSurface` updates the task's newest card with that `surfaceId` instead of posting a new one: a progress card can move from "building" to "412 tests passed" in place. Components are replaced by id, the card's data is written path by path, and the reply's text replaces the message's text. `deleteSurface` removes the card and leaves the message with its new text.

An update is an edit, not a new message: it sends no push, Slack or email notification. The whole card is checked again after the update, and an update is refused when no card in the task has that id, when the card has been answered, when it is a question asked with `elicit`, or when it attaches files (an updated card shows only the files its first reply attached).

## The text is always there

A reply with a card still needs its text, and the text should say everything the card does. Slack, push notifications, email, the desktop app's notifications and other agents reading the thread show only the text. If a stored card ever fails to load, the thread shows the text in its place.

## Media comes only from your own storage

A card cannot show a picture, video or sound from a link. Its media must be a file attached to the same reply, named as `attachment:<filename>`, and AgentRQ swaps in the stored file's link before saving the card. So a card never makes your browser fetch an address the agent chose, and a card you open later shows the same file it showed when it was posted.

## When a card is refused

The server checks every card against the A2UI v1.0 schemas before it is posted. A card that fails, such as one with an unknown component, a missing `root`, a link as an image, or a button with no action name, is refused as a whole: nothing is posted, no file is kept, and the agent's call returns every problem found, so it can fix the card and send it again.

Limits: one surface per reply, 64 KiB of card JSON, 200 components, and nesting 16 deep.
