# ForgeSchema resume checkpoint

Updated: 2026-10-15, end of day 1 session 1.

Status: PAUSED. Session 1 is complete. Await an explicit go-ahead for session 2 only. Do not start inspection, planning or execution in this checkpoint.

## Published checkpoints
Engine implementation: c5e2bd4d03334279fc1715d1abcd17c13ab0f085 on main in ForgeSchema/forgeschema.
Website content: a86d6ab63839be2883ae91e599cf967d696e7367 on main in photozeitgeist/forgeschema.com.
This resume update is a separate documentation commit following the implementation commit.

## What exists
Go module (minimum Go 1.24), CLI version/help/validate, strict version 1 YAML reader, canonical schema representation and SHA-256 fingerprints. The contract supports one PostgreSQL schema with tables, columns, nullability, basic/composite primary keys and ordinary named indexes. Type aliases normalize; names, references and relation-name collisions are validated. Unknown and duplicate fields, nulls, coercions, multiple documents, anchors/aliases and unsupported types/features are rejected. Input is limited to 1 MiB. Tables are limited to 1600 columns; keys/indexes to 32.

SCHEMA.md is authoritative. Omitted nullable means false. Table/column/index ordering is nonsemantic; key/index column ordering is semantic. Physical column position is not modeled. Primary-key backing index names are reserved as TABLE_pkey; no automatic truncation or suffix guessing. Explicit rename metadata is deferred to session 3.

Examples, a populated PostgreSQL SQL fixture, disposable local Compose setup, dependency notices, scripts/check.sh, unit tests, fuzz target and a GitHub Actions unit/build workflow are committed. No binaries or releases have been published. Keep the engine all rights reserved.

Website: replaced inherited pages/posts and old demo/image assets; retained the Launch theme, all 18 theme blobs byte-for-byte, existing layout sections and menu structure. Updated project copy and destinations only, added a workflow SVG and accurate status/build/schema/workflow pages. Domain connection remains the owner's task.

## Validation evidence
Local Go 1.26.1: scripts/check.sh passed (go test -race ./..., go vet ./..., go build, validation of both examples). Final fuzz run passed after 11,813 executions; prior run passed after 95,668 executions. Final schema statement coverage 89.9%, CLI package 93.1%; the small main wrapper is exercised by command smoke checks rather than instrumented package tests. Raw final output: test/results/session-1.txt.
Hugo 0.158.0 built the replacement website successfully (24 generated pages). Generated internal links and asset paths all resolved; no inherited AltSql branding remained in generated HTML. Remote tree comparison confirmed all theme blobs unchanged.
GitHub Actions API reported zero runs for the implementation and website commits when checked. A hosted CI result and website deployment have NOT been verified; local results are the evidence.

## Environment limitations and tooling
No Go, Hugo, Docker or PostgreSQL was initially installed. Go and Hugo were downloaded to /tmp for local validation; these tools are transient. Package-manager installation failed due to container identity restrictions; no escalation was attempted. Docker/Compose fixture execution and live database validation were NOT performed. This is the first task to resolve in session 2, using a real disposable PostgreSQL instance.
Public clone and fetch work via shell, but shell pushes and private clones lack credentials. Use the GitHub connector's create_tree/create_commit/update_ref (with expected head SHA) to publish changes atomically. Current website source is in the private repository; do not infer deployed access from successful repository writes.

## Next session, only after go-ahead
Read PLAN.md and SCHEMA.md. Establish and run the PostgreSQL fixture. Implement read-only catalog inspection, using the same canonical model and explicit managed-schema scope. Handle primary-key indexes separately from ordinary indexes. Normalize supported built-in types and flag defaults, identity/generated columns, unsupported constraints/index features and identifiers rather than silently losing metadata. Test repeated inspection and fixture equivalence against actual PostgreSQL. Do not implement migration execution yet.
At session end run appropriate checks, publish code, update this file and website with actual results, report both commit IDs and pause for the next go-ahead.
