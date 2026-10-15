# Release process

The owner authorized the first GitHub release and downloadable binaries on 2026-10-15. Version v0.1.0-alpha.1 is an alpha prerelease under Apache 2.0. This authorization is recorded separately from the earlier six-session prototype checkpoints.

VERSION and internal/cli/cli.go must agree. The Release workflow runs for main-branch changes to VERSION, its own YAML or scripts/release.sh, and supports manual workflow_dispatch. Ordinary documentation/source commits do not publish another release automatically. Future versions require an intentional version change and matching notes; existing release names are never overwritten.

The reusable Test workflow runs the PostgreSQL 16.10/17.6 suites and real CLI walkthrough plus browser replay checks. Only successful validation permits packaging. scripts/release.sh cross-compiles CGO-disabled Linux/macOS/Windows binaries for amd64 and arm64, includes licenses/notices/guides, smoke-tests Linux amd64 and computes SHA256SUMS. Other platforms are cross-compiled, not native-runtime tested. Archives are unsigned and not notarized.

The publishing job alone has contents: write. It downloads the built artifacts, verifies all checksums and seven expected files, creates a draft prerelease targeting the exact tested commit, checks that all seven assets uploaded and then publishes. GitHub creates the version tag at that commit. No external secret is needed; publishing uses the scoped GitHub Actions token. If publication fails after creating a draft, inspect that draft and its assets before resuming; the workflow refuses to overwrite an existing release.

After publication, verify the public release/tag/assets, download the Linux archive and SHA256SUMS, check its digest and run its version/validation commands. Update the existing website download page and RESUME.md with the actual release URL, tested commit, workflow results and platform limitations. Preserve the website theme and contact.
