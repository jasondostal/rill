# Rill — Recovery from Backup

Backups live in `$BACKUP_DIR` (default `/var/lib/rill/backups`) as `rill-YYYYMMDDTHHMMSSZ.surql.gz`.

## Restore to a fresh SurrealDB

```bash
# 1. Pick a backup
ls -1t /var/lib/rill/backups/rill-*.surql.gz | head -5

# 2. Stop Rill (the application) — don't let it reconnect mid-restore
docker compose stop rill

# 3. Decompress
gunzip -k /var/lib/rill/backups/rill-20260512T180000Z.surql.gz

# 4. Wipe the existing database. THIS IS DESTRUCTIVE. Have a fresh
#    backup at hand BEFORE running this if there's any data worth keeping.
surreal sql \
    --endpoint ws://localhost:8000 \
    --username root --password root \
    --namespace rill --database rill <<'EOF'
REMOVE DATABASE rill;
DEFINE DATABASE rill;
EOF

# 5. Import the backup
surreal import \
    --endpoint ws://localhost:8000 \
    --username root --password root \
    --namespace rill --database rill \
    /var/lib/rill/backups/rill-20260512T180000Z.surql

# 6. Verify counts before restarting Rill
surreal sql \
    --endpoint ws://localhost:8000 \
    --username root --password root \
    --namespace rill --database rill <<'EOF'
SELECT count() FROM memory GROUP ALL;
SELECT count() FROM document GROUP ALL;
SELECT count() FROM entity GROUP ALL;
EOF

# 7. Restart Rill
docker compose start rill
```

## Test the procedure BEFORE you need it

Spin up a second SurrealDB on a non-default port:

```bash
surreal start --bind 0.0.0.0:8001 --user root --pass root \
    surrealkv:///tmp/rill-restore-test
```

Then run steps 3, 5, 6 above against port 8001 instead of 8000. If
counts match production, your backup is good.
