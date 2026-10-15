# ForgeSchema three-day prototype plan

Six sessions, approximately 2–3 hours of focused development each, grouped as two sessions per day (12–18 hours total). These are effort budgets, not automatic timed jobs or promises of background execution. Pauses and owner review may spread the work over more calendar days.

The goal is a Go command line prototype for PostgreSQL: inspect an existing database, compare it with a desired YAML schema, save an explained migration plan, apply supported changes, and verify the result. It is not a production-ready migration service.

## Day 1, session 1: foundation and schema contract
Create the Go module, CLI skeleton, strict YAML reader, canonical schema representation and validation. Establish PostgreSQL fixtures and a reproducible test environment. Define supported identifiers and types, schemas, nullability, basic primary keys and ordinary indexes. Reject unsupported features rather than silently omit them.
Acceptance: valid examples load; invalid and unknown fields fail clearly; tests and CLI build pass.
Replace inherited website content with accurate ForgeSchema content while preserving the theme, layouts, CSS and site structure. Edit project copy and necessary destinations in configuration only; leave domain connection to the owner.

## Day 1, session 2: inspect PostgreSQL
Implement read-only catalog inspection and deterministic snapshot output. Normalize supported types and defaults consistently with the YAML model. Detect unsupported objects in managed scope, document the boundary and test against a populated database.
Acceptance: repeated inspections agree and a supported database matches its expected canonical snapshot.

## Day 2, session 3: compare and plan
Implement deterministic operations for creating tables, adding nullable columns, explicitly renaming columns and adding ordinary indexes. Include primary keys on newly created tables. Generate quoted SQL, readable explanations and a versioned JSON plan containing source and target fingerprints. Report blocked operations for drops, type changes, required-column additions and unsupported constraints.
Acceptance: equivalent schemas yield no operations; explicit renames preserve intent; unsafe or ambiguous changes cannot produce an executable plan.

## Day 2, session 4: apply reviewed plans
Implement execution of validated plans using supported transactional DDL, a migration coordination lock, timeouts, drift checks and atomic history recording. Validate plan consistency against its declared schemas and operation set rather than trusting arbitrary embedded SQL. Reinspect under appropriate locking and verify before commit. Refuse unsupported concurrent DDL and document coordination limits with external writers.
Acceptance: supported migration succeeds on populated fixtures; stale or altered plans fail; a forced SQL failure rolls back schema changes and history.

## Day 3, session 5: verification and failure cases
Finish standalone verify, useful CLI exit codes and integration cases for duplicate names, quoting, nullability, dependency order, two competing ForgeSchema runs and rollback. Confirm original row values survive renames and additive migrations. Add CI with a PostgreSQL service and record actual validation results.
Acceptance: end-to-end inspect → plan → apply → verify passes; negative cases fail with understandable messages.

## Day 3, session 6: demonstrable prototype and handoff
Deliver a reproducible customer-database walkthrough, example YAML and plan output, command documentation and an honest supported/unsupported matrix. Run build, unit and database integration checks on the final state. Fix issues within the session budget or record precise blockers. Update website to describe verified behavior and show actual examples.
Acceptance: a fresh checkout can reproduce the walkthrough using documented prerequisites. No untested production claims or automatic public release.

## Scope after the prototype
Existing-table foreign keys and unique-constraint evolution, general type conversions, destructive migrations, resumable backfills, expand-and-contract orchestration, concurrent index builds, other databases, browser UI and MCP belong to later milestones. Revisit scope if evidence shows the six-session budget is insufficient; do not conceal incomplete features.

## Mandatory session checkpoint
Reserve the last 20–30 minutes of every session for validation and handoff. Commit and push completed code and documentation to ForgeSchema/forgeschema. Update RESUME.md with completed work, exact commands and results, known failures, decisions and the next concrete task. Update the website repository photozeitgeist/forgeschema.com with matching public progress; preserve its template and structure. Record both repository commit IDs and any deployment uncertainty. Report to the owner and stop. Start only the next session after an explicit go-ahead; one go-ahead never authorizes all remaining sessions.

## Current state
Session 1 completed on 2026-10-15. The Go foundation and strict schema validator are implemented and the inherited website content has been replaced. Sessions 2–6 remain pending. Session 2 requires an explicit go-ahead. See RESUME.md for validation evidence and limitations.
