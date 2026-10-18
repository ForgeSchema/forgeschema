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

The plan agreed with the owner on 2026-10-18 is done and went out as 0.3.0:

1. The low maintenance workflow: automated releases, this file, recorded results.
2. Online plans: `plan --online` builds indexes concurrently, and apply resumes after a failed build.
3. Check constraints in a small grammar that round-trips through PostgreSQL.
4. Explicit drops and type conversions in the change file, and staged expand and contract changes (PLANNING.md).

An independent review before the release found a deadlock between an online build and a waiting apply, lock_timeout cancelling builds, unsafe drops of keys a foreign key needs and lossy conversions; all fixed with tests. Results for 0.3.0 are in test/results/ (PostgreSQL 16.15 locally; CI runs 16.10 and 17.6).

What comes next hasn't been decided. Agree it with the owner before it goes here, and start work only when the owner says go.
