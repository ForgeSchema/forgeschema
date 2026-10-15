# Prototype capability matrix

This alpha prototype targets PostgreSQL 16/17. CI uses 16.10 and 17.6. The accepted contract is deliberately narrow; inspection rejects unsupported managed objects instead of silently producing an incomplete executable model.

| Area | Supported | Outside the prototype |
| --- | --- | --- |
| Model | One named schema; ordinary tables, columns, ordered basic/composite primary keys, ordinary btree indexes | Foreign keys, unique/check constraints, partitions, views and advanced indexes |
| Types | smallint, integer, bigint, text, boolean, uuid, date, timestamp without/with time zone, jsonb, bytea, real, double precision; documented aliases | Custom types, enums, arrays, numeric precision/scale and unsupported modifiers |
| Column attributes | Names, supported type and nullability | Defaults, generated/identity columns and custom collations |
| Planning | Create tables with primary keys/indexes, explicit one-to-one column renames, nullable additions and ordinary index additions | Drops, type/nullability conversions, required additions to existing tables, primary-key alteration and index replacement |
| Artifact | Deterministic format 1 JSON, full source/target, fingerprints, quoted SQL, strict rebuild validation, plan ID | Arbitrary SQL execution, rename inference or partial execution of a blocked plan |
| Apply | Transactional supported DDL, advisory coordination, source table locks, fresh drift check, target verification and atomic history | Online zero-downtime guarantees, concurrent index builds, backfills and general crash recovery |
| Verify | Independent read-only supported structural comparison with deterministic differences | Data correctness, grants/comments, history auditing or automatic repair |
| Delivery | Go CLI/API, reproducible synthetic walkthrough, interactive browser replay of real CI evidence | Connected database browser application, MCP server, other database engines or production-readiness guarantees |

Read SCHEMA.md and INSPECTION.md for exact identifier, size and object rejection rules. Nullable additions apply to existing tables; new tables can contain required columns. Ordered primary-key and index columns are significant.

Apply requires external DDL to be paused. ForgeSchema's advisory lock coordinates its own clients; source relation locks do not prevent every namespace CREATE, outside dependency or database-wide hook. Metadata requires exclusive ownership/ACLs for the consistent applying role. A commit connection error can leave outcome uncertain. Read APPLY.md before execution.

Inspect and verify read catalog metadata, not application rows. The separate demo harness reads its own synthetic rows to assert preservation. Verification is a snapshot of supported structures, not proof of application correctness or future state.

All six prototype sessions are complete. Further features and release decisions require owner direction. Copyright 2026 ForgeSchema. Licensed under the Apache License, Version 2.0; see LICENSE and THIRD_PARTY_NOTICES.md.
