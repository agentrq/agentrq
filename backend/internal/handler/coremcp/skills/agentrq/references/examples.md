# Example Workflows

## 1. Creating a Task for a Specialized Agent

**task**
```json
{
  "action": "create",
  "payload": {
    "workspaceId": "aB3dE5gH7jK",
    "title": "Optimize database connection pooling",
    "body": "We are seeing intermittent connection timeouts during peak hours. Analyze logs from the last 7 days and propose connection pool configuration changes.",
    "assignee": "agent"
  }
}
```

## 2. Creating a Scheduled/Cron Task

**task**
```json
{
  "action": "create",
  "payload": {
    "workspaceId": "aB3dE5gH7jK",
    "title": "Daily health check report",
    "body": "Run system health checks and post a summary of any issues found.",
    "assignee": "agent",
    "cronSchedule": "0 9 * * *"
  }
}
```

## 3. Replying to a Task Thread

**task**
```json
{
  "action": "reply",
  "payload": {
    "workspaceId": "aB3dE5gH7jK",
    "taskId": "zX9vW7tS5rQ",
    "text": "Analysis complete. The connection pool exhausts at 50 concurrent connections. Recommend increasing max pool size to 100 and adding a 30s idle timeout."
  }
}
```

## 4. Updating Self-Learning Notes

**workspace**
```json
{
  "action": "update",
  "payload": {
    "id": "aB3dE5gH7jK",
    "selfLearningLoopNote": "- Database changes require DBA team review before applying.\n- Preferred connection pool library: pgxpool."
  }
}
```

More in [examples-more.md](examples-more.md).
