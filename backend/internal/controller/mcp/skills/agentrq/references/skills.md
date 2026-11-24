# skills

Playbooks for kinds of task, the same format as Claude Code skills.

- `search`: `q` matches a skill's name or description; each result has its `SKILL.md`'s `skill://` URI. Call it at the start of a task.
- `load`: one file by `uri` (`skill://<name>/<path>`; `skill://<name>` alone reads its `SKILL.md`). Load a matching skill's `SKILL.md` and follow it; load its other files only when it points you to them.
- `save`: `uri` and `content`; replaces the file whole. Writing `SKILL.md` creates or updates the skill: it starts with YAML frontmatter holding a `description`. A skill shared with a team needs the `version` `load` showed.
- `delete`: a file, or the whole skill from this workspace.

When you improve a skill this workspace uses, save it so the next agent gets the better one.

## Examples

```json
{ "action": "search", "payload": { "q": "release" } }
{ "action": "load", "payload": { "uri": "skill://release-notes" } }
{ "action": "save", "payload": { "uri": "skill://release-notes/SKILL.md", "content": "---\ndescription: Write release notes from merged PRs.\n---\n# Release notes\n..." } }
{ "action": "delete", "payload": { "uri": "skill://release-notes/references/old.md" } }
```
