# @agentrq/agentrq-ws

The AgentRQ workspace client: everything an agent can do in a workspace, from a
shell, without spending an agent's tokens to do it.

```bash
npx -y @agentrq/agentrq-ws@latest help
```

It reads the `.mcp.json` in the current directory — the same file the agent
working there uses — so inside a workspace checkout there is nothing to
configure.

```bash
cd ~/code/my-workspace
npx -y @agentrq/agentrq-ws@latest workspace
```

## Why it exists

An agent talks to its workspace over MCP, which is the right shape for an agent
and the wrong shape for a person: every call is a JSON envelope, and every
answer is tokens the agent pays for. The same eighteen tools are underneath this
CLI, but a command is a line of shell and the answer is plain text.

It is also the difference between reading an attachment and *handling* one — see
below.

## Install

Nothing to install: `npx -y @agentrq/agentrq-ws@latest <command>` fetches it on
demand, and it has no runtime dependencies. To keep it around:

```bash
npm install -g @agentrq/agentrq-ws
agentrq-ws help
```

Node 20.6 or newer.

## Commands

| Command | What it does |
| --- | --- |
| `workspace` | Show the workspace title and mission |
| `task get <taskId>` | Fetch a task, with `--conversation` for its history |
| `task next` | Take the next not-started task — **this dequeues the queue** |
| `task create <title>` | Create a task, with `--clear-context` to send `/clear` to the agent first |
| `task status <taskId> <status>` | Set a task's status |
| `reply <taskId> <text>` | Send a message to a task |
| `attachment get <id> --task <taskId>` | Download an attachment to a file |
| `memory load [name]` | Read a workspace memory (defaults to the index) |
| `memory save [name] --content …` | Replace a workspace memory |
| `memory delete [name]` | Delete a workspace memory |
| `skill search [q]` | Find the skills this workspace can use, by name or description; `--limit`/`--offset` page |
| `skill load <uri>` | Read a skill file (`skill://<name>` reads its `SKILL.md`) |
| `skill save <uri> --content …` | Replace a file of one of this workspace's skills |
| `skill delete <uri>` | Take a skill out of this workspace (`skill://<name>`), deleting it when no other workspace uses it, or delete one of its files |
| `event publish <name>` | Publish a named event |
| `ask <taskId> <message>` | Ask the human a question and wait |
| `site-tools [q]` | List the websites shared with this workspace from the Chrome extension, and their tools' names and descriptions; `q` ranks the tools, `--pattern` filters them by a regular expression, `--limit`/`--offset` page |
| `site-tool <site> <tool>` | Show one shared website's tool in full, with its input schema |
| `call-site-tool <site> <tool> --task <taskId>` | Run a shared website's tool in the human's Chrome, with `--args '{…}'` |
| `tools` | List the tools this workspace server offers |
| `call <tool> --args '{…}'` | Call any tool directly |

Every command takes `--help`, and so does every family:

```bash
agentrq-ws task --help          # lists task get / next / create / status
agentrq-ws attachment get -h    # the one command, its options and defaults
```

`task next` is a separate verb rather than a bare `task get` on purpose: it
claims work, and a command that mutates the queue should say so in its name.

## Attachments never become your problem

The underlying tools speak base64 in both directions. This CLI does not.

**Uploading** takes a path. The filename, media type and encoding are worked out
for you:

```bash
agentrq-ws reply 0isnjTCkpW5 "Build is green — log attached" --attach ./run.log
agentrq-ws task create "Review the design" --body @brief.md --attach ./mock.png
```

**Downloading** writes a file and prints where it went. With no `--out` it lands
in the OS temp directory:

```bash
$ agentrq-ws attachment get 0isp9dJxr85 --task 0isnjTCkpW5
/tmp/agentrq-ws-help.txt
```

`--out` takes a directory (keeping the attachment's own name) or a full path
(renaming it):

```bash
agentrq-ws attachment get 0isp9dJxr85 --task 0isnjTCkpW5 --out ~/Downloads
agentrq-ws attachment get 0isp9dJxr85 --task 0isnjTCkpW5 --out ./report.pdf
```

The `--task` is needed because an attachment belongs to a task. The CLI asks
for the attachment's public link, downloads it, and saves the file under the
name a human gave it; an older attachment with no link arrives the same way,
from its content.

## Long text

Anywhere prose is expected — `--body`, `--content`, `--payload`, and a reply's
text — `@path` reads a file and `-` reads stdin:

```bash
agentrq-ws task create "Post-mortem" --body @notes.md
git log --oneline -20 | agentrq-ws reply 0isnjTCkpW5 -
agentrq-ws memory save release-notes.md --content @CHANGELOG.md
```

## Asking the human something

```bash
# a form
agentrq-ws ask 0isnjTCkpW5 "Which branch should I release from?" \
  --field branch:string:"Branch name" --field sign:boolean:"Sign the tag?"

# or a link to go and do something
agentrq-ws ask 0isnjTCkpW5 "Approve the deploy, then confirm" --url https://example.com/approve
```

Both block until the human answers or the timeout elapses (`--timeout`, one hour
by default and at most).

## Output

Commands print the server's text. `--json` prints the raw result instead, for
piping into `jq`:

```bash
agentrq-ws tools --json | jq -r '.[].name'
```

Failures print one line and exit non-zero. A stack trace means a bug in the CLI,
not a mistake in the command.

## Choosing a server

The nearest `.mcp.json` at or above the working directory is used. When it
defines several servers, name one:

```bash
agentrq-ws workspace --server agentrq-workspace
agentrq-ws workspace --config ../other/.mcp.json
```

| Variable | Effect |
| --- | --- |
| `AGENTRQ_WS_URL` | Use this server URL and ignore `.mcp.json` entirely |
| `AGENTRQ_WS_SERVER` | Which server in `.mcp.json` to use |

## Development

```bash
cd cli/agentrq-ws
npm test                  # 147 tests, no network
npm run test:coverage
```

The tests drive the whole path — argv, config loading, the MCP handshake, SSE
framing, files on disk — with only the socket replaced.

## License

Apache-2.0
