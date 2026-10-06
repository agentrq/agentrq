# AgentRQ Production Deployment

> **For deploying AgentRQ in production with high availability and security.**

## Pre-Deployment Checklist

- [ ] Public domain registered and DNS configured
- [ ] Google OAuth2 credentials created
- [ ] PostgreSQL database (external or Docker Compose service)
- [ ] Backup strategy planned
- [ ] SSL/TLS ready (Let's Encrypt automatic or manual)

## Production Setup

### 1. Run Setup Script for Production

```bash
bash scripts/setup.sh production
```

Will prompt you for:
- Domain name
- Email for Let's Encrypt
- Google OAuth credentials
- PostgreSQL connection details (optional)
- Slack integration (optional)

### 2. Enable PostgreSQL (Recommended)

Edit `.env`:
```env
AGENTRQ_POSTGRES_ENABLED=true
AGENTRQ_SQLITE_ENABLED=false
AGENTRQ_POSTGRES_HOST=postgres  # or external host
AGENTRQ_POSTGRES_PORT=5432
AGENTRQ_POSTGRES_USER=postgres
AGENTRQ_POSTGRES_PASSWORD=<strong-password>
AGENTRQ_POSTGRES_DBNAME=agentrq
```

To use the included PostgreSQL service:
```bash
docker-compose --profile postgres up -d
```

### 3. Configure TLS/SSL

Edit `.env`:
```env
AGENTRQ_SSL_ENABLED=true
AGENTRQ_SSL_LETSENCRYPT_EMAIL=admin@your-domain.com
AGENTRQ_DOMAIN=your-domain.com
AGENTRQ_BASE_URL=https://your-domain.com
```

**Important**: Ports 80 and 443 must be publicly accessible for Let's Encrypt to work.

### 4. Start Services

```bash
# Create necessary directories
mkdir -p storage certs
chmod 0777 storage certs

# Start with PostgreSQL
docker-compose --profile postgres up -d

# Or without PostgreSQL (if using external)
docker-compose up -d
```

### 5. Verify TLS Certificate

```bash
# Check certificate was created
ls -la certs/

# View logs
docker-compose logs -f agentrq | grep -i ssl
```

## Security Hardening

### 1. Disable Root Login

Make sure in `.env`:
```env
AGENTRQ_AUTH_ROOT_LOGIN_ENABLED=false
```

### 2. Strong Secrets

Verify secrets are strong (32+ characters):
```bash
openssl rand -base64 32  # JWT secret
openssl rand -hex 16     # Token key (32 bytes)
```

### 3. Backup Strategy

```bash
# Backup PostgreSQL
docker-compose exec postgres pg_dump -U postgres agentrq > backup.sql

# Backup storage (attachments)
tar -czf storage-backup.tar.gz storage/

# Backup .env (securely)
cp .env .env.backup
```

### 4. Network Security

- Place behind a reverse proxy (Nginx, Traefik, Cloudflare)
- Enable rate limiting (default: 60 req/IP, 100 req/user per minute)
- Enable DDoS protection (default: 20 req/sec)
- Use firewall rules to restrict non-HTTP(S) traffic

### 5. Regular Updates

```bash
# Pull latest image
docker-compose pull

# Restart services
docker-compose up -d

# View changelog: https://github.com/agentrq/agentrq/releases
```

## Monitoring

### View Logs

```bash
# Real-time
docker-compose logs -f agentrq

# Last 100 lines
docker-compose logs --tail 100 agentrq

# Filter by level
docker-compose logs agentrq | grep ERROR
```

### Database Health

```bash
# PostgreSQL
docker-compose exec postgres pg_isready -U postgres

# SQLite (if used)
sqlite3 storage/agentrq.db "SELECT COUNT(*) FROM tasks;"
```

### Storage Usage

```bash
# Check attachment storage
du -sh storage/

# Check database size (PostgreSQL)
docker-compose exec postgres psql -U postgres -c "SELECT pg_size_pretty(pg_database_size('agentrq'));"
```

### Attachment Auto-Cleanup

AgentRQ automatically deletes attachment files older than a configurable retention period. The cleanup runs daily at midnight UTC and skips database files.

| Variable | Default | Description |
|---|---|---|
| `AGENTRQ_APP_ATTACHMENT_RETENTION` | `7d` | Retention period for attachment files. Accepts day values (`7d`, `30d`) or Go duration strings (`168h`). |

Set in `.env`:
```env
AGENTRQ_APP_ATTACHMENT_RETENTION=30d
```

Skill files are not attachments: cleanup never deletes them.

### Skill and Attachment Storage

Skill files and attachments are kept under `AGENTRQ_STORAGE_DIR` by default (`/storage` in docker-compose, `./_storage` otherwise), in `skills/` and `artifacts/` (where attachments live). Set `AGENTRQ_SKILLS_STORAGE=s3` or `AGENTRQ_ARTIFACTS_STORAGE=s3` to keep either in any S3-compatible bucket instead (AWS S3, MinIO, Cloudflare R2…), under the same two prefixes; the server refuses to start on any value other than `local` or `s3`.

Every file is kept at a key that names its workspace and task, or its account and skill, and ends in a fresh id:

- attachments: `artifacts/w-<workspace id>/<task id>/<id>`
- skill files: `skills/u-<account id>/skill-<skill id>/<id>`

**Each file has a public link, and the link is the only credential needed to read it.** Without knowing both the workspace id and the task id (or the account id and the skill id), a link is nearly impossible to guess. Ids are time-ordered, though, so someone who holds one link has both, and can work out the links of the other files on that task or skill: share a link only with someone who may see everything there. The web app's Download button, the agents' `getAttachment` tool (by default) and `agentrq-ws attachment get` all use it. Local files are served by the server itself at `<AGENTRQ_BASE_URL>/storage/artifacts/...` and `/storage/skills/...`, with no sign-in; files in S3 are read from the bucket, at `<AGENTRQ_S3_PUBLIC_URL>/artifacts/...` and `/skills/...`.

| Variable | Default | Description |
|---|---|---|
| `AGENTRQ_STORAGE_DIR` | `./_storage` | Writable directory for local files. Its root also holds the SQLite database and older attachments. |
| `AGENTRQ_SKILLS_STORAGE` | `local` | `local` or `s3`. |
| `AGENTRQ_ARTIFACTS_STORAGE` | `local` | `local` or `s3`, for attachments. |
| `AGENTRQ_S3_ENDPOINT` | | The S3 endpoint URL, e.g. `https://s3.us-east-1.amazonaws.com`. Addressed path-style. |
| `AGENTRQ_S3_ACCESS_KEY` | | Access key id. |
| `AGENTRQ_S3_SECRET_ACCESS_KEY` | | Secret access key. |
| `AGENTRQ_S3_REGION` | `us-east-1` | Bucket region. |
| `AGENTRQ_S3_BUCKET` | | Bucket name. It must already exist. |
| `AGENTRQ_S3_PUBLIC_URL` | `<endpoint>/<bucket>` | Base of public links to the bucket, e.g. a CDN in front of it. |

**Serving local files.** The server answers `/storage/...` only for a path of exactly the shape above — 11-character base62 ids, nothing else — and reads only from `skills/` and `artifacts/`, never following a link out of them. Do not serve the storage directory any other way (a static file handler, a proxy `alias`): its root holds the database. A file that could run in a browser, such as HTML or SVG, is sent as a download.

**Serving from S3.** No object ACL is set, since new AWS buckets and several S3-compatible stores refuse them, so allow anonymous reads of the two prefixes, and nothing else, with a bucket policy (AWS example):

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": "*",
    "Action": "s3:GetObject",
    "Resource": ["arn:aws:s3:::<bucket>/artifacts/*", "arn:aws:s3:::<bucket>/skills/*"]
  }]
}
```

An attachment's upload type is kept only when a browser cannot run it; anything else is stored as `application/octet-stream`.

**Retention.** The daily cleanup deletes local attachments, in `artifacts/` and flat in the storage directory where they were kept before, once they pass `AGENTRQ_APP_ATTACHMENT_RETENTION`. With S3, add a lifecycle rule that expires `artifacts/` after the same period.

**Switching** does not move files already saved. Attachments kept flat in the storage directory before this layout are still found there, whichever store is in use, but have no public link. Copy skills' `w-*` directories from `<storage dir>/skills/` to `skills/` in the bucket, or re-import them.

## Reverse Proxy Setup (Nginx Example)

```nginx
server {
    listen 80;
    server_name your-domain.com;

    location / {
        proxy_pass http://localhost:2026;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Increase timeouts for long-running connections
        proxy_connect_timeout 60s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;
    }
}
```

## Troubleshooting

See [docs/SETUP_TROUBLESHOOTING.md](./SETUP_TROUBLESHOOTING.md)
