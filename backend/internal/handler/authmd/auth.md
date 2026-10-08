# AgentRQ auth.md

How an agent or MCP client gets access to AgentRQ. Every credential acts for
one person, who approves it in their browser; an agent cannot register an
account or obtain a token on its own, so there is no `agent_auth` block in the
authorization server metadata.

## Who this is for

MCP clients and coding agents (Claude Code, Cursor, Gemini CLI and the like)
that connect to one of AgentRQ's two MCP servers on a person's behalf:

| Server | Endpoint | Scope | What it can do |
|---|---|---|---|
| Workspace | `{{BASE}}/mcp/{workspaceId}` | `mcp` | Act inside one workspace: its tasks, messages, memory and skills |
| Supervisor | `{{BASE}}/mcp` | `supervisor-mcp` | Act across every workspace on the account |

`{workspaceId}` is the workspace's base62 ID, shown in the AgentRQ web app.

## Discovery

Both servers follow the MCP authorization spec. An unauthenticated request
answers `401` with a `WWW-Authenticate: Bearer` challenge naming the
protected resource metadata (RFC 9728), which names the authorization server
whose metadata (RFC 8414) lists every endpoint below.

| | Workspace | Supervisor |
|---|---|---|
| Protected resource metadata | `{{BASE}}/.well-known/oauth-protected-resource/mcp/{workspaceId}` | `{{BASE}}/.well-known/oauth-protected-resource/mcp` |
| Authorization server metadata | `{{BASE}}/.well-known/oauth-authorization-server/mcp/{workspaceId}` | `{{BASE}}/.well-known/oauth-authorization-server` |

## Registration

Clients are public: no client secret is issued, and the token endpoint takes
`token_endpoint_auth_method` `none`. A client identifies itself one of two ways.

- **Client ID Metadata Document.** Use an `https` URL that serves the client's
  metadata as its `client_id`. Nothing to register first.
- **Dynamic Client Registration (RFC 7591).** `POST` JSON with `client_name`
  and `redirect_uris` to `{{BASE}}/mcp/{workspaceId}/oauth2/register` or
  `{{BASE}}/mcp/oauth2/register`, and use the `client_id` it returns.

A `redirect_uri` must be one the client registered; loopback addresses match
on any port (RFC 8252).

## Getting a credential

The authorization code grant, with a person in the loop:

1. Send the person's browser to `/oauth2/authorize` under the server's path
   with `response_type=code`, `client_id`, `redirect_uri` and `state`.
2. If they are not signed in, they sign in to AgentRQ first.
3. They see a consent page naming the client and what it will be able to do,
   and press Allow or Deny. Allow returns `code` to the `redirect_uri`; Deny
   returns `error=access_denied`.
4. `POST` `grant_type=authorization_code&code=…` to `/oauth2/token` under the
   same path.

The response carries `access_token`, `refresh_token`, `token_type` `bearer`,
`expires_in` and `scope`. To renew, `POST`
`grant_type=refresh_token&refresh_token=…` to the same endpoint.

Each server has one scope, listed above. A client may leave `scope` out; any
scope it asks for is granted as the server's own.

## Using the credential

Send it as `Authorization: Bearer <access_token>` on every request to the MCP
endpoint. A token works only on the server, and for a workspace token the
workspace, it was issued for.

## Not supported

- Agent self-registration (`agent_auth`: anonymous, verified email or
  identity assertion flows). A person always approves access.
- The `client_credentials` grant: every token acts for a person.
