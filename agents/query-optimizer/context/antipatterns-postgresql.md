PostgreSQL anti-pattern checklist:

Index-related:
- [ ] Missing index on WHERE/JOIN/ORDER BY columns (seq scan on large table)
- [ ] Wrong composite index column order (should match equality-then-range pattern)
- [ ] Missing covering index (INCLUDE columns to enable index-only scan)
- [ ] Missing partial index where a constant filter applies (WHERE active = true)
- [ ] Over-indexing on write-heavy table (10+ indexes killing INSERT/UPDATE)

Query-related:
- [ ] SELECT * when fewer columns would enable index-only scan
- [ ] N+1 query pattern (visible in pg_stat_statements call frequency vs rows)
- [ ] OFFSET-based deep pagination (should use keyset pagination)
- [ ] NOT IN (subquery) with NULLs (should use NOT EXISTS)
- [ ] Leading wildcard LIKE without trigram index
- [ ] DISTINCT or GROUP BY as band-aid for duplicate joins
- [ ] Correlated subquery in SELECT list (executed per row)
- [ ] OR conditions preventing index use (rewrite as UNION ALL)

Type and cast issues:
- [ ] Implicit type cast preventing index use (integer vs text)
- [ ] Function call on indexed column without functional index (lower(email))

Maintenance:
- [ ] Stale statistics (last_analyze too old, row estimates > 10x off)
- [ ] Autovacuum falling behind (dead tuple ratio > 10-20%)
- [ ] Sort exceeding work_mem (external merge sort in plan)
- [ ] Hash join spilling to disk (Batches > 1 in BUFFERS output)

Schema:
- [ ] Missing foreign key index on referencing column
- [ ] COUNT(*) on entire large table without approximation
- [ ] CTE materialization preventing pushdown (MATERIALIZED keyword)
