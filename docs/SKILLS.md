# Skills

A skill is a playbook an agent loads when a task matches it. It is a directory with a `SKILL.md` and, optionally, other files that the `SKILL.md` points to, such as references, prompts and scripts. It is the same format Claude Code and Antigravity use, so skills written for them work here.

Skills belong to your account, and each workspace has its own switch for each of them. A workspace's agents see a skill only when it is on in that workspace and on for the account. Open a workspace's **Skills** button, beside **Analytics** above its tasks, or its **Settings → Skills**, to see all of them with that workspace's switches.

## A minimal skill

```markdown
---
name: pr-reviewer
description: Use when reviewing a pull request, before leaving any comments.
---

# Reviewing a pull request

1. Read the description, then `references/checklist.md`.
2. Run the tests before commenting on style.
```

- `description` is required. It says what the skill does and when to use it, because an agent decides whether to load a skill from its description.
- `name` is optional and defaults to the skill's directory name.
- Any other frontmatter keys are kept. The body can be empty.

## Rules and limits

| | |
|---|---|
| File name | Exactly `SKILL.md` |
| `name` | Lowercase letters and digits joined by single hyphens, at most 64 characters. Unique in the account |
| `description` | Required, at most 1024 characters, no `<` or `>` |
| `SKILL.md` size | At most 96 KiB |
| Other files | At most 64 KiB each, UTF-8 text only |
| Paths | Relative to the skill. No `..`, no absolute paths, no hidden files, at most 255 characters |
| Files per skill | At most 256, including `SKILL.md` |

Anything over a limit is refused, never truncated. The error says what to change.

## Importing from GitHub or GitLab

On the **Skills** page, or in a workspace's **Settings → Skills**, paste a public GitHub or GitLab link, tick the workspaces to turn the skills on in, and choose **Import**. From a workspace, that workspace is ticked already. Accepted forms:

```
https://github.com/obra/superpowers
https://github.com/obra/superpowers.git
https://github.com/obra/superpowers/tree/main
https://github.com/obra/superpowers/tree/main/skills/systematic-debugging
https://gitlab.com/group/subgroup/project
https://gitlab.com/group/project/-/tree/main/skills/tdd
```

GitLab links are to the one GitLab host the server's admin set, gitlab.com unless they chose another. Groups and subgroups can be any depth. As on GitHub, the first part after `/-/tree/` is the branch, tag or commit, so a branch with a `/` in its name cannot be linked.

**Which directories are skills:**
- If the repository has a plugin manifest, `.agentrq/plugin.json` or else Muse's `.muse-plugin/plugin.json`, its `capabilities.skills` list decides.
- Otherwise every directory with a `SKILL.md` is a skill.

**Which files are kept:**
- every Markdown (`.md`) file in the skill's folder, referenced or not, except a repository's own `README.md`, `CLAUDE.md`, `AGENTS.md`, `GEMINI.md`, `CHANGELOG.md`, `LICENSE.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md` and `SECURITY.md`
- any other file that `SKILL.md` or a kept file references, in turn. A file counts as referenced when it is named by its path, by a relative path, or through a folder written with a trailing `/`.

Everything else is left out, including one of those meta files unless something references it.

**The import report** lists what was imported and, for everything skipped, the reason. For example:
- a `SKILL.md` over 96 KiB, which skips the whole skill
- a binary, symlink or hidden file
- a file nothing references
- a name the account already uses

Every skill in obra/superpowers fits these limits, so importing it brings in all of them.

**Large repositories.** A repository whose download is over 20 MB compressed or 64 MB unpacked, such as garrytan/gstack, is not imported whole. The tab lists its skills instead, with each `SKILL.md`'s size (GitLab does not list sizes, so a GitLab project shows none) and, greyed out, any that are over a limit. Tick the ones you want and choose **Import selected**: only their files are downloaded.

**Overwrite existing** replaces the account's skill of the same name. It stays on in the workspaces it was on in, and is turned on in the ones ticked now.

### Private repositories

When your server's admin has set up a GitHub App (see below), the import shows **Private repositories** with a **Connect GitHub** link. It opens GitHub, where you install the app on your account or an organization and pick the repositories it may read. GitHub then brings you back, and the account is listed beside the import. From then on, a link to one of those repositories imports like a public one. The same connection is used when you import into a team workspace, so each member imports only what they have connected themselves.

- AgentRQ keeps no GitHub token. Each import gets a token from GitHub that reads that one repository, and only its contents, and expires within the hour.
- **Add or change on GitHub** changes which repositories the app reads. **×** beside an account stops importing through it. The app stays installed on GitHub until you uninstall it there.
- If an organization needs its owners' approval, GitHub sends them your request. Connect again once they have approved it.

**Setting up the GitHub App (admins).** Create a GitHub App under your organization's or your own developer settings:
1. **Callback URL**: `<your AgentRQ URL>/github/callback`. Tick **Request user authorization (OAuth) during installation**; without it AgentRQ cannot check who installed the app, and refuses the connect.
2. **Repository permissions**: Contents, read-only. Nothing else is needed, and webhooks can stay off.
3. Generate a client secret and a private key.
4. On **/admin/config → GitHub App**, fill in the App ID, the slug (the name in `github.com/apps/<slug>`), the client ID and secret, and paste the private key. Then turn on **enabled**. The same settings can come from the `AGENTRQ_GITHUB_APP_*` environment variables on first start.

### Private GitLab projects

When your server's admin has set up a GitLab application (see below), the import shows **Private projects** with a **Connect GitLab** link. It opens GitLab, where you allow AgentRQ to read your projects. GitLab then brings you back, and your GitLab username is listed beside the import. From then on, a link to any project you can read on that GitLab imports like a public one, into the account or, with your own connection, into a team workspace.

- The connection reads with the `read_api` scope only. GitLab's access tokens last two hours, so AgentRQ keeps the connection's tokens, encrypted, and renews them when an import needs it.
- **×** beside your username disconnects: AgentRQ forgets the tokens and asks GitLab to revoke them. You can also revoke AgentRQ under **Edit profile → Applications** on GitLab; the next import then reads anonymously.

**Setting up the GitLab application (admins).** On the GitLab host, register an OAuth application under **Edit profile → Applications** (or, for the whole instance, **Admin → Applications**):
1. **Redirect URI**: `<your AgentRQ URL>/gitlab/callback`.
2. **Confidential**: on. **Scopes**: `read_api` only.
3. On **/admin/config → GitLab app**, set the host (gitlab.com by default; a self-managed one such as `gitlab.example.com`), the Application ID as the client ID, and the Secret. Then turn on **enabled**. The same settings can come from the `AGENTRQ_GITLAB_APP_*` environment variables on first start. The host applies to public imports too, even while **enabled** is off.

## Turning a skill on and off

A skill has two kinds of switch:
- **One per workspace.** On, that workspace's agents see the skill. A skill page lists every workspace with its switch, and a workspace's **Settings → Skills** has the switch for that workspace on each skill. A fork uses its parent's switches.
- **One for the account.** Off, no agent sees the skill in any workspace, but it is kept, and so are its workspace switches, for when it is turned back on.

It is one skill wherever it is on, not a copy, so an edit made from any workspace reaches all of them. **Delete** on a skill's page deletes it from the account, and every workspace loses it.

## Skills from your team

**Add** on a team workspace's skill links it into your account; nothing is copied. Your agents load and improve that one skill, the teammate's, and every teammate who added it sees each change. It shows as *shared by a teammate*, with your own workspace switches. When the team stops sharing it with you, your agents no longer find it; when its owner deletes it, the link goes too. If you already have your own skill of that name, **Use the team's instead** on its team page turns yours into the link: its files are deleted, its workspace switches stay, and anyone who linked to yours now links to your teammate's.

Because several people write a shared skill, `skills.load` shows each file's version and `skills.save` must send it to replace the file. A save made on an older version is refused with the version there is now, to read and merge first. Adding a new file needs no version.

Skills from before they belonged to the account were moved to it when the server first started with this version. Each one is on in the workspace that had it and in every workspace it was shared into. When two workspaces had a skill with the same name, the two were merged if their files were the same; otherwise the later one was renamed `<name>-<workspace>`, with its `SKILL.md` changed to match.

## Reading a skill

The Skills tab lists only skills, never their other files. Opening a skill shows its `SKILL.md`. To open another of its files, click the reference to it in the file you are reading. A reference can be:
- a link, `skill://…` or relative
- inline code that names one of the skill's files exactly, like `` `references/checklist.md` ``

A breadcrumb shows where you are and **Back** returns to where you came from.

## `skill://` URIs

`skill://<name>/<path>` addresses one file, for example `skill://systematic-debugging/SKILL.md` or `skill://brainstorming/scripts/helper.js`. `skill://<name>` alone means the skill's `SKILL.md`.

## MCP tools

On the workspace server, which every agent in the workspace connects to:

| Tool | What it does |
|---|---|
| `skills.search(q?, limit?, offset?)` | The skills on in this workspace (and on for the account), each with its description and `skill://` URI. `q` (at least 3 characters) keeps only skills whose name or description contains it, ignoring case; `limit` (at most 100) and `offset` page through the matches, and the answer says how many there are in all. Contents are not included |
| `skills.load(uri)` | One file, as stored. A `SKILL.md` comes with the URIs of the skill's other files |
| `skills.save(uri, content, version?)` | Writes one file of a skill on in this workspace. Writing `SKILL.md` creates or updates the skill; a new one belongs to the account and is on in this workspace. `version` is the one `skills.load` showed: a file of a skill shared with your team can only be replaced with it, and a save on a version someone has replaced since is refused with the version there is now |
| `skills.delete(uri)` | `skill://<name>` turns the skill off in this workspace, and deletes it from the account when no other workspace has it on; `skill://<name>/<path>` deletes one of its files. A `SKILL.md` cannot be deleted on its own |

Agents are told to call `skills.search` at the start of a task and to load the `SKILL.md` of any skill that matches it.

The supervisor's account-wide server has read-only `skills.search(workspaceId, q?, limit?, offset?)` and `skills.get(workspaceId, uri)`.

The REST API searches the same way: `GET /api/v1/skills?q=&limit=&offset=` returns the account's `{skills, total}`, each with the `workspaceIds` it is on in, and `GET /api/v1/workspaces/{id}/skills` the same skills with `workspaceEnabled` for that workspace.
