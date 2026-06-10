# AgentRQ Quick Start Guide

> **Want to get running in 3 minutes?** This is your guide.

## Prerequisites

- **Docker** & **Docker Compose** ([install](https://docs.docker.com/get-docker/))
- **Google OAuth2 credentials** ([get them](https://console.cloud.google.com/apis/credentials))

## Quick Start (All Environments)

### Option A: Automated Setup (Recommended)

```bash
# 1. Run the setup script
bash scripts/setup.sh

# 2. Answer the prompts (it auto-generates secrets)

# 3. Start AgentRQ
docker-compose up -d

# 4. Open http://localhost:2026 (or your configured domain)
```

**What it does:**
- Generates secure random secrets automatically
- Creates `.env` file with your configuration
- Handles both local and production setups

### Option B: Manual Setup

```bash
# 1. Copy the example configuration
cp .env.example .env

# 2. Edit .env and fill in:
#    - AGENTRQ_ACCOUNTS_OAUTH2_CLI_GOOGLE_CLIENT_ID
#    - AGENTRQ_ACCOUNTS_OAUTH2_CLI_GOOGLE_CLIENT_SECRET
#    - Generate JWT_SECRET: openssl rand -base64 32
#    - Generate TOKEN_KEY: openssl rand -hex 16

# 3. Start AgentRQ
docker-compose up -d
```

## Verify It's Running

```bash
# Check container status
docker-compose ps

# View logs
docker-compose logs -f agentrq

# Open in browser
open http://localhost:2026  # macOS
xdg-open http://localhost:2026  # Linux
```

## Common Tasks

### Stop AgentRQ
```bash
docker-compose down
```

### Restart AgentRQ
```bash
docker-compose restart agentrq
```

### View Database
```bash
# SQLite (default)
sqlite3 storage/agentrq.db

# PostgreSQL
docker-compose exec postgres psql -U postgres -d agentrq
```

### Update to Latest Version
```bash
docker-compose pull
docker-compose up -d
```

## Next Steps

- **Connect Claude Code**: See [README.md](../README.md#-claude-code--ai-integration)
- **Setup Slack**: See [integrations/slack/README.md](../integrations/slack/README.md)
- **Production Deployment**: See [docs/SETUP_PRODUCTION.md](./SETUP_PRODUCTION.md)
- **Troubleshooting**: See [docs/SETUP_TROUBLESHOOTING.md](./SETUP_TROUBLESHOOTING.md)

## Need Help?

- **GitHub Issues**: https://github.com/agentrq/agentrq/issues
- **Discord**: https://discord.gg/xFSMaEA2b2
- **Documentation**: https://agentrq.com/docs
