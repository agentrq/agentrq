# AgentRQ Troubleshooting Guide

## Common Issues

### Port Already in Use

```bash
# Error: bind: address already in use

# Find process using port 2026
lsof -i :2026

# Kill it or change port in .env
PORT=3000
docker-compose up -d
```

### Google OAuth Not Working

**Problem**: "Redirect URI mismatch" error

**Solution**:
1. Go to [Google Cloud Console](https://console.cloud.google.com/apis/credentials)
2. Select your OAuth 2.0 Client ID
3. Add your redirect URI:
   - Local: `http://localhost:2026/api/v1/auth/google/callback`
   - Production: `https://your-domain.com/api/v1/auth/google/callback`
4. Save and update `.env` with credentials
5. Restart: `docker-compose restart agentrq`

### Container Won't Start

```bash
# Check logs
docker-compose logs agentrq

# Common causes:
# - Missing .env file
# - Invalid secrets (token key not 32 bytes)
# - Port already in use
# - Database connection error
```

### Database Connection Error

**SQLite Error**:
```bash
# Make sure storage directory exists
mkdir -p storage
chmod 0777 storage
docker-compose up -d
```

**PostgreSQL Error**:
```bash
# Check PostgreSQL is running
docker-compose ps postgres

# View PostgreSQL logs
docker-compose logs postgres

# Test connection
docker-compose exec postgres psql -U postgres -c "SELECT 1;"
```

### SSL/TLS Certificate Issues

**Certificate not generating**:
```bash
# Check Let's Encrypt logs
docker-compose logs agentrq | grep -i letsencrypt

# Verify ports 80 and 443 are open
sudo netstat -tlnp | grep -E ':80|:443'

# Ensure DNS points to your server
nslookup your-domain.com
```

### High Memory Usage

```bash
# Monitor container stats
docker stats agentrq

# If too high, restart
docker-compose restart agentrq

# Check for memory leaks in logs
docker-compose logs agentrq | grep -i "memory\|OOM"
```

### Slow Performance

1. **Check logs for errors**:
   ```bash
   docker-compose logs agentrq | grep ERROR
   ```

2. **Check database performance**:
   ```bash
   # SQLite
   sqlite3 storage/agentrq.db ".schema tasks" | head -20
   
   # PostgreSQL
   docker-compose exec postgres psql -U postgres -c "SELECT * FROM pg_stat_statements LIMIT 10;"
   ```

3. **Check disk space**:
   ```bash
   df -h
   du -sh storage/
   ```

4. **Restart services**:
   ```bash
   docker-compose restart
   ```

## Environment Variable Issues

### Invalid JWT Secret

**Error**: All users logged out, "invalid token"

**Cause**: `AGENTRQ_AUTH_JWT_SECRET` changed

**Solution**: Restore from backup or regenerate and clear sessions

### Invalid Token Key

**Error**: "invalid token key" when connecting MCP

**Cause**: `AGENTRQ_AUTH_WORKSPACE_TOKEN_KEY` not exactly 32 bytes

**Solution**:
```bash
# Generate correct key
openssl rand -hex 16

# Update .env
AGENTRQ_AUTH_WORKSPACE_TOKEN_KEY=<new-key>

# Regenerate workspace tokens
docker-compose restart agentrq
```

## File System Issues

### Permission Denied on Storage

```bash
# Fix permissions
chmod -R 0777 storage
chmod -R 0777 certs

# Or change owner
sudo chown -R 1000:1000 storage certs
```

### Disk Space Full

```bash
# Check disk usage
df -h

# Clean Docker artifacts
docker system prune -a

# Backup and cleanup old attachments
tar -czf storage-old-$(date +%s).tar.gz storage/
```

## Database Issues

### SQLite Database Locked

```bash
# Restart container
docker-compose restart agentrq

# If persistent, migrate to PostgreSQL (recommended)
```

### PostgreSQL Connection Pool Exhausted

```bash
# Check active connections
docker-compose exec postgres psql -U postgres -c "SELECT * FROM pg_stat_activity;"

# Restart PostgreSQL
docker-compose restart postgres
```

## Network Issues

### Cannot Reach AgentRQ from Another Machine

1. Check firewall:
   ```bash
   sudo ufw allow 2026/tcp  # Linux
   ```

2. Check binding:
   ```bash
   docker-compose logs agentrq | grep -i "listen\|bind"
   ```

3. Use correct hostname/IP:
   - Local: `localhost:2026`
   - Network: `<your-ip>:2026`
   - Domain: `your-domain.com`

## Getting Help

1. **Collect debug info**:
   ```bash
   docker-compose logs agentrq > logs.txt
   docker-compose ps > status.txt
   docker-compose --version > version.txt
   ```

2. **Open GitHub issue**: https://github.com/agentrq/agentrq/issues

3. **Join Discord**: https://discord.gg/xFSMaEA2b2

4. **View docs**: https://agentrq.com/docs
