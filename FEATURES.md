# Capability matrix

ForgeSchema targets PostgreSQL 16, 17 and 18. CI runs the current minor of each, and the recorded results name the exact versions. The accepted contract is deliberately narrow; inspection rejects unsupported managed objects instead of silently producing an incomplete executable model.

| Area | Supported | Not supported |
| --- | --- | --- |
| Model | One named schema; ordinary tables, columns, ordered primary keys, unique constraints, plain and unique btree indexes, foreign keys within the schema with referential actions, check constraints built from simple comparisons; enum types; plain views with their dependencies | Other check expressions, exclusion constraints, deferrable or NOT VALID constraints, cross-schema foreign keys, partitions, materialized views, views with options, triggers or rules, and advanced indexes |
| Types | smallint, integer, bigint, text, varchar(n), char(n), numeric(p,s), boolean, uuid, date, timestamp without/with time zone, jsonb, bytea, real, double precision; documented aliases; enums of the schema; one-dimensional arrays of any of these | Other custom types, domains, multidimensional arrays, timestamp precision and other modifiers |
| Column attributes | Names, type, nullability, literal and common function defaults (now(), current_date, gen_random_uuid() and others in SCHEMA.md), identity columns | Expression defaults, serial columns, generated columns and custom collations |
| Planning | Create enums, new enum values in any position, tables with keys, constraints and indexes, and views in dependency order; explicit column renames; column additions that are nullable or have a default; widening varchar/numeric; nullability and default changes; new unique constraints, check constraints, indexes and foreign keys on existing tables; concurrent index builds with plan --online; drops and type conversions listed in a change file, with staged expand and contract plans; views dropped and created again around the changes they depend on | Unlisted drops or conversions, required additions without a default, identity changes, primary-key alteration, constraint/index replacement, removing or reordering enum values, type changes into or out of enums and arrays, and backfills |
| Artifact | Deterministic format 1 JSON, full source/target, fingerprints, quoted SQL, strict rebuild validation, plan ID; saved 0.1 plans still validate | Arbitrary SQL execution, rename inference or partial execution of a blocked plan |
| Apply | Transactional DDL, advisory coordination, locks on changed tables only, fresh drift check, target verification, atomic history, adjustable timeouts, PostgreSQL's error reason in failures, online plans that build indexes concurrently and resume after a failed build | Zero-downtime guarantees for column changes, backfills and general crash recovery |
| Verify | Independent read-only structural comparison with deterministic differences, including defaults, identity, constraints, check expressions and foreign keys | Data correctness, grants/comments, history auditing or automatic repair |
| Delivery | Go CLI/API, a GitHub Action that posts plan --explain on pull requests (ACTION.md), reproducible synthetic walkthrough, interactive browser replay of real CI evidence | Connected database browser application, MCP server, other database engines or production-readiness guarantees |

Read SCHEMA.md and INSPECTION.md for exact identifier, type, default and object rejection rules. New tables can contain required columns; existing tables can gain one only with a default. Ordered key, constraint and index columns are significant.

Apply requires external DDL to be paused. ForgeSchema's advisory lock coordinates its own clients; table locks cover the tables a plan changes and do not prevent DDL elsewhere, outside dependencies or database-wide hooks. Metadata requires exclusive ownership/ACLs for the consistent applying role. A commit connection error can leave outcome uncertain. Read APPLY.md before execution.

Inspect and verify read catalog metadata, not application rows. The separate demo harness reads its own synthetic rows to assert preservation. Verification is a snapshot of supported structures, not proof of application correctness or future state.

Copyright 2026 ForgeSchema. Licensed under the Apache License, Version 2.0; see LICENSE and THIRD_PARTY_NOTICES.md.
