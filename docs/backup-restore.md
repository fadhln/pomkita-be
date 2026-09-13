# Backup and restore runbook

This runbook applies to the PomKita PostgreSQL database. Use a separate restore database for every rehearsal. Do not restore over the live database.

## Targets

- RPO: at most five minutes. WAL archive must complete within five minutes.
- RTO: less than one hour for a restore from the latest base backup and WAL.
- Backups must use an encrypted storage location with restricted access.

## Base backup

Set `DATABASE_URL` to the source database. Run the command from a host that can reach PostgreSQL:

```sh
umask 077
pg_dump --format=custom --no-owner --no-privileges \
  --file="pomkita-$(date -u +%Y%m%dT%H%M%SZ).dump" "$DATABASE_URL"
```

Copy the dump to the backup store. Record its SHA-256 checksum and keep the checksum beside the dump:

```sh
sha256sum pomkita-*.dump > pomkita-*.dump.sha256
```

## WAL archiving

Set these PostgreSQL parameters in the managed service or `postgresql.conf`:

```conf
wal_level = replica
archive_mode = on
archive_timeout = 300s
archive_command = 'test ! -f /secure/wal/%f && cp %p /secure/wal/%f'
```

Use an archive command that copies WAL to durable, access-controlled storage. Monitor the oldest unarchived WAL file, archive failures, and the age of the newest archived WAL. A failed archive must alert the operator before the five-minute RPO is exceeded.

## Restore rehearsal

1. Create an empty PostgreSQL database with the same major version as the source.
2. Restore the custom dump. Apply WAL through the selected recovery point when point-in-time recovery is required.

```sh
createdb "$RESTORE_DATABASE"
pg_restore --exit-on-error --no-owner --no-privileges \
  --dbname="$RESTORE_DATABASE" pomkita-YYYYMMDDTHHMMSSZ.dump
```

3. Run the service migration check against the restore database. Do not allow application traffic until the migration is current.
4. Compare the source and restore checksums. The checksum input must use stable table names and row counts, in table-name order:

```sql
select encode(app.digest(convert_to(coalesce(string_agg(
  table_name || ':' || row_count::text, E'\n' order by table_name), ''), 'UTF8'), 'sha256'), 'hex')
from (
  select table_name, count(*) as row_count
  from information_schema.tables t
  join lateral (select 1 from pg_catalog.pg_class c
                 join pg_catalog.pg_namespace n on n.oid = c.relnamespace
                where n.nspname = t.table_schema
                  and c.relname = t.table_name
                  and c.relkind in ('r', 'p')) rel on true
  where t.table_schema = 'public'
  group by table_name
) counts;
```

The source and restore checksums must match. Also check the latest `schema_migrations` version, the number of organizations, and a demo report read through the service. Record the dump checksum, restore checksum, restore duration, WAL position, and operator in the rehearsal log.

5. Drop the restore database after the rehearsal and retain the log with the backup record.

Run this rehearsal at least once before pilot release and after a PostgreSQL major-version change.
