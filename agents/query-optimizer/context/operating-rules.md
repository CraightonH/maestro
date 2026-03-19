Query optimization operating rules (all engines):

- All database access is read-only. Never execute DML or DDL against the target database.
- The agent produces analysis reports and migration files in the workspace, not live changes.
- Safety is non-negotiable: every DDL recommendation includes risk assessment and rollback path.
- Evidence before opinion: every recommendation cites specific diagnostic output (plan nodes, catalog values, sizes).
- Prefer the least invasive fix: index creation > query rewrite > schema change > config change.
- If the database engine is not stated in the issue, infer from query syntax (SQL vs CQL), catalog names, or ask.

Engine-specific safety rules:

PostgreSQL:
  - Always use CREATE INDEX CONCURRENTLY. Never without it.
  - For tables > 100 GB, warn about build duration and disk space.
  - For DDL requiring ACCESS EXCLUSIVE lock, estimate lock duration.
  - If estimated lock > 5 seconds on an active table, flag as requiring maintenance window.

MySQL/Aurora:
  - For large tables (> 10M rows), recommend pt-online-schema-change or gh-ost instead of direct ALTER TABLE.
  - Direct ALTER TABLE ADD INDEX locks the table in MySQL 5.x. MySQL 8.0+ supports online DDL for most index ops — note the version requirement.
  - Always specify the storage engine (InnoDB) explicitly in DDL.
  - For Aurora, note that reader instances can serve read traffic during schema changes.

ScyllaDB/Cassandra:
  - Never recommend secondary indexes on high-cardinality columns. Use materialized views or denormalization instead.
  - Partition key design changes require a new table + data migration. Flag this as high-effort.
  - Compaction strategy changes are online but can cause temporary read/write latency spikes. Recommend off-peak.
  - Tombstone-heavy workloads need TTL or explicit compaction, not query changes.
