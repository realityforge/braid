# CLI Progress Output Contract

Status: draft, ready for user review

## Stream Classes

- Data output: command-requested data remains on `stdout` and is not suppressed by `--quiet`. This includes `version`, `help`, `status` result lines, and `diff` data.
- Progress/info output: new start, dot, and completed messages write to `stderr` and are suppressed by `--quiet`.
- Warning output: existing and new warnings remain visible under `--quiet`. Existing stream placement is preserved unless a task explicitly changes it.
- Recovery/result output: existing non-progress command-result and recovery output remains visible under `--quiet`. This includes pull conflict summaries/instructions, skipped revision-locked mirror lists, and push stop messages.
- Error output: command errors continue to print through the existing `braid: ...` path on `stderr`.

## Operation Matrix

| Area | Condition | Start Message | Success/No-Op Message | Writer | Quiet |
| --- | --- | --- | --- | --- | --- |
| Add default branch detection | `add` without `--branch`, `--tag`, or `--revision` runs `ls-remote` | `Braid: detecting default branch for mirror <path>` | `Braid: detected default branch for mirror <path>` | `stderr` | suppress |
| Cache hydration | Cache enabled and a command calls `fetchCache` for a mirror URL | `Braid: updating cache for mirror <path>` | `Braid: updated cache for mirror <path>` | `stderr` | suppress |
| Mirror fetch | A command calls `fetchMirror` for a mirror | `Braid: fetching mirror <path>` | `Braid: fetched mirror <path>` | `stderr` | suppress |
| Pull/sync revision check | `pull` or `sync` has fetched a mirror and resolves whether the selected revision differs from the recorded revision | `Braid: checking mirror <path>` | `Braid: checked mirror <path>` when an update will proceed, or `Braid: mirror <path> already up to date` when remote work found no update | `stderr` | suppress |
| Pull/sync content update | Revision check found a new revision or tracking strategy change and starts local content/config update | `Braid: updating mirror <path>` | `Braid: updated mirror <path> to <short-revision>` | `stderr` | suppress |
| Push upstream | `push` or `sync` has a changed, up-to-date branch mirror and starts upstream push | `Braid: pushing mirror <path>` | `Braid: pushed mirror <path>` | `stderr` | suppress |
| Diff remote hydration | `diff` cannot resolve the recorded base revision locally and must fetch cache or mirror data | use cache/mirror fetch rows | use cache/mirror fetch rows | `stderr` | suppress |
| Status remote check | `status` checks whether a mirror's remote revision moved | use cache/mirror fetch rows | use cache/mirror fetch rows | `stderr` | suppress |
| Setup local remote | `setup` adds or replaces a Braid-managed Git remote locally | `Braid: setting up mirror remote <path>` | `Braid: set up mirror remote <path>` | `stderr` | suppress |

## Setup Scope

`setup` must not hydrate the cache or contact upstream repositories. It may emit progress only for local Braid-managed remote configuration changes. If the remote already exists and `--force` is not set, setup may stay quiet for that mirror because no operation was performed.

## Existing Non-Progress Output

- Pull conflict summaries, merge details, unrelated-staged warnings in the conflict instruction block, and recovery commands keep their existing `stdout` placement.
- Push provenance warnings keep their existing `stderr` placement.
- Push stop messages such as `Braid: Mirror is not up to date. Stopping.` and `Braid: No local changes found in downstream HEAD. Stopping.` keep their existing `stdout` placement.
- Skipped revision-locked mirror lists keep their existing `stdout` placement.

These outputs are not progress output and are not suppressed by `--quiet`.

## Reporter Lifecycle

- Start writes the start message and begins ticking only when progress is enabled.
- TTY progress appends `.` every 5 seconds while the operation is running.
- Non-TTY progress writes bounded line-based start/completed messages and no dots.
- Successful completion stops ticking and writes the success or no-op completion message.
- Failure, cancellation, and early return stop ticking, emit a newline first when dots or same-line progress were written, and do not write a success completion message.
- Reporter tests must use fake ticker/clock behavior and must not sleep for 5 seconds.

## Terminal Probe

Use an injectable terminal probe for tests. The production default should avoid external dependencies unless implementation proves the standard library insufficient; prefer checking `*os.File` writer mode with `os.ModeCharDevice`. If a dependency is introduced, update Bazel/module metadata in the same task.

## CI Matrix Note

The required local gate mirrors the commands in `.github/workflows/ci.yml`. Local execution does not prove the Windows, Linux ARM64, and macOS GitHub Actions matrix. The final implementation report must state whether the GitHub Actions matrix was observed or remains an unvalidated platform gap.
