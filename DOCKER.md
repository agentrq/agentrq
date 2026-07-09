# AgentRQ Docker Image

AgentRQ is an agent-human collaboration platform built on the Model Context Protocol (MCP). This image runs the full stack — Go backend API + Vue.js frontend — in a single, minimal container.

## Quick Start

```bash
mkdir -p _storage
chmod 0777 _storage

docker run -d \
  --name agentrq \
  --restart unless-stopped \
  -p 2026:2026 \
  --env-file .env \
  -v ./_storage:/_storage \
  agentrq/agentrq:latest
```

Open **http://localhost:2026** in your browser.

## Tags

| Tag | Description |
|---|---|
| `latest` | Most recent build from `main` |
| `<git-sha>` | Immutable build pinned to a specific commit |

## Environment Variables

### Required

| Variable | Description |
|---|---|
| `AGENTRQ_BASE_URL` | Full public URL (e.g. `http://localhost:2026` or `https://your-domain.com`) |
| `AGENTRQ_DOMAIN` | Domain without protocol (e.g. `localhost` or `your-domain.com`) |
| `AGENTRQ_BASE_PATH` | Base path prefix when behind a reverse proxy (e.g., `/abc/def`) |
| `AGENTRQ_PROXY_DOMAIN` | Public domain name of the reverse proxy (e.g., `example.com`) |
| `AGENTRQ_COOKIE_SECURE` | Force cookie Secure flag for TLS-terminating proxies (`true`/`false`) |
| `AGENTRQ_AUTH_JWT_SECRET` | Secret for signing session JWTs — use a random 32+ character string |
| `AGENTRQ_AUTH_WORKSPACE_TOKEN_KEY` | AES-256-GCM key for MCP token encryption — must be **exactly 32 bytes** |
| `AGENTRQ_ACCOUNTS_OAUTH2_CLI_GOOGLE_CLIENT_ID` | Google OAuth2 Client ID |
| `AGENTRQ_ACCOUNTS_OAUTH2_CLI_GOOGLE_CLIENT_SECRET` | Google OAuth2 Client Secret |

### Optional — TLS (built-in Let's Encrypt)

| Variable | Default | Description |
|---|---|---|
| `AGENTRQ_SSL_ENABLED` | `false` | Enable built-in TLS |
| `AGENTRQ_SSL_LETSENCRYPT_EMAIL` | — | Email for certificate registration |
| `AGENTRQ_SSL_CACHE_DIR` | `/_certs` | Directory for TLS certificate cache |
| `AGENTRQ_SSL_CLOUDFLARE_API_TOKEN` | — | Cloudflare API token for DNS-01 challenge |

### Optional — Database

SQLite is used by default (zero config). Switch to PostgreSQL for production workloads.

| Variable | Default | Description |
|---|---|---|
| `AGENTRQ_SQLITE_ENABLED` | `true` | Use SQLite |
| `AGENTRQ_SQLITE_DSN` | `./_storage/agentrq.db` | SQLite file path |
| `AGENTRQ_POSTGRES_ENABLED` | `false` | Use PostgreSQL |
| `AGENTRQ_POSTGRES_HOST` | — | PostgreSQL host |
| `AGENTRQ_POSTGRES_PORT` | `5432` | PostgreSQL port |
| `AGENTRQ_POSTGRES_USER` | — | PostgreSQL user |
| `AGENTRQ_POSTGRES_PASSWORD` | — | PostgreSQL password |
| `AGENTRQ_POSTGRES_DBNAME` | `agentrq` | PostgreSQL database name |

### Optional — SMTP

| Variable | Default | Description |
|---|---|---|
| `AGENTRQ_SMTP_ENABLED` | `false` | Enable email notifications |
| `AGENTRQ_SMTP_HOST` | — | SMTP host |
| `AGENTRQ_SMTP_PORT` | `587` | SMTP port |
| `AGENTRQ_SMTP_USERNAME` | — | SMTP username |
| `AGENTRQ_SMTP_PASSWORD` | — | SMTP password |
| `AGENTRQ_SMTP_FROM` | — | From address |

### Optional — Web Push Notifications (PWA)

| Variable | Default | Description |
|---|---|---|
| `AGENTRQ_WEBPUSH_VAPID_PUBLIC_KEY` | — | VAPID public key — enables push when set |
| `AGENTRQ_WEBPUSH_VAPID_PRIVATE_KEY` | — | VAPID private key (keep secret) |
| `AGENTRQ_WEBPUSH_SUBSCRIBER` | `mailto:hi@example.com` | Contact URI required by VAPID spec |

Generate VAPID keys with: `npx web-push generate-vapid-keys`

### Optional — Slack

| Variable | Default | Description |
|---|---|---|
| `AGENTRQ_SLACK_ENABLED` | `false` | Enable Slack integration |
| `AGENTRQ_SLACK_CLIENT_ID` | — | Slack app Client ID |
| `AGENTRQ_SLACK_CLIENT_SECRET` | — | Slack app Client Secret |
| `AGENTRQ_SLACK_SIGNING_SECRET` | — | Slack app Signing Secret |
| `AGENTRQ_SLACK_APP_ID` | — | Slack App ID |

## Volumes

| Path | Description |
|---|---|
| `/_storage` | SQLite database and file attachments — **always back this up** |
| `/_certs` | TLS certificate cache (only needed when `AGENTRQ_SSL_ENABLED=true`) |

## Ports

| Port | Description |
|---|---|
| `80` | HTTP (production with TLS) |
| `443` | HTTPS (production with TLS) |
| `2026` | HTTP (local dev default) |
| `3000` | HTTP (internal default, override with `PORT`) |

## Example `.env`

```env
ENV=production
PORT=2026
AGENTRQ_BASE_URL=http://localhost:2026
AGENTRQ_DOMAIN=localhost

AGENTRQ_SSL_ENABLED=false

AGENTRQ_SQLITE_ENABLED=true
AGENTRQ_SQLITE_DSN=./_storage/agentrq.db

AGENTRQ_AUTH_JWT_SECRET=CHANGE-ME-TO-A-LONG-RANDOM-SECRET-32-CHARS-MIN
AGENTRQ_AUTH_WORKSPACE_TOKEN_KEY=CHANGE-ME-EXACTLY-32-BYTES-LONG!
AGENTRQ_AUTH_ROOT_LOGIN_ENABLED=true
AGENTRQ_AUTH_ROOT_ACCESS_TOKEN=CHANGE-ME-ROOT-TOKEN

AGENTRQ_ACCOUNTS_OAUTH2_CLI_GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
AGENTRQ_ACCOUNTS_OAUTH2_CLI_GOOGLE_CLIENT_SECRET=your-client-secret
```

## Production (with TLS)

```bash
mkdir -p _storage _certs
chmod 0777 _storage _certs

docker run -d \
  --name agentrq \
  --restart unless-stopped \
  -p 80:80 -p 443:443 \
  --env-file .env \
  -v ./_storage:/_storage \
  -v ./_certs:/_certs \
  agentrq/agentrq:latest
```

Set `AGENTRQ_SSL_ENABLED=true`, `AGENTRQ_BASE_URL=https://your-domain.com`, and `AGENTRQ_SSL_LETSENCRYPT_EMAIL=you@example.com` in your `.env`.

## Reverse Proxy (Path constraint & TLS termination)

If deploying behind a reverse proxy that enforces a path constraint (e.g. `https://example.com/abc/def/`):

1. Set `AGENTRQ_BASE_PATH=/abc/def` in your `.env`.
2. Set `AGENTRQ_BASE_URL=https://example.com/abc/def` in your `.env`.
3. Set `AGENTRQ_PROXY_DOMAIN=example.com` in your `.env` (this allows requests matching the proxy host to pass through validation).
4. If the proxy terminates SSL (AgentRQ runs on HTTP but proxy handles TLS), set `AGENTRQ_COOKIE_SECURE=true` to keep cookies secure.
5. Simply run the standard pre-built image (e.g. `agentrq/agentrq:latest`) — base path injection and relative asset paths are resolved dynamically at runtime!

## Full Setup Guide

See [SETUP.md](https://github.com/agentrq/agentrq/blob/main/SETUP.md) for the complete self-hosting guide including Google OAuth2 setup, production configuration, and MCP client connection instructions.

## Source

[github.com/agentrq/agentrq](https://github.com/agentrq/agentrq) — Apache-2.0
