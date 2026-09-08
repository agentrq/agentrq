# Claude Code plugins

The two plugins AgentRQ publishes for [Claude Code](https://claude.com/claude-code), served
from this repository's own marketplace.

| Plugin | Connects to | For |
|---|---|---|
| [`agentrq`](agentrq/README.md) | the account-level supervisor MCP server | orchestrating work across every workspace you own |
| [`agentrq-workspace`](agentrq-workspace/README.md) | one workspace's own MCP server | working a single workspace's task queue |

```bash
/plugin marketplace add https://github.com/agentrq/agentrq
/plugin install agentrq@agentrq
/plugin install agentrq-workspace@agentrq
```

A Claude marketplace is a git URL rather than a published package, so the manifest that
lists these lives at the repository root — [`.claude-plugin/marketplace.json`](../../.claude-plugin/marketplace.json)
— and its `source` paths point back here.

## Keeping them honest

Each plugin's README and skill document the tools its server offers. Those tables are the
only description an agent reads before deciding what it can do, and they have been wrong in
every direction: a tool that had been removed was still listed, six that existed were not,
and the task statuses were copied from a schema that advertised three values the server
rejects — including a human-in-the-loop instruction that told agents to report being stuck
with a status that fails.

So they are checked rather than trusted. `backend/internal/handler/coremcp/plugin_docs_test.go`
compares every table against the `AddTool` calls in the server it describes, in both
directions, and refuses the specific retired names that have shipped here before. The
[workflow](../../.github/workflows/plugin-claude.yml) runs it on changes to these plugins
*or to either MCP server*, so adding a tool to a server fails the build until it is
documented here.
