---
name: redis
description: Interact with Redis for key-value operations, monitoring, and diagnostics
homepage: https://redis.io/docs/latest/commands/
user-invocable: true
command-dispatch: tool
command-tool: redis.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: infra.redis
    emoji: "\U0001F534"
    requires:
      bins:
        - redis-cli
    always: false
---
# Redis

Interact with Redis instances for key-value operations, monitoring, and diagnostics.

## Capabilities

- Read and write keys (strings, hashes, lists, sets, sorted sets)
- Search keys by pattern
- Monitor server health and performance
- View memory usage and statistics
- Manage key expiration (TTL)
- Pub/sub operations
- Scan large keyspaces safely
- Inspect slow queries

## Authentication

Connection via environment variables:

- `REDIS_URL`: Full connection URL (e.g., redis://user:password@host:6379/0)
- Or individual: `REDIS_HOST`, `REDIS_PORT`, `REDIS_PASSWORD`, `REDIS_DB`

### Connection Strings

```bash
# Using REDIS_URL
redis-cli -u "${REDIS_URL}"

# Using individual variables
redis-cli -h "${REDIS_HOST:-localhost}" -p "${REDIS_PORT:-6379}" \
  -a "${REDIS_PASSWORD}" -n "${REDIS_DB:-0}" --no-auth-warning
```

## Usage

### Basic Key Operations

```bash
# Get a key
redis-cli -u "${REDIS_URL}" GET mykey

# Set a key
redis-cli -u "${REDIS_URL}" SET mykey "myvalue"

# Set with expiration (60 seconds)
redis-cli -u "${REDIS_URL}" SET session:abc123 "data" EX 60

# Check if key exists
redis-cli -u "${REDIS_URL}" EXISTS mykey

# Delete a key
redis-cli -u "${REDIS_URL}" DEL mykey

# Get key TTL
redis-cli -u "${REDIS_URL}" TTL mykey

# Get key type
redis-cli -u "${REDIS_URL}" TYPE mykey
```

### Hash Operations

```bash
# Set hash fields
redis-cli -u "${REDIS_URL}" HSET user:1001 name "Alice" email "alice@example.com" role "admin"

# Get all hash fields
redis-cli -u "${REDIS_URL}" HGETALL user:1001

# Get a single field
redis-cli -u "${REDIS_URL}" HGET user:1001 name

# Increment a hash field
redis-cli -u "${REDIS_URL}" HINCRBY user:1001 login_count 1
```

### List Operations

```bash
# Push to list
redis-cli -u "${REDIS_URL}" LPUSH queue:tasks "task1" "task2" "task3"

# Get list range
redis-cli -u "${REDIS_URL}" LRANGE queue:tasks 0 -1

# Pop from list
redis-cli -u "${REDIS_URL}" RPOP queue:tasks

# List length
redis-cli -u "${REDIS_URL}" LLEN queue:tasks
```

### Set and Sorted Set Operations

```bash
# Add to set
redis-cli -u "${REDIS_URL}" SADD tags:article:42 "golang" "redis" "tutorial"

# Get set members
redis-cli -u "${REDIS_URL}" SMEMBERS tags:article:42

# Add to sorted set with score
redis-cli -u "${REDIS_URL}" ZADD leaderboard 100 "alice" 85 "bob" 92 "charlie"

# Get top entries
redis-cli -u "${REDIS_URL}" ZREVRANGE leaderboard 0 9 WITHSCORES
```

### Scanning Keys (Safe for Production)

```bash
# NEVER use KEYS in production! Use SCAN instead.

# Scan for keys matching a pattern
redis-cli -u "${REDIS_URL}" --scan --pattern "session:*"

# Count keys matching a pattern
redis-cli -u "${REDIS_URL}" --scan --pattern "cache:*" | wc -l

# Scan with a specific count hint
redis-cli -u "${REDIS_URL}" --scan --pattern "user:*" --count 100
```

### Server Information and Monitoring

```bash
# Server info overview
redis-cli -u "${REDIS_URL}" INFO server

# Memory usage
redis-cli -u "${REDIS_URL}" INFO memory

# Connected clients
redis-cli -u "${REDIS_URL}" INFO clients

# Keyspace statistics
redis-cli -u "${REDIS_URL}" INFO keyspace

# Database size (key count)
redis-cli -u "${REDIS_URL}" DBSIZE

# Memory usage for a specific key
redis-cli -u "${REDIS_URL}" MEMORY USAGE mykey

# Slow log (recent slow queries)
redis-cli -u "${REDIS_URL}" SLOWLOG GET 10

# Latency test
redis-cli -u "${REDIS_URL}" --latency-history -i 3

# Current connected clients
redis-cli -u "${REDIS_URL}" CLIENT LIST
```

### Bulk Operations

```bash
# Delete all keys matching a pattern (use with caution)
redis-cli -u "${REDIS_URL}" --scan --pattern "cache:temp:*" | \
  xargs -L 100 redis-cli -u "${REDIS_URL}" DEL

# Export keys as commands
redis-cli -u "${REDIS_URL}" --scan --pattern "config:*" | while read key; do
  echo "Key: $key"
  redis-cli -u "${REDIS_URL}" GET "$key"
done
```

## Error Handling

- If "Connection refused", verify the Redis server is running and the host/port are correct.
- If "NOAUTH Authentication required", set the REDIS_PASSWORD or include auth in REDIS_URL.
- If "WRONGTYPE", you are using a command on the wrong data type. Check with `TYPE key` first.
- If "OOM command not allowed", Redis is out of memory. Check `INFO memory` for details.

## Security

- NEVER expose REDIS_PASSWORD in command output.
- NEVER use the `KEYS` command in production, as it blocks the server. Always use `SCAN`.
- Confirm with the user before executing `FLUSHDB`, `FLUSHALL`, or bulk `DEL` operations.
- Do not run `CONFIG SET` or `DEBUG` commands without explicit user request.
- Be cautious with `CLIENT LIST` output, which may contain connection details.
- Avoid `MONITOR` in production as it significantly impacts performance.
