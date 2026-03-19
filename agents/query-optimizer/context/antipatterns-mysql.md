MySQL/Aurora anti-pattern checklist:

Index-related:
- [ ] Missing index on WHERE/JOIN/ORDER BY columns (full table scan / type=ALL in EXPLAIN)
- [ ] Wrong composite index column order (leftmost prefix rule not satisfied)
- [ ] Missing covering index (InnoDB stores row data in clustered index — covering avoids secondary lookup)
- [ ] Using SELECT * defeating covering index optimization
- [ ] Index on low-cardinality column where full scan is cheaper (e.g., boolean flag)
- [ ] Too many indexes on write-heavy table (each INSERT/UPDATE maintains all indexes)
- [ ] Not leveraging InnoDB clustered index (primary key IS the table — choose it wisely)

Query-related:
- [ ] Implicit charset/collation conversion preventing index use (utf8 vs utf8mb4, collation mismatch in JOINs)
- [ ] Function on indexed column in WHERE (DATE(created_at) = ... instead of range)
- [ ] Using != or NOT IN preventing index range scan
- [ ] OFFSET-based pagination on large result sets (use keyset/seek pagination)
- [ ] SELECT COUNT(*) without WHERE on large InnoDB table (requires full index scan — no cheap count like MyISAM)
- [ ] Subquery in IN clause re-executed per row (MySQL < 5.6) or not materialized
- [ ] FORCE INDEX hint masking the real problem (optimizer has bad stats)
- [ ] Using HAVING for conditions that belong in WHERE
- [ ] ORDER BY with mixed ASC/DESC not matching index order (MySQL 8.0+ supports descending indexes)
- [ ] GROUP BY causing filesort when an index could satisfy the grouping

InnoDB-specific:
- [ ] Long-running transactions preventing purge (undo log bloat, history list length growing)
- [ ] Gap locks causing deadlocks on range scans with concurrent inserts
- [ ] Buffer pool too small for working set (high pages_read vs pages_read_ahead in SHOW ENGINE INNODB STATUS)
- [ ] innodb_buffer_pool_size < 70% of available RAM on dedicated DB server
- [ ] Row format COMPACT when DYNAMIC would avoid off-page storage for large VARCHAR

Schema:
- [ ] VARCHAR(255) as default for every string column (wastes index space, especially with utf8mb4 = 1020 bytes per key)
- [ ] Missing AUTO_INCREMENT on primary key for InnoDB (random UUIDs as PK cause page splits)
- [ ] ENUM type used for frequently changing value sets (schema change required to add values)
- [ ] TEXT/BLOB columns in tables that are frequently scanned (move to separate table or use COMPRESSED row format)
- [ ] Foreign key constraints on high-throughput tables causing lock contention

Replication/Aurora:
- [ ] Queries hitting writer when they could use reader endpoint
- [ ] Long-running queries on writer causing binlog bloat
- [ ] Statement-based replication with non-deterministic functions (NOW(), RAND())
