# Notes for Claude

This is the one file that says where work stands and what comes next.

Releases are automated. To release, raise `const Version` in `internal/cli/cli.go` and push to `main`; [RELEASING.md](RELEASING.md) has the procedure and the checks afterwards. Never ask the owner to publish a release.

The owner's part is the go-ahead on what to build. Everything else (code, tests, docs, CI, releases, forgeschema.com and the softdevstudio.com note) is done here. Agree the next plan with the owner before it goes in this file.

## Standing rules

- Licence: Apache 2.0 for the code, chosen by the owner on 2026-10-15. Keep LICENSE, NOTICE and THIRD_PARTY_NOTICES.md.
- Website, photozeitgeist/forgeschema.com: content and config values only; never touch the theme, layouts, CSS or site structure. Hugo posts take title (double-quoted, no dots or special characters), date (date only), draft: false and tags, plus featured_image when there's a lead image. Public copy discusses licensing for the engine only, not the website or theme.
- Every number on the site comes from test/results/, written by `scripts/record-tests.sh` against a disposable database.
- Before calling a change done: an independent review by a separate agent, fixes, then a fresh recording.
- Plan format 1 stays loadable: never change the SQL or explanation text an existing operation kind produces.
- Keep docs/dev/HISTORY.md as the record of the first sessions; it isn't a to-do list.

## Where it stands

ForgeSchema 0.4.0 is released (enums, arrays, plain views, PostgreSQL 18, the GitHub Action) and softdev-plan item 24 is done. Nothing is in progress; what comes next is agreed with the owner.

The plan agreed with the owner on 2026-10-18 is done and went out as 0.3.0:

1. The low maintenance workflow: automated releases, this file, recorded results.
2. Online plans: `plan --online` builds indexes concurrently, and apply resumes after a failed build.
3. Check constraints in a small grammar that round-trips through PostgreSQL.
4. Explicit drops and type conversions in the change file, and staged expand and contract changes (PLANNING.md).

An independent review before the release found a deadlock between an online build and a waiting apply, lock_timeout cancelling builds, unsafe drops of keys a foreign key needs and lossy conversions; all fixed with tests. Results for 0.3.0 are in test/results/ (PostgreSQL 16.15 locally; CI runs 16.10 and 17.6).

## ForgeSchema 0.4: manage real-world schemas (softdev-plan item 24, approved by YK 2026-11-03)

What and why: inspection rejects any schema with an enum, an array column or a view, so many existing PostgreSQL databases can't be managed at all. PostgreSQL 18 (September 2025) isn't tested and the CI minors are old. The reviewable plan is the selling point, but nothing puts it on a pull request. Releases build on Go 1.24, which is out of support.

Design decisions:
- Phase A, enums and arrays.
  - A document gains an optional `enums` list: name and ordered `values`. Enum names follow the naming policy and share PostgreSQL's type namespace with tables, so they must not collide with a relation, nor with the `_name` array type a table or enum gets.
  - Values are 1 to 63 bytes with no control characters, at least one per enum, no repeats.
  - A column type may name an enum, and any supported base type or enum may be a one-dimensional array: `integer[]`, `character varying(20)[]`, `status[]`.
  - Defaults:
    - An enum column takes a quoted literal that is one of its values; PostgreSQL stores it with a cast, which inspection strips.
    - An array column takes only `'{}'`.
  - Enum and array columns may sit in keys, unique constraints and indexes. A foreign key needs identical types on both sides. Check expressions on enum or array columns stay outside the grammar and are refused with that reason.
  - Plans:
    - `create_enum` runs before tables are created.
    - New values inserted anywhere keep the old values in order, as `add_enum_value` with BEFORE or AFTER. ALTER TYPE ADD VALUE runs inside the transaction, but PostgreSQL won't let the same transaction use the new value, so a plan whose new defaults use a value added in that plan is blocked with the reason.
    - Removing or reordering values, dropping an enum still in the source, and type changes into or out of an enum or array are blocked.
  - Canonical JSON omits `enums` when empty, so existing fingerprints and plan IDs don't change.
- Phase B, plain views.
  - A document gains an optional `views` list: name, `query`, and `depends_on`, the tables and views the query reads.
  - The query is compared as PostgreSQL prints it: pg_get_viewdef with the managed schema as search_path, whitespace outside literals collapsed, and the trailing semicolon dropped.
  - Apply's existing target verification therefore catches a query written in another form, rolls back, and names the form PostgreSQL wants, so the file converges to the canonical text.
  - `depends_on` is checked the same way against pg_depend.
  - Plans:
    - `create_view` runs in dependency order after every table change.
    - A changed view, and any view that reads it, is dropped first and created again (`drop_view`, then `create_view`).
    - A view that reads a table with a renamed, converted or dropped column, directly or through another view, is dropped before the change and created again after it.
    - A view removed from the target is dropped; it holds no data, and the explanation says so.
    - In online plans, drops go in the first phase and creates in the final one.
  - Inspection accepts plain views in the managed schema. Materialized views, views with options or a check option, recursive views, and views reading other schemas are refused with the reason.
- Phase C, PostgreSQL 18 and the Action.
  - CI runs PostgreSQL 16, 17 and 18 on their major tags, so it always tests the current minors; the recorded results name the exact versions. Inspection accepts 18, including the NOT NULL constraints that 18 stores in pg_constraint. FEATURES.md and INSPECTION.md say 16 to 18.
  - A composite `action.yml` downloads the release binary for the runner (pinned to the action's tag, else latest) and checks SHA256SUMS.
  - The Action runs `plan --explain` against a database URL secret, posts or updates one pull request comment found by a hidden marker, written only by the commenting bot, and fails the step when the plan is blocked. `mode: verify` runs verify after a deploy instead.
  - Tested by a workflow on Linux with a PostgreSQL service, and on the published binary after each release.
- Upkeep: go.mod and README to Go 1.26; release and test workflows on that.
- Version 0.4.0. Done when a schema with enums, arrays and two dependent views round-trips plan, apply and verify, on PostgreSQL 16, 17 and 18.

Steps:
1. [x] Phase 0: this plan.
2. [x] Upkeep (9579489): go 1.26, CI on PostgreSQL 16, 17 and 18 (major tags), inspection accepts 18 and its pg_constraint NOT NULL rows.
3. [x] Phase A (46b773e): enums and arrays; schema/enum_test.go, plan/enum_test.go, postgres/enum_test.go.
4. [x] Phase B (5081f97): views; postgres/view_test.go (round trip, change, rename around views, removal, a query in another form names the stored form, depends_on checked, online plan, verify drift, refusals, document rules, examples/store.yaml round trip). Plan change: recursive views are not refused (a WITH RECURSIVE query is an ordinary query here). Apply's target drift error now lists the differences.
5. [x] Phase C (dc05bd8, 12868fc; action-test green: local and comment jobs on PostgreSQL 17, since the 0.3.0 download step knows 16 and 17 only): action.yml, .github/workflows/action-test.yml (local, comment, download after release on Linux with PostgreSQL, install path on macOS and Windows), ACTION.md; docs (SCHEMA, INSPECTION, PLANNING, APPLY, VERIFY, FEATURES, README, COMMANDS, WALKTHROUGH).
6. [x] Independent review, two rounds, fixes, fresh test/results.
   Review round 1, no blockers. Should-fix, all fixed with tests: an enum named with a keyword (order, user) couldn't round-trip a default, since PostgreSQL quotes only that part of the cast (shop."order"); a view naming an enum value the same plan adds got a ready plan that always failed at apply (now blocked like a default, explanation reworded); README limits still listed views, arrays and enums; the Action's macOS, Windows and download paths had never run (an install job on push now runs them with v0.3.0). Left, documented in APPLY.md: views aren't locked up front, since LOCK TABLE on a view locks its base tables too, so a reader of a dropped view can deadlock with apply (one side is cancelled, nothing committed). Nits fixed: enum values refuse backslashes; the comment fence is longer than any backtick run; the version output is the resolved tag; fallback to latest only when the own release is a 404; a PostgreSQL 18 test that NOT ENFORCED checks and foreign keys are refused. Left: no --explain warning that online plans leave views missing during the builds (in PLANNING.md); whitespace collapsing can turn a multi-line string concatenation into a syntax error (refused at apply).
   Review round 2: the round 1 fixes hold; one should-fix, fixed: the new view check matched 'a'::status inside 'a'::status_old, so it now reads every literal and its cast (plan/enumcast.go), catching array and schema-qualified casts too. Nits fixed: a repeated word in README, INSPECTION.md names the backslash rejection for enum values. Fresh test/results recorded locally (Go 1.24, scratch go.mod, PostgreSQL 16.15); CI runs 16, 17 and 18.
7. [x] 0.4.0 released by the workflow (commit 817dbb7, release run 41702014471). All six archives match SHA256SUMS at the tag and at latest, byte-identical. The downloaded Linux binary printed 0.4.0, planned, applied and verified examples/store.yaml from an empty schema (7 operations), then renamed orders.quantity under both views and added an enum value (views dropped and created again, totals unchanged), and refused a view query in another form with exit 5 and the stored form. Record in test/results/release-0.4.0.txt. The action-test download jobs run on the published binaries after the release.
8. [x] forgeschema.com post "ForgeSchema Now Manages Enums, Arrays and Views and Posts Plans on Pull Requests" with forgeschema-0-4-views.svg (rasterized and inspected), Download, Roadmap, About, Engine and home (site cb8c66e, Cloudflare Pages succeeded; no local Hugo, front matter checked by script); softdevstudio.com note and listing (d5bc5f2); softdev-plan item 24 done with a log row (8ec1a4a).

Local note: this cloud session has Go 1.24 and no Go module proxy. Local runs use a scratch modfile with go 1.24 and the two dependencies cloned from GitHub (`GOFLAGS=-modfile=...`), against a disposable PostgreSQL 16.15. CI is the real check, and the only one for 17 and 18.
