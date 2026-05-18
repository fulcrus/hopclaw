---
name: database
description: Connect to and query PostgreSQL, MySQL, and SQLite databases
homepage: https://www.postgresql.org/docs/current/app-psql.html
user-invocable: true
command-dispatch: tool
command-tool: database.query
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: dev.database
    emoji: "\U0001F5C4\uFE0F"
    requires:
      anyBins:
        - psql
        - mysql
        - sqlite3
    always: false
---
# Database

Connect to PostgreSQL, MySQL, or SQLite databases to query data and inspect schemas.

## Capabilities

- Connect to local or remote database instances
- Run SELECT queries and display results
- Describe table schemas, indexes, and constraints
- List databases, tables, and views
- Run INSERT, UPDATE, DELETE with user confirmation
- Export query results as CSV or JSON
- Explain query execution plans

## Authentication

Connection details via environment variables:

**PostgreSQL:**
- `DATABASE_URL`: Full connection string (e.g., postgres://user:pass@host:5432/dbname)
- Or individual: `PGHOST`, `PGPORT`, `PGUSER`, `PGPASSWORD`, `PGDATABASE`

**MySQL:**
- `MYSQL_HOST`, `MYSQL_PORT`, `MYSQL_USER`, `MYSQL_PASSWORD`, `MYSQL_DATABASE`

**SQLite:**
- Direct file path, no credentials needed.

## Usage

### PostgreSQL (psql)

```bash
# Connect and run a query
psql "${DATABASE_URL}" -c "SELECT count(*) FROM users"

# With individual variables
PGPASSWORD="${PGPASSWORD}" psql -h "${PGHOST}" -p "${PGPORT:-5432}" \
  -U "${PGUSER}" -d "${PGDATABASE}" -c "SELECT * FROM users LIMIT 10"

# List tables
psql "${DATABASE_URL}" -c "\dt"

# Describe a table
psql "${DATABASE_URL}" -c "\d+ users"

# List databases
psql "${DATABASE_URL}" -c "\l"

# Export as CSV
psql "${DATABASE_URL}" -c "COPY (SELECT * FROM users) TO STDOUT WITH CSV HEADER"

# Query plan
psql "${DATABASE_URL}" -c "EXPLAIN ANALYZE SELECT * FROM users WHERE email LIKE '%@example.com'"

# Formatted output
psql "${DATABASE_URL}" --pset=format=wrapped -c "SELECT id, name, email FROM users"
```

### MySQL

```bash
# Connect and run a query
mysql -h "${MYSQL_HOST}" -P "${MYSQL_PORT:-3306}" \
  -u "${MYSQL_USER}" -p"${MYSQL_PASSWORD}" "${MYSQL_DATABASE}" \
  -e "SELECT count(*) FROM users"

# List tables
mysql -h "${MYSQL_HOST}" -u "${MYSQL_USER}" -p"${MYSQL_PASSWORD}" \
  "${MYSQL_DATABASE}" -e "SHOW TABLES"

# Describe table
mysql -h "${MYSQL_HOST}" -u "${MYSQL_USER}" -p"${MYSQL_PASSWORD}" \
  "${MYSQL_DATABASE}" -e "DESCRIBE users"

# Export as CSV
mysql -h "${MYSQL_HOST}" -u "${MYSQL_USER}" -p"${MYSQL_PASSWORD}" \
  "${MYSQL_DATABASE}" -B -e "SELECT * FROM users" | tr '\t' ','

# Query plan
mysql -h "${MYSQL_HOST}" -u "${MYSQL_USER}" -p"${MYSQL_PASSWORD}" \
  "${MYSQL_DATABASE}" -e "EXPLAIN SELECT * FROM users WHERE email LIKE '%@example.com'"
```

### SQLite

```bash
# Open a database file and query
sqlite3 /path/to/database.db "SELECT count(*) FROM users"

# List tables
sqlite3 /path/to/database.db ".tables"

# Describe table schema
sqlite3 /path/to/database.db ".schema users"

# Formatted output
sqlite3 -header -column /path/to/database.db "SELECT * FROM users LIMIT 10"

# Export as CSV
sqlite3 -header -csv /path/to/database.db "SELECT * FROM users"

# Export as JSON (SQLite 3.33+)
sqlite3 -json /path/to/database.db "SELECT * FROM users LIMIT 10"
```

### Common Operations

```bash
# Count rows
SELECT count(*) FROM table_name;

# Find duplicates
SELECT email, count(*) as cnt FROM users GROUP BY email HAVING cnt > 1;

# Recent records
SELECT * FROM orders ORDER BY created_at DESC LIMIT 20;

# Table sizes (PostgreSQL)
SELECT relname, pg_size_pretty(pg_total_relation_size(relid))
FROM pg_catalog.pg_statio_user_tables ORDER BY pg_total_relation_size(relid) DESC;
```

## Error Handling

- If "connection refused", verify the database server is running and the host/port are correct.
- If "authentication failed", check credentials. For PostgreSQL, verify pg_hba.conf allows the connection.
- If "relation does not exist", the table name may be case-sensitive or in a different schema. Use `\dt *.*` to search.
- If queries are slow, run EXPLAIN ANALYZE to identify missing indexes.

## Security

- NEVER expose database passwords in command output. Use environment variables for all credentials.
- Always use LIMIT on SELECT queries to avoid returning excessive data.
- Confirm with the user before running any write operations (INSERT, UPDATE, DELETE, DROP, TRUNCATE).
- Never run DROP TABLE or TRUNCATE without explicit user confirmation and a clear understanding of the impact.
- Prefer read-only connections when only querying data.
- Do not display full row data for tables that may contain PII (personally identifiable information) without user consent.
