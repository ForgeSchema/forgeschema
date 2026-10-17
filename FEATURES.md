# Capability matrix

ForgeSchema targets PostgreSQL 16/17. CI uses 16.10 and 17.6. The accepted contract is deliberately narrow; inspection rejects unsupported managed objects instead of silently producing an incomplete executable model.

| Area | Supported | Not supported |
| --- | --- | --- |
| Model | One named schema; ordinary tables, columns, ordered primary keys, unique constraints, plain and unique btree indexes, foreign keys within the schema with referential actions | Check/exclusion constraints, deferrable or NOT VALID constraints, cross-schema foreign keys, partitions, views and advanced indexes |
| Types | smallint, integer, bigint, text, varchar(n), char(n), numeric(p,s), boolean, uuid, date, timestamp without/with time zone, jsonb, bytea, real, double precision; documented aliases | Custom types, enums, arrays, timestamp precision and other modifiers |
| Column attributes | Names, type, nullability, literal and common function defaults (now(), current_date, gen_random_uuid() and others in SCHEMA.md), identity columns | Expression defaults, serial columns, generated columns and custom collations |
| Planning | Create tables with keys, constraints and indexes; explicit column renames; column additions that are nullable or have a default; widening varchar/numeric; nullability and default changes; new unique constraints, indexes and foreign keys on existing tables | Drops, other type conversions, required additions without a default, identity changes, primary-key alteration and constraint/index replacement |
| Artifact | Deterministic format 1 JSON, full source/target, fingerprints, quoted SQL, strict rebuild validation, plan ID; saved 0.1 plans still validate | Arbitrary SQL execution, rename inference or partial execution of a blocked plan |
| Apply | Transactional DDL, advisory coordination, locks on changed tables only, fresh drift check, target verification, atomic history, adjustable timeouts, PostgreSQL's error reason in failures | Online zero-downtime guarantees, concurrent index builds, backfills and general crash recovery |
| Verify | Independent read-only structural comparison with deterministic differences, including defaults, identity, constraints and foreign keys | Data correctness, grants/comments, history auditing or automatic repair |
| Delivery | Go CLI/API, reproducible synthetic walkthrough, interactive browser replay of real CI evidence | Connected database browser application, MCP server, other database engines or production-readiness guarantees |

Read SCHEMA.md and INSPECTION.md for exact identifier, type, default and object rejection rules. New tables can contain required columns; existing tables can gain one only with a default. Ordered key, constraint and index columns are significant.

Apply requires external DDL to be paused. ForgeSchema's advisory lock coordinates its own clients; table locks cover the tables a plan changes and do not prevent DDL elsewhere, outside dependencies or database-wide hooks. Metadata requires exclusive ownership/ACLs for the consistent applying role. A commit connection error can leave outcome uncertain. Read APPLY.md before execution.

Inspect and verify read catalog metadata, not application rows. The separate demo harness reads its own synthetic rows to assert preservation. Verification is a snapshot of supported structures, not proof of application correctness or future state.

Copyright 2026 ForgeSchema. Licensed under the Apache License, Version 2.0; see LICENSE and THIRD_PARTY_NOTICES.md.
