# Output Contract

Source transactions use names:

- `Braid: Add source 'replicant' at '18480c9'`
- `Braid: Update source 'replicant' to 'abcdef0'`
- `Braid: Remove source 'replicant'`
- Progress refers to `source :replicant`.

Topology changes distinguish mirrors:

- `Braid: Add mirrors to source 'replicant'`
- `Braid: Remove mirror 'licenses/replicant-LICENSE.txt' from source 'replicant'`
- Removing the last mirror uses the source-removal subject.

Status remains one line per mirror and distinguishes content from source drift, for example:

`licenses/replicant-LICENSE.txt (REVISION) [BRANCH=main] (Up To Date, Behind)`

Diff headings identify mirror local paths. Errors for a source-wide action name the source and every relevant mirror path.

Progress start/completion is on stderr; quiet suppresses progress only and preserves warnings, errors, and data. No-op/locked-skip output uses `:name`; ordering is source name then local mirror path. `--no-commit` reports one staged source transaction or the specific topology change. Conflict output lists paths and recovery commands for all affected mirrors plus `.braids.json`. Existing revision/tracking status detail remains and is augmented by separate content/source-drift states.

Representative exact contracts:

- stderr progress: `Braid: fetching source :replicant` / `Braid: fetched source :replicant`; checking, updating, pushing, and cache operations use the same start/past-tense shape.
- stdout no-op: `Braid: source :replicant is already up to date`.
- stdout locked block: `Braid: skipped revision-locked sources:\n  :replicant`.
- stdout no-commit: `Braid: staged update of source ':replicant'`, `Braid: staged addition of mirrors to source ':replicant'`, or `Braid: staged removal of mirror 'PATH' from source ':replicant'`.
- conflict begins `Braid: conflicts while updating source :replicant:` followed by sorted indented paths, then `Resolve them, then run:` and a `git add --` command containing every source mirror path plus `.braids.json`, followed by `git commit -F <MERGE_MSG>`.
- status: `PATH (REVISION) [TRACKING] (CONTENT_STATE, SOURCE_STATE)` where states use the exact title-cased names from requirements.
- diff heading: `Braid: Diffing mirror PATH` inside the existing separator lines.
- push/sync partial progress names completed sources as sorted `:name` entries and states whether rerunning `braid pull :name` or `braid sync :name` is required.

Tests may preserve more detailed existing wording, but these tokens, stream ownership, selector spelling, and deterministic ordering are mandatory.
