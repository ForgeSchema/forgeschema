# Releasing ForgeSchema

Releases go out on their own. Nobody clicks Publish, and the owner is never asked to publish one. This is the same setup as HyperCrux's, trimmed to one engine.

## The short version

1. Raise `const Version = "X.Y.Z"` in `internal/cli/cli.go`.
2. Put what changed in the commit message. The release notes list the files; forgeschema.com's posts carry the story.
3. Push to `main`.

When the `test` workflow passes on that commit, the `release` workflow builds the six archives, runs the smoke test on the build machine and again on Linux ARM64, macOS (Apple silicon) and Windows (x86-64), then tags the commit `vX.Y.Z` and publishes "ForgeSchema vX.Y.Z" with the archives and SHA256SUMS. If any build or check fails, nothing is tagged and nothing goes public. Fix it, push again, and the next green run picks it up.

Pushing without raising the version is fine: the plan sees the tag exists and stops quietly.

## What it does

- **plan** ([scripts/release-plan.py](scripts/release-plan.py), cases in [scripts/release-plan-test.py](scripts/release-plan-test.py), which CI runs). Goes on only after a passing `test` run for a push to `main` in this repository, and only if `main` still points at that commit. Reads the version from `internal/cli/cli.go`. Stops if `vX.Y.Z` is a tag already. Fails loudly if the version isn't X.Y.Z or isn't above the newest X.Y.Z tag. The two alpha prereleases, v0.1.0-alpha.1 and v0.2.0-alpha.1, came before this workflow; their tags exist, so they're "out already".
- **build** ([scripts/release.sh](scripts/release.sh)). Cross-compiles CGO-free binaries for Linux, macOS and Windows on amd64 and arm64, packs each with the licences and reference guides, smoke-tests Linux x86-64 and writes SHA256SUMS.
- **check.** Unpacks the Linux ARM64, macOS Apple silicon and Windows x86-64 archives on their own runners and runs [scripts/smoke.sh](scripts/smoke.sh): the version, strict validation, the byte-identical example plan and a full plan of examples/shop.yaml. macOS Intel and Windows ARM64 are cross-compiled and not run.
- **release.** Runs only after every build and check passed. Creates the tag and the release in one step with `gh release create --target <commit> --latest` on the repository's own token (`contents: write` on this job only). A concurrency group per tag stops two runs publishing twice.

Releases are full releases, not prereleases, so `releases/latest` always points at the newest one.

## Files

| System | File |
|---|---|
| Linux, x86-64 | forgeschema_linux_amd64.tar.gz |
| Linux, ARM64 | forgeschema_linux_arm64.tar.gz |
| macOS, Apple silicon | forgeschema_darwin_arm64.tar.gz |
| macOS, Intel | forgeschema_darwin_amd64.tar.gz |
| Windows, x86-64 | forgeschema_windows_amd64.zip |
| Windows, ARM64 | forgeschema_windows_arm64.zip |
| Checksums | SHA256SUMS |

Archive names carry no version, so links like `https://github.com/ForgeSchema/forgeschema/releases/latest/download/forgeschema_linux_amd64.tar.gz` keep working from one release to the next, and the site's download page doesn't change per release. (The two alpha prereleases used versioned names; their links stay as they were.) Binaries are unsigned.

## After pushing: checking it went out

Cloud sessions can't create tags or releases, and `gh`'s GraphQL calls are blocked there. The REST API works:

```sh
gh api "repos/ForgeSchema/forgeschema/actions/workflows/test.yml/runs?per_page=1" \
  --jq '.workflow_runs[0] | "\(.status) \(.conclusion) \(.head_sha[0:7])"'
gh api "repos/ForgeSchema/forgeschema/actions/workflows/release.yml/runs?per_page=1" \
  --jq '.workflow_runs[0] | "\(.status) \(.conclusion) \(.head_sha[0:7])"'
gh api repos/ForgeSchema/forgeschema/releases/latest --jq '.tag_name, .name, (.assets[].name)'
```

Then download one archive and SHA256SUMS through the `latest/download` links, check the digest, unpack it and run `forgeschema version`. Only then is the release done. Then add a post on forgeschema.com and a short note on softdevstudio.com.

If the release run failed, read its jobs (`gh api repos/ForgeSchema/forgeschema/actions/runs/<id>/jobs`), fix the cause on `main` and push; the same version goes out on the next green run, since no tag was made. A run that failed for a reason outside the repository can run again whole with `gh api -X POST repos/ForgeSchema/forgeschema/actions/runs/<id>/rerun`.

## Other ways in

- A release published by hand still works: the workflow builds and attaches the files.
- To rebuild an existing tag's files, run the `release` workflow by hand with the tag.
- Releases made by the workflow don't start it again: events from `GITHUB_TOKEN` don't trigger runs.

## Rules that don't change

- The owner doesn't publish releases. If something blocks one, fix it in the repository.
- Versions only go up, as X.Y.Z.
- The CI workflow keeps the name `test`, because `release.yml` listens for it by name, and `workflow_run` only sees workflow files on `main`.
