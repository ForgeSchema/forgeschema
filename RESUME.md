# ForgeSchema resume checkpoint

Updated: 2026-10-15, Apache 2.0 license adoption complete.

Status: COMPLETE AND PAUSED. All six original sessions, the browser-demo follow-up and Apache 2.0 adoption are complete. Await the owner's next direction; do not start another milestone, publish a release, create a tag or change the selected Apache 2.0 licensing automatically.

## Apache License 2.0 adoption

Owner explicitly requested Apache 2.0 for the website and code repositories on 2026-10-15. Both repositories now contain the full canonical Apache License, Version 2.0 in LICENSE and ForgeSchema attribution in NOTICE. The code README/FEATURES/AGENTS/PLAN and browser-demo documentation reflect the chosen license. Proprietary/all-rights-reserved statements have been removed from current project copy.

Engine license/docs commit: 3646f405aaeea005529576fe8bad84b3409a2b83.
Website license/content commit: cc432a0f5ff75a8d68eceef2b627fd4c29db9441.
This resume is a separate checkpoint commit.

The website serves LICENSE.txt and NOTICE.txt, discloses Apache 2.0 in its existing homepage/footer description and About/Build pages, and records the decision in its README and roadmap. The inherited Launch theme retains its MIT license, unchanged at themes/launch/LICENSE.md; THIRD_PARTY_NOTICES.md preserves its attribution verbatim. Engine dependency notices remain unchanged. Apache 2.0 applies to original ForgeSchema work and does not replace third-party licenses.

Validation: all three license copies match /usr/share/common-licenses/Apache-2.0 byte for byte. Hugo 0.158.0 build passed (30 pages, 39 static files); generated internal links/assets resolve; homepage/About/Build include the license disclosure. All 18 theme blobs match the pre-change tree. git diff --check passed. Live HTTPS homepage/About/Build disclose Apache 2.0, and the served LICENSE.txt matches the repository license byte for byte. No executable behavior changed, so no new engine tests were needed for this documentation/license change. No tag, binary release or release automation was added.

## Browser-demo follow-up

Owner authorization: Build browser demo, 2026-10-15. Completed static interactive replay at https://forgeschema.com/demo/ with full-width view https://forgeschema.com/browser-demo/. This is recorded real CLI evidence, not live SQL execution or a database connection. Four views show before/after schemas, six explained SQL operations, preserved customer rows with stale replay rejection, and selectable match/drift reports. Visitors can select PostgreSQL 16.10 or 17.6 and download original artifacts. No credentials or backend service are introduced.

Engine source/docs/CI commit: 7d0b27c49b227a204bbb3104ea58cf430ed23e74.
Website content/static assets commit: d0224151a881a3904404af0d9d7dfc284aed4df5.
CI: https://github.com/ForgeSchema/forgeschema/actions/runs/38704099435 — browser-demo job and both full PostgreSQL jobs passed. The latter include the actual disposable CLI walkthrough. The engine implementation remains 0.0.6-dev with plan format 1.

Source is web/demo/; BROWSER_DEMO.md documents synchronization. python3 scripts/export-browser-demo.py --check validates generated data against both committed recordings. python3 scripts/export-browser-demo.py --site /path/to/forgeschema.com copies four static app files and 32 original artifacts to static/browser-demo/. Source and deployed files must stay synchronized.

Local node --test web/demo/demo.test.cjs passed three DOM-contract tests exercising both recordings, all views, scenario switching/focus announcements, download paths and failure fallback. node --check web/demo/demo.js passed. The checks execute the actual renderer with a DOM harness; they are not a graphical browser test. Browser-control capability was unavailable, so real-browser/graphical QA remains unperformed. No engine Go behavior changed in this follow-up.

Hugo 0.158.0 built 30 pages and 37 static files. Generated internal links/assets resolve. Four copied app files and all 32 copied artifacts match source byte for byte. All 18 theme blobs are unchanged; templates/layout/theme CSS and site structure are preserved. Styling is isolated in the embedded demo document. The contact remains info@forgeschema.com.

The live /demo/ page, full-width HTML, JavaScript, CSS and data JSON were checked over HTTPS. App assets match committed source byte for byte; sampled original downloads from both server versions match. Cloudflare-protected live contact decoded to the correct email. The first /demo/ fetch saw the previous content during deployment; a later check confirmed the new iframe.

This resume update is the final follow-up checkpoint. Stop and await owner direction. Live database editing/execution, expanded migration support and releases are not authorized by this completed task.

## Published checkpoints

Final engine implementation, guides and evidence: 3db9be9d7f6c8481a1166bff8b37de47ed5603c1.
Final website content: 724d4a5c02a45ac31018d9e90f28034d1c591278.
The functional walkthrough commit and exact CI evidence source are recorded in test/results/session-6-postgres-16.10.txt and session-6-postgres-17.6.txt. This resume is a separate checkpoint commit.

## Reproduce and review

Start with WALKTHROUGH.md. A fresh checkout needs Go 1.24+ and an explicitly supplied disposable PostgreSQL 16/17 database. Docker Compose is the documented local option. Set FORGESCHEMA_DEMO_DATABASE_URL explicitly, then run sh scripts/demo.sh --output demo-output. It never falls back to DATABASE_URL and never overwrites an existing output directory.

The harness builds/runs the real CLI, creates a randomly named isolated schema, inspects two synthetic customers, generates and explains a plan, applies six operations and independently verifies. It checks both original rows, a nullable email, new NULL notes and exactly one matching six-operation history record. Stale replay returns CLI exit 1; an injected column produces verify exit 4. Before/after inspection proves verify performed no repair. The harness removes its injected fixture column and verifies the final target.

Default cleanup drops only its newly created schema; metadata history remains. --keep-schema preserves the successful target. Output run.json records cleanup, versions and IDs. Fixture creation/drift injection/cleanup are direct harness SQL, not supported destructive migration features. Compose down discards its disposable database. These privileged fixtures/tests must never target production.

COMMANDS.md documents commands and exits. FEATURES.md defines supported/unsupported capabilities. SCHEMA.md, INSPECTION.md, PLANNING.md, APPLY.md and VERIFY.md retain exact contracts and API details. README.md links the final handoff guides. Actual reports/plans/rows/transcripts for both servers are in examples/walkthrough/. Captured plans describe those original random schemas; generate a new plan for another run.

## Actual final validation

Fresh-checkout functional walkthrough and full CI suite passed on PostgreSQL 16.10 and 17.6:
https://github.com/ForgeSchema/forgeschema/actions/runs/38702626841
Both runs report passed, six operations, two preserved rows, stale exit 1, drift exit 4 and cleanup dropped. Selected verbatim test/command logs are in test/results/session-6-postgres-*.txt; actual sixteen-file artifact sets are committed for each server.

The final implementation/guides/evidence commit also passed both complete PostgreSQL jobs and the fresh-checkout walkthrough:
https://github.com/ForgeSchema/forgeschema/actions/runs/38702950666

Local go test -race ./..., go vet ./..., both command builds and git diff --check passed. scripts/check.sh passed; transcript test/results/session-6-local.txt includes offline examples and planner output. Local database cases skip without the test URL; hosted CI supplies the actual database evidence. Native PostgreSQL/Docker are unavailable locally; do not bypass PostgreSQL root protections.

Earlier tests remain passing: strict input/artifact loading, deterministic planning, populated CLI migration, independent read-only verification, competing apply clients, drift after lock waits, cancellation, statement timeout, SQL/target mismatch rollback, metadata bootstrap and history-insert failure rollback. General process-crash and commit-network-failure fault injection remains unestablished.

Hugo 0.158.0 built 29 pages. All generated internal links/assets resolve; no inherited AltSql branding remains. All 18 theme blobs match the previous site tree. HTTPS homepage now serves Prototype workflow verified; the live walkthrough serves all six sessions complete. Live contact decoded from Cloudflare email protection remains info@forgeschema.com. The first Python HTTP request returned 403; a subsequent ordinary browser-user-agent curl fetched and verified all three pages successfully. No DNS/configuration changes were made.

## Implemented contract and operational limits

CLI 0.0.6-dev, plan format 1. Commands: validate, inspect, plan, apply, verify, version/help. One named schema; strict supported ordinary tables/columns, basic/composite primary keys and ordinary btree indexes. Explicit column renames, new tables/keys/indexes, nullable additions and ordinary index additions are executable. Unsupported changes block the whole plan with no operations. Saved artifacts are rebuilt and validated before execution.

Apply freezes the validated artifact, takes a transaction advisory lock and ACCESS EXCLUSIVE source-table locks, inspects the source after locks under READ COMMITTED, checks fingerprints, validates isolated metadata, executes supported DDL, verifies target and records history atomically. Defaults: lock 5s, statement 30s, overall 60s. A commit error may leave outcome uncertain and reports IDs for investigation.

External DDL must be paused. Advisory locks coordinate ForgeSchema clients only; relation locks do not guard every namespace CREATE, outside-schema dependency or database-wide hook. ACCESS EXCLUSIVE can block application reads/writes. Metadata requires consistent applying-role exclusive ownership/ACLs and initial database CREATE permission.

Verify is READ ONLY/REPEATABLE READ and reads catalog metadata only. It reports supported structural differences without repairs/history writes. A match is a snapshot, not proof of application data, grants/comments, history, commit outcome or future state. The separate harness reads only its own synthetic rows for preservation checks.

Defaults, generated/identity columns, foreign/unique/check constraints, general type/nullability conversion, required additions to existing tables, destructive changes, advanced indexes, backfills, concurrent index builds, other databases, live connected browser UI and MCP remain outside this prototype. Copyright 2026 ForgeSchema. The owner selected Apache License 2.0 for both repositories on 2026-10-15; see LICENSE, NOTICE and third-party notices. No binary release or tag is authorized.

## Continuation instructions

There is no automatically authorized session 7. Read this file, PLAN.md and AGENTS.md, inspect repository state, and obtain the owner's next concrete direction before developing another milestone. Preserve website theme/templates/layout/CSS/structure and info@forgeschema.com. Domain is already connected by the owner.

Repositories: ForgeSchema/forgeschema (engine), photozeitgeist/forgeschema.com (private Hugo website). Public shell fetch works; private clone and pushes lack shell credentials. GitHub connector create_tree/create_commit/update_ref with a fresh expected head publishes changes. CI supplies disposable official PostgreSQL 16.10/17.6 services. Transient local tools are /tmp/go/bin and /tmp/forgeschema-hugo; reinstall normally if absent.
