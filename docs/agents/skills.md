# Skills

> Workspace skills: `service/skill` (rules), `service/skillimport` (GitHub),
> `controller/crud/skill.go`, the MCP tools in `controller/mcp/skill.go`, and
> the Skills tab (`frontend/src/composables/useSkills.js`). Read before changing any of them.

- **The database holds metadata only; file content is in `service/storage`.** Write the blob before the row and delete it if the row fails. Purge a replaced blob only after the commit, or a crash leaves a row pointing at nothing.
- **Skill blobs go through the controller's `skillStorage`, never `storage`.** `AGENTRQ_SKILLS_STORAGE=s3` points only that one at a bucket; attachments have their own switch. A blob is keyed `w-<workspace>/skill-<skill>/<blob>` on both, under `<storage.dir>/skills/` locally, and is public: see [storage.md](storage.md).
- **The file is exactly `SKILL.md`.** It is capped at 96 KiB and every other file at 64 KiB. An oversized file is refused, never truncated, so a caller can decide what to cut.
- **Names are canonicalised at the boundary, and uniqueness counts shared-in skills.** A share that would give the target two skills with one name is refused, or `skill://<name>` would be ambiguous there.
- **A share is a live reference, read-only in the target.** Only the owning workspace writes; the refusal names it so an agent knows where to go.
- **The importer never fetches a URL it was given.** It builds the codeload and API URLs from owner/repo/ref, which prevents SSRF. It downloads one tarball, because the contents API allows 60 unauthenticated calls an hour.
- **A repository too large for the tarball is listed, not refused.** One trees API call offers its skills as `candidates`; a chosen import reads only what the tarball path would keep, file by file from raw.githubusercontent.com. Reading every file instead is what the size cap exists to prevent.
- **An import reads `.agentrq/plugin.json`, then `.muse-plugin/plugin.json`, before scanning for `SKILL.md`.** It keeps the skill's Markdown files, less repo meta files (README, CLAUDE, AGENTS…), and any other file only if a kept file references it. Loosening that pulls in scripts, fixtures and binaries no agent reads.
- **The Skills tab lists only skills; a skill's own page lists its files** beside the one being read (the human's call, reversing an earlier rule). References in the file on screen open files too; inline code counts only on an exact match with an existing file, because that is how real skills refer to theirs.
- **A skill turned off is hidden only from agents.** It is stored as `disabled` so every skill is on with no default; `skillStore` (not the controller) hides it from `searchSkills`/`loadSkill` and refuses agent writes, because the Skills tab reads the same controller. A re-import keeps it off.
- **Tool lists:** a skills MCP tool is a new tool like any other; see [mcp-and-tasks.md](mcp-and-tasks.md) for the places it must be added.
