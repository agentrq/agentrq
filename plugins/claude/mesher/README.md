# Mesher plugin for Claude Code

[Mesher](https://mesher.sh) runs connectors (Gmail, GitHub, a remote MCP server, anything
you install) behind **one** MCP tool, `callTool`, which takes a command line. This plugin
connects Claude Code to the hosted server at `https://app.mesher.sh/mcp` and teaches the
agent the command grammar.

Installed from this repository's marketplace:

```bash
/plugin marketplace add https://github.com/agentrq/agentrq
/plugin install mesher@agentrq
```

It asks nothing at install. Claude Code opens the Mesher sign-in the first time a tool is
used. For a Mesher you run yourself (`mesher serve`), add its `/mcp` URL as an MCP server
in `.mcp.json` instead and keep this plugin for its skills.

## Skills

| Skill | For |
| --- | --- |
| `mesher` | Calling connectors: the `callTool` command grammar, finding a connector and its tools, signing in, profiles, and the built-in `manage` connector |
| `mesher-remote-mcp` | Installing a service's own MCP server in Mesher by URL, signing in to it, and reaching its tools like any connector's |

## Permissions

A plugin cannot pre-approve its own tools, so Claude Code asks before each call until you
allow them. One wildcard in `.claude/settings.local.json` covers every tool the server
offers:

```json
{
  "permissions": {
    "allow": ["mcp__plugin_mesher_mesher__*"]
  }
}
```

`callTool` reaches every installed connector, including ones that send mail or delete
data, so allow it only if you trust what is installed.

## Tools

| Tool | What it does |
| --- | --- |
| `callTool` | Runs one Mesher command line, such as `gmail.searchTools 'unread' 10` or `gmail.tools sendEmail -b '{"to":"ada@example.com","subject":"Hi","text":"Hello"}'`. One call reaches any installed connector. |

The server also offers resources and prompts, including those of the built-in `manage`
connector (`manage.resources`, `manage.prompts`).

More: [mesher.sh](https://mesher.sh) and [github.com/agentrq/mesher](https://github.com/agentrq/mesher).

[Privacy Policy](https://agentrq.com/privacy)
