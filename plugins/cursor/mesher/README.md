# Mesher plugin for Cursor

[Mesher](https://mesher.sh) runs connectors (Gmail, GitHub, a remote MCP server, anything
you install) behind **one** MCP tool, `callTool`, which takes a command line. This plugin
connects Cursor to the hosted server at `https://app.mesher.sh/mcp` and teaches the agent
the command grammar.

Install it from the Cursor Marketplace: open **Customize** in the sidebar, find **Mesher**
and select **Install**. The marketplace manifest is
[`.cursor-plugin/marketplace.json`](../../../.cursor-plugin/marketplace.json) at the
repository root, and its `source` points back here.

It asks nothing at install; Cursor opens the Mesher sign-in the first time a tool is used.
For a Mesher you run yourself (`mesher serve`), add its `/mcp` URL to `~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "mesher": { "url": "http://127.0.0.1:8080/mcp" }
  }
}
```

To try a change locally, copy this folder to `~/.cursor/plugins/local/mesher` and run
**Developer: Reload Window**.

## Skills

| Skill | For |
| --- | --- |
| `mesher` | Calling connectors: the `callTool` command grammar, finding a connector and its tools, signing in, profiles, and the built-in `manage` connector |
| `mesher-remote-mcp` | Installing a service's own MCP server in Mesher by URL, signing in to it, and reaching its tools like any connector's |

They are copies of the [Claude Code plugin's](../../claude/mesher/README.md), and a test
fails if they drift.

## Tools

| Tool | What it does |
| --- | --- |
| `callTool` | Runs one Mesher command line, such as `gmail.searchTools 'unread' 10` or `gmail.tools sendEmail -b '{"to":"ada@example.com","subject":"Hi","text":"Hello"}'`. One call reaches any installed connector. |

[Privacy Policy](https://agentrq.com/privacy)
