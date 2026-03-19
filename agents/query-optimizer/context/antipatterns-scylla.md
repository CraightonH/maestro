ScyllaDB/Cassandra anti-pattern checklist:

Data modeling:
- [ ] Partition key with unbounded growth (time-series with only date as partition key — partitions grow forever)
- [ ] Hot partition (single partition receiving disproportionate read/write traffic)
- [ ] Large partitions (> 100 MB or > 100K rows — causes GC pressure and read latency spikes)
- [ ] Using ALLOW FILTERING in production queries (full cluster scan)
- [ ] Secondary index on high-cardinality column (scatter-gather across all nodes)
- [ ] Materialized view on frequently-updated base table (write amplification)
- [ ] Relational modeling patterns forced onto wide-column store (JOINs via application, normalization)
- [ ] Single-row partitions everywhere (missing the point of wide rows for related data)

Query patterns:
- [ ] SELECT * on wide rows (fetches all columns across entire partition)
- [ ] Range query without clustering key order (Scylla can only range-scan in clustering key order)
- [ ] IN clause on partition key with many values (scatter-gather, worse than multiple single-partition queries in some cases)
- [ ] Unprepared statements in hot path (no query plan caching)
- [ ] Lightweight transactions (LWT/IF) used for every write (Paxos overhead, 4x latency)
- [ ] LIMIT without partition restriction (scans across partitions until limit satisfied)
- [ ] Token-range full-table scan when a denormalized lookup table would serve

Tombstones and deletions:
- [ ] Delete-heavy workload without TTL (tombstone accumulation slows reads)
- [ ] Deleting individual columns in wide rows instead of entire rows (column tombstones)
- [ ] gc_grace_seconds too high for workload (tombstones retained too long)
- [ ] gc_grace_seconds too low (risk of zombie data resurrection on node recovery)
- [ ] Reading ranges that span deleted data (tombstone warnings in logs: "Read N live rows and M tombstone cells")

Compaction:
- [ ] Size-Tiered Compaction (STCS) on read-heavy workload with updates (space amplification, read amp)
- [ ] Leveled Compaction (LCS) on write-heavy workload (write amplification)
- [ ] Incremental Compaction (ICS) not considered for Scylla (Scylla-specific, best of both worlds)
- [ ] Time-Window Compaction (TWCS) not used for time-series data (prevents tombstone accumulation across windows)
- [ ] Compaction falling behind (pending compaction tasks growing — check nodetool compactionstats)

Consistency and timeouts:
- [ ] Using QUORUM reads/writes when LOCAL_QUORUM suffices (cross-DC latency)
- [ ] Read timeout on large partition (partition too big, not a consistency issue)
- [ ] Write timeout during compaction pressure (back-pressure from compaction)
- [ ] Speculative retry not enabled for latency-sensitive reads

Operational:
- [ ] Repair not running on schedule (data inconsistency across replicas)
- [ ] Node count not matching replication factor well (uneven data distribution)
- [ ] Commit log and data on same disk (I/O contention)
