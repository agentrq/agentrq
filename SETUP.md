# AgentRQ Self-Hosting Setup Guide

> Agent-friendly guide for setting up AgentRQ locally using Docker.
> Source: [Self-Hosting Docs](https://agentrq.com/docs/howto-self-hosting)

---

## Prerequisites

| Requirement | Details |
|---|---|
| **Docker** | Only runtime dependency. Install from [docs.docker.com/get-docker](https://docs.docker.com/get-docker/) |
| **Google OAuth2 credentials** | Client ID + Client Secret from [Google Cloud Console](https://console.cloud.google.com/apis/credentials) |
| **Domain or localhost** | `http://localhost` for local dev; a public domain for production |

---

## Quick Start (Development / Local)

### 1. Pull the image

```bash
docker pull agentrq/agentrq:latest
```

### 2. Create the `.env` file

Create a `.env` file in your working directory:

```env
# ── App ──────────────────────────────────────────────────
ENV=production
PORT=2026
AGENTRQ_BASE_URL=http://localhost:2026
AGENTRQ_DOMAIN=localhost

# ── TLS ──────────────────────────────────────────────────
# Disabled for local dev
AGENTRQ_SSL_ENABLED=false
AGENTRQ_SSL_LETSENCRYPT_EMAIL=
AGENTRQ_SSL_CACHE_DIR=/_certs
AGENTRQ_SSL_CLOUDFLARE_API_TOKEN=

# ── Database: SQLite (default, zero setup) ───────────────
AGENTRQ_SQLITE_ENABLED=true
AGENTRQ_SQLITE_DSN=./_storage/agentrq.db

# ── Database: PostgreSQL (disabled for local dev) ────────
AGENTRQ_POSTGRES_ENABLED=false
AGENTRQ_POSTGRES_HOST=localhost
AGENTRQ_POSTGRES_PORT=5432
AGENTRQ_POSTGRES_USER=postgres
AGENTRQ_POSTGRES_PASSWORD=yourpassword
AGENTRQ_POSTGRES_DBNAME=agentrq

# ── Authentication ───────────────────────────────────────
AGENTRQ_AUTH_JWT_SECRET=CHANGE-ME-TO-A-LONG-RANDOM-SECRET-32-CHARS-MIN
# Encrypts MCP workspace tokens (AES-256-GCM). Must be exactly 32 bytes.
# CRITICAL: If you change this, all existing MCP tokens become unreadable.
AGENTRQ_AUTH_WORKSPACE_TOKEN_KEY=CHANGE-ME-EXACTLY-32-BYTES-LONG!
# Root login for initial setup (disable after first use)
AGENTRQ_AUTH_ROOT_LOGIN_ENABLED=true
AGENTRQ_AUTH_ROOT_ACCESS_TOKEN=CHANGE-ME-ROOT-TOKEN

# ── Google OAuth2 ────────────────────────────────────────
AGENTRQ_ACCOUNTS_OAUTH2_CLI_GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
AGENTRQ_ACCOUNTS_OAUTH2_CLI_GOOGLE_CLIENT_SECRET=your-client-secret

# ── SMTP (optional) ─────────────────────────────────────
AGENTRQ_SMTP_ENABLED=false
AGENTRQ_SMTP_HOST=smtp.example.com
AGENTRQ_SMTP_PORT=587
AGENTRQ_SMTP_USERNAME=user@example.com
AGENTRQ_SMTP_PASSWORD=yourpassword
AGENTRQ_SMTP_FROM=AgentRQ <no-reply@example.com>

# ── Slack (optional) ────────────────────────────────────
AGENTRQ_SLACK_ENABLED=false
AGENTRQ_SLACK_CLIENT_ID=your-slack-client-id
AGENTRQ_SLACK_CLIENT_SECRET=your-slack-client-secret
AGENTRQ_SLACK_SIGNING_SECRET=your-slack-signing-secret
AGENTRQ_SLACK_APP_ID=your-slack-app-id

# ── Web Push Notifications / PWA (optional) ──────────────
# Generate keys: npx web-push generate-vapid-keys
AGENTRQ_WEBPUSH_VAPID_PUBLIC_KEY=
AGENTRQ_WEBPUSH_VAPID_PRIVATE_KEY=
AGENTRQ_WEBPUSH_SUBSCRIBER=mailto:hi@example.com

# ── DDoS Protection ─────────────────────────────────────
AGENTRQ_DDOS_ENABLED=true
AGENTRQ_DDOS_MAX_REQ_PER_SEC=20
AGENTRQ_DDOS_BLOCK_DURATION=5m

# ── Rate Limiting ───────────────────────────────────────
AGENTRQ_RATELIMIT_ENABLED=true
AGENTRQ_RATELIMIT_MAX_PER_IP=60
AGENTRQ_RATELIMIT_MAX_PER_USER=100
AGENTRQ_RATELIMIT_WINDOW=60s
```

### 3. Create storage directory and run

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

### 4. Verify

```bash
# Check container is running
docker ps --filter name=agentrq

# Check logs
docker logs agentrq

# Open in browser
open http://localhost:2026  # macOS
# or: xdg-open http://localhost:2026  # Linux
```

AgentRQ is now running at **http://localhost:2026**.

---

## Production Setup

### Differences from local dev

| Setting | Local Dev | Production |
|---|---|---|
| `AGENTRQ_BASE_URL` | `http://localhost` | `https://your-domain.com` |
| `AGENTRQ_DOMAIN` | `localhost` | `your-domain.com` |
| `AGENTRQ_SSL_ENABLED` | `false` | `true` |
| `AGENTRQ_SSL_LETSENCRYPT_EMAIL` | (empty) | `you@example.com` |
| `AGENTRQ_AUTH_ROOT_LOGIN_ENABLED` | `true` | `false` |
| `AGENTRQ_AUTH_JWT_SECRET` | any string | **long random secret (32+ chars)** |
| Database | SQLite | PostgreSQL recommended |

### Production `.env` changes

```env
# ── App ──────────────────────────────────────────────────
ENV=production
PORT=80
AGENTRQ_BASE_URL=https://your-domain.com
AGENTRQ_DOMAIN=your-domain.com

# ── TLS (built-in Let's Encrypt) ─────────────────────────
AGENTRQ_SSL_ENABLED=true
AGENTRQ_SSL_LETSENCRYPT_EMAIL=you@example.com
AGENTRQ_SSL_CACHE_DIR=/_certs
# Optional: Cloudflare DNS challenge for wildcard certs
AGENTRQ_SSL_CLOUDFLARE_API_TOKEN=

# ── Database: PostgreSQL (recommended for production) ────
AGENTRQ_SQLITE_ENABLED=false
AGENTRQ_POSTGRES_ENABLED=true
AGENTRQ_POSTGRES_HOST=your-postgres-host
AGENTRQ_POSTGRES_PORT=5432
AGENTRQ_POSTGRES_USER=postgres
AGENTRQ_POSTGRES_PASSWORD=your-secure-password
AGENTRQ_POSTGRES_DBNAME=agentrq

# ── Authentication ───────────────────────────────────────
AGENTRQ_AUTH_JWT_SECRET=USE-A-CRYPTOGRAPHICALLY-RANDOM-STRING-HERE
AGENTRQ_AUTH_ROOT_LOGIN_ENABLED=false
AGENTRQ_AUTH_ROOT_ACCESS_TOKEN=CHANGE-ME
```

### Production docker run (with TLS)

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

> **Note:** Ports 80 and 443 must be reachable from the internet for Let's Encrypt certificate provisioning.

---

## Google OAuth2 Setup

This is required for user authentication. Follow these steps:

1. Go to [Google Cloud Console → APIs & Services → Credentials](https://console.cloud.google.com/apis/credentials)
2. Click **+ Create Credentials** → **OAuth 2.0 Client ID**
3. Application type: **Web application**
4. Add **Authorized Redirect URI**:
   - Production: `https://your-domain.com/api/v1/auth/google/callback`
   - Local dev: `http://localhost:2026/api/v1/auth/google/callback`
5. Click **Create** — copy the Client ID and Client Secret into your `.env`:
   - `AGENTRQ_ACCOUNTS_OAUTH2_CLI_GOOGLE_CLIENT_ID=<Client ID>`
   - `AGENTRQ_ACCOUNTS_OAUTH2_CLI_GOOGLE_CLIENT_SECRET=<Client Secret>`

> Google allows multiple redirect URIs per credential, so you can use the same credential for both local and production.

---

## Web Push Notifications (PWA)

AgentRQ is a PWA and can send native push notifications to your device when agents create tasks, update task status, or reply. This is optional — the app works fully without it.

### 1. Generate VAPID keys

VAPID keys identify your server to push services (Google, Apple, Mozilla). Generate them once:

```bash
npx web-push generate-vapid-keys
```

Output:
```
Public Key:
BEl62iUYgUivxIkv69yViEuiBIa-Ib9-SkvMeAtA3LFgDzkrxZJjSgSnfckjBJuBkr3qBUYIHBQFLXYp5Nksh8U

Private Key:
UUxI4O8-FbRouAevSmBQ6co-ocp-k_2Wah5mfWFkQ58
```

Keep the **Private Key** secret — treat it like a password.

### 2. Add to your `.env`

```env
AGENTRQ_WEBPUSH_VAPID_PUBLIC_KEY=<Public Key from above>
AGENTRQ_WEBPUSH_VAPID_PRIVATE_KEY=<Private Key from above>
# Contact URI sent to push services (mailto: or https:). Required by VAPID spec.
AGENTRQ_WEBPUSH_SUBSCRIBER=mailto:hi@example.com
```

### 3. How it works

Once configured, users visiting the site will be prompted to allow notifications. Each device subscribes per workspace — notifications only arrive for workspaces the user subscribed to on that device.

**Notification types** users receive:
| Event | Title format |
|---|---|
| Agent opens a new task | `New task: <title>` |
| Agent changes task status | `Task COMPLETED: <title>` |
| Agent sends a reply | `Reply on: <title>` (body = first 100 chars of reply) |

> If `AGENTRQ_WEBPUSH_VAPID_PUBLIC_KEY` is not set, push notifications are silently disabled — no errors, no prompts.

---

## Connecting Claude Code / MCP Clients

After setup, sign into your AgentRQ instance, create a workspace, and copy the MCP token. Then configure `.mcp.json`:

```json
{
  "mcpServers": {
    "agentrq": {
      "type": "http",
      "url": "https://WORKSPACE_ID.mcp.your-domain.com/mcp?token=YOUR_TOKEN"
    }
  }
}
```

For local dev (no TLS):

```json
{
  "mcpServers": {
    "agentrq": {
      "type": "http",
      "url": "http://WORKSPACE_ID.mcp.localhost:2026/mcp?token=YOUR_TOKEN"
    }
  }
}
```

---

## Common Operations

```bash
# Stop
docker stop agentrq

# Start (after stop)
docker start agentrq

# Restart
docker restart agentrq

# Remove container
docker stop agentrq && docker rm agentrq

# Update to latest version
docker stop agentrq && docker rm agentrq
docker pull agentrq/agentrq:latest
# Then re-run the docker run command above

# View logs (follow mode)
docker logs -f agentrq

# View last 100 lines
docker logs --tail 100 agentrq
```

---

## Database Notes

| | SQLite | PostgreSQL |
|---|---|---|
| **Setup** | Zero config, default | Requires external Postgres instance |
| **Best for** | Personal use, small teams | Multi-user teams, high concurrency |
| **Storage** | Single file at `_storage/agentrq.db` | External database server |
| **Backups** | Copy the `_storage/` directory | Use `pg_dump` or managed backups |
| **Schema** | Auto-migrated on startup | Auto-migrated on startup |

### Important

- **File attachments** are stored in `_storage/` regardless of database choice — always back up this directory.
- **`AGENTRQ_AUTH_JWT_SECRET`** signs user session JWTs. Changing it invalidates all active sessions.
- **`AGENTRQ_AUTH_WORKSPACE_TOKEN_KEY`** encrypts MCP tokens via AES-256-GCM. Must be exactly 32 bytes. **If you change it, all existing workspace MCP tokens become unreadable and must be regenerated.** Back it up along with your database.
- **Never commit `.env`** to version control — it contains secrets. Add it to `.gitignore`.

---

## Environment Variable Reference

| Variable | Required | Default | Description |
|---|---|---|---|
| `ENV` | Yes | `production` | Environment mode |
| `PORT` | Yes | `3000` | HTTP listen port |
| `AGENTRQ_BASE_URL` | Yes | — | Full public URL (e.g. `https://your-domain.com`) |
| `AGENTRQ_DOMAIN` | Yes | — | Domain without protocol (e.g. `your-domain.com`) |
| `AGENTRQ_BASE_PATH` | No | — | Base path prefix when behind a reverse proxy (e.g., `/abc/def`) |
| `AGENTRQ_PROXY_DOMAIN` | No | — | Public domain name of the reverse proxy (e.g., `example.com`) |
| `AGENTRQ_COOKIE_SECURE` | No | — | Force cookie Secure flag for TLS-terminating proxies (`true`/`false`) |
| `AGENTRQ_SSL_ENABLED` | No | `false` | Enable built-in TLS via Let's Encrypt |
| `AGENTRQ_SSL_LETSENCRYPT_EMAIL` | If TLS | — | Email for Let's Encrypt registration |
| `AGENTRQ_SSL_CACHE_DIR` | No | `/_certs` | Directory for TLS certificate cache |
| `AGENTRQ_SSL_CLOUDFLARE_API_TOKEN` | No | — | Cloudflare API token for DNS challenge |
| `AGENTRQ_SQLITE_ENABLED` | No | `true` | Use SQLite as database backend |
| `AGENTRQ_SQLITE_DSN` | If SQLite | `./_storage/agentrq.db` | Path to SQLite database file |
| `AGENTRQ_POSTGRES_ENABLED` | No | `false` | Use PostgreSQL as database backend |
| `AGENTRQ_POSTGRES_HOST` | If Postgres | — | PostgreSQL host |
| `AGENTRQ_POSTGRES_PORT` | If Postgres | `5432` | PostgreSQL port |
| `AGENTRQ_POSTGRES_USER` | If Postgres | — | PostgreSQL user |
| `AGENTRQ_POSTGRES_PASSWORD` | If Postgres | — | PostgreSQL password |
| `AGENTRQ_POSTGRES_DBNAME` | If Postgres | `agentrq` | PostgreSQL database name |
| `AGENTRQ_AUTH_JWT_SECRET` | Yes | — | Secret for signing session JWTs (32+ chars) |
| `AGENTRQ_AUTH_WORKSPACE_TOKEN_KEY` | Yes | — | AES-256-GCM key for MCP token encryption (exactly 32 bytes) |
| `AGENTRQ_AUTH_ROOT_LOGIN_ENABLED` | No | `false` | Enable root login bypass for initial setup |
| `AGENTRQ_AUTH_ROOT_ACCESS_TOKEN` | If root | — | Root login token |
| `AGENTRQ_ACCOUNTS_OAUTH2_CLI_GOOGLE_CLIENT_ID` | Yes | — | Google OAuth2 Client ID |
| `AGENTRQ_ACCOUNTS_OAUTH2_CLI_GOOGLE_CLIENT_SECRET` | Yes | — | Google OAuth2 Client Secret |
| `AGENTRQ_SMTP_ENABLED` | No | `false` | Enable email notifications |
| `AGENTRQ_SMTP_HOST` | If SMTP | — | SMTP server host |
| `AGENTRQ_SMTP_PORT` | If SMTP | `587` | SMTP server port |
| `AGENTRQ_SMTP_USERNAME` | If SMTP | — | SMTP username |
| `AGENTRQ_SMTP_PASSWORD` | If SMTP | — | SMTP password |
| `AGENTRQ_SMTP_FROM` | If SMTP | — | From address for emails |
| `AGENTRQ_SLACK_ENABLED` | No | `false` | Enable Slack integration |
| `AGENTRQ_SLACK_CLIENT_ID` | If Slack | — | Slack app Client ID |
| `AGENTRQ_SLACK_CLIENT_SECRET` | If Slack | — | Slack app Client Secret |
| `AGENTRQ_SLACK_SIGNING_SECRET` | If Slack | — | Slack app Signing Secret |
| `AGENTRQ_SLACK_APP_ID` | If Slack | — | Slack App ID |
| `AGENTRQ_WEBPUSH_VAPID_PUBLIC_KEY` | No | — | VAPID public key for Web Push (enables push notifications when set) |
| `AGENTRQ_WEBPUSH_VAPID_PRIVATE_KEY` | No | — | VAPID private key for Web Push (keep secret) |
| `AGENTRQ_WEBPUSH_SUBSCRIBER` | No | `mailto:hi@example.com` | Contact URI sent to push services (required by VAPID spec) |
| `AGENTRQ_DDOS_ENABLED` | No | `true` | Enable DDoS protection |
| `AGENTRQ_DDOS_MAX_REQ_PER_SEC` | No | `20` | Max requests per second before blocking |
| `AGENTRQ_DDOS_BLOCK_DURATION` | No | `5m` | How long to block offending IPs |
| `AGENTRQ_RATELIMIT_ENABLED` | No | `true` | Enable rate limiting |
| `AGENTRQ_RATELIMIT_MAX_PER_IP` | No | `60` | Max requests per window per IP |
| `AGENTRQ_RATELIMIT_MAX_PER_USER` | No | `100` | Max requests per window per user |
| `AGENTRQ_RATELIMIT_WINDOW` | No | `60s` | Rate limit window duration |
