# AI Push Message Generation Implementation Plan

Status: accepted
Date: 2026-06-21

The user approved implementation after two iterative plan-review loops reported
no remaining actionable findings.

## Phase Sequence

1. Resolve generation command contract and failure semantics.
2. Add gitexec support for editor-reviewed commit messages seeded from a file.
3. Add prompt generation and external command execution in the shared push path.
4. Wire generated-message review into `braid push` and sync push actions.
5. Add focused tests for success, failure, no-op, and sync behavior.
6. Update README and run targeted plus full gates.
7. Request user review and record plan acceptance before implementation closure.

## Delivery Approach

- Keep the feature behind user-level environment configuration.
- Reuse the existing push temporary repository and cleanup lifecycle.
- Keep Git operations in `internal/gitexec`; use command execution outside
  `internal/gitexec` only for the user-configured non-Git generator command.
- Build prompt context from data available after temporary push repository setup:
  current mirror config, base revision, synthetic `newTree`, provenance commits,
  downstream repository root, and staged upstream diff.
- Preserve the current editor flow as the human review gate.
- Make tests use local shell scripts as fake generators; do not require network,
  Codex, global Git identity, real Braid cache, or external remotes.

## Planned Implementation Shape

- Split raw push provenance collection from commented-template formatting so
  generation can use provenance data even when `core.commentChar=auto`; keep
  existing commented guidance behavior for the non-generation path.
- Add a small command-message generation module under `internal/command`.
- Update `internal/command/BUILD.bazel` if a new source file is added.
- Resolve configuration from an environment variable using `os.LookupEnv`.
- Treat unset or empty `BRAID_PUSH_COMMIT_MESSAGE_COMMAND` as disabled.
- Replace the single temp repo directory with one temp workspace that is removed
  by one deferred cleanup and contains:
  - `push-repo/` for the isolated upstream commit repository
  - `context/` for generator prompt, message, seed, and large-diff files
- Inside `pushViaTempRepo`, after temp repo init/config/alternates/HEAD setup,
  sparse checkout setup, and `read-tree -um <newTree>`, populate `context/`
  with:
  - prompt file
  - generated message file
  - large diff context file when the upstream diff exceeds 5 KiB
  - editor seed file
- Format the prompt with:
  - instruction block
  - mirror metadata
  - downstream commit provenance
  - upstream diff between `baseRevision` and `newTree`, inlined only when it is
    at most 5 KiB
  - path to a temporary full-diff context file when the diff is larger than 5
    KiB
  - path where the generator must write the message
- Produce the upstream diff from the temporary push repository with
  `git diff --cached --no-color --no-ext-diff --no-textconv --binary HEAD --
  [<remote-path>]`; omit the pathspec when the mirror has no remote path.
- Measure the inline threshold using the byte length of that raw diff output.
- Treat provenance collection failure as optional section failure: warn, add a
  short unavailable note to the prompt, and continue generation using metadata
  and diff context.
- Treat temporary workspace creation, context directory creation, prompt/seed
  writes, context file writes, diff generation, and index verification as fatal
  infrastructure failures that stop before the editor opens.
- Treat nonzero generator exit, missing generated-message file, and
  whitespace-only generated-message content as recoverable generator output
  failures that open the editor with commented diagnostics. Treat unexpected
  I/O errors while inspecting the generated-message file as fatal
  infrastructure failures.
- Run the configured command through the shell after substituting approved
  placeholders: `{REPO_DIR}`, `{CONTEXT_DIR}`, `{PROMPT_FILE}`, and
  `{MESSAGE_FILE}`.
- Use `/bin/sh -c` on Unix-like systems, working directory `{REPO_DIR}`, inherited
  environment, and captured stdout/stderr. Do not write generator output to Braid
  stdout; include at most 4 KiB from stdout and at most 4 KiB from stderr in
  failure diagnostics, appending `[truncated after 4096 bytes]` to any stream
  that exceeded the limit.
- If `BRAID_PUSH_COMMIT_MESSAGE_COMMAND` is set on Windows, fail before
  generator/editor execution with a clear unsupported-platform diagnostic. If it
  is unset or empty, keep existing behavior unchanged.
- Shell-quote substituted placeholder values. Leave unknown placeholder-like
  text literal.
- Validate that the generated message file exists and has non-whitespace content.
- Choose the seed comment character from the source repository's single-character
  `core.commentChar` when set and not `auto`; otherwise use `#` in the temporary
  push repository.
- If generation fails, prepare the normal commit editor template with a
  commented diagnostic describing the generation error.
- Prepare one combined seed file:
  - success: generated message, blank line, commented provenance guidance when
    available
  - failure: commented generation diagnostic plus normal commented guidance
- Open Git's editor by calling a gitexec helper that runs
  `git commit --cleanup=strip -v -F <seed-file> -e`.
- Add a named `internal/gitexec` helper for `git write-tree`; before committing,
  use that helper to verify the temporary push repository index still equals the
  expected `newTree`; fail before commit/push if it differs.
- Commit only after Git accepts the editor-reviewed message.

## High-Risk Areas

- Shell command template injection:
  - Impact: The env var is arbitrary trusted user code.
  - Mitigation: Document it as trusted local configuration, shell-quote
    substituted paths, and do not interpolate untrusted repository content into
    the command line.
- Broken generation commands:
  - Impact: A push could fail after Braid has done setup work.
  - Mitigation: Open the normal commit editor template with commented failure
    guidance, then rely on the existing editor review gate; do not mutate
    upstream if the editor fails.
- Shell runner behavior:
  - Impact: Platform, quoting, output, and typo behavior can become inconsistent.
  - Mitigation: Specify POSIX `/bin/sh -c`, shell-quoted documented
    placeholders, literal unknown placeholders, repo-root working directory,
    inherited environment, captured output, 4 KiB per-stream failure excerpts,
    and tests for spaces, quotes, empty configuration, no output file,
    whitespace-only output, nonzero exit, and output truncation. Configured
    generation is unsupported on Windows in this initial version.
- Large prompts:
  - Impact: External generators may fail or produce poor messages.
  - Mitigation: Inline diffs only up to 5 KiB, write larger diffs as temporary
    context files, and document that sandboxed generators must be able to read
    those referenced files.
- Commit cleanup interaction:
  - Impact: Commented provenance guidance could leak into commit messages.
  - Mitigation: Use a combined seed file and `git commit --cleanup=strip -v -F
    <seed-file> -e`; set deterministic temporary `core.commentChar` and test
    `commit.cleanup=whitespace` plus `core.commentChar=auto`.
- Provenance refactor:
  - Impact: Existing provenance collection is coupled to comment-char lookup, so
    generated prompts could lose provenance when `core.commentChar=auto`.
  - Mitigation: Split raw provenance data collection from commented formatting,
    keep old non-generation skip behavior where needed, and test generation
    success with `core.commentChar=auto` plus `commit.cleanup=whitespace`.
- Sync multi-push behavior:
  - Impact: A single `braid sync` can invoke multiple generators and editors.
  - Mitigation: Document that generation is per pushed mirror when sync pushes
    more than one mirror, and accept/test existing sequential partial-state
    behavior when a later mirror fails after an earlier push completed.
- Trusted generator side effects:
  - Impact: Braid cannot prevent arbitrary configured shell code from changing
    the downstream repository.
  - Mitigation: Narrow the guarantee to Braid's own plumbing, keep context files
    outside the worktree and temporary push repository, and verify the temporary
    push repository index still matches `newTree` before commit.

## Required Full Gates

```bash
bazel test //...
```

## Targeted Gates

```bash
bazel test //internal/gitexec:gitexec_test
bazel test //internal/command:command_test
git diff --check
```

## Decision Log

Locked user-supplied decisions are recorded in `00-requirements.md`:

- Generation is automatic when configured.
- Every generated message is reviewed in Git's editor before commit.

| id | decision | plan impact |
| --- | --- | --- |
| Q-01 | Use `BRAID_PUSH_COMMIT_MESSAGE_COMMAND` as trusted shell template with shell-quoted `{REPO_DIR}`, `{PROMPT_FILE}`, and `{MESSAGE_FILE}` placeholders. | Implement environment lookup for that exact variable, substitute only the approved placeholders, execute through the shell, and document the variable as trusted local shell code. |
| Q-02 | When generation fails or produces no usable message, open the normal commit editor template with a commented diagnostic describing the error. | Add a generation-failure template path that preserves manual commit progress, appends actionable commented diagnostics, and forces cleanup so diagnostics are not committed if left in place. |
| Q-03 | Inline diffs up to 5 KiB; write larger diffs to temporary context files and reference those file paths from the prompt. | Add prompt formatting that switches diff handling at 5 KiB, creates a full-diff temp file for large diffs, and documents filesystem-read requirements for sandboxed generator commands. |
| Q-04 | Apply generation and editor review to each actual upstream push commit created by `braid push` or the push phase of `braid sync`. | Keep integration in the shared push path, add sync tests for per-push generation, and document that `sync --pull-only`, no-change mirrors, and not-up-to-date stops do not run generation. |
| Q-05 | Add `{CONTEXT_DIR}` as a shell-quoted command-template placeholder and store prompt, message, and large diff files under that temporary context directory. | Extend placeholder substitution, document `--add-dir {CONTEXT_DIR}` style usage for sandboxed tools, and keep generated context files outside the downstream worktree. |

## Review Fix Log

| round | fix |
| --- | --- |
| R1 | Pinned generated-message commit helper semantics to a combined seed file plus `git commit --cleanup=strip -v -F <seed-file> -e`. |
| R1 | Defined deterministic seed comment char behavior for generated/failure templates, including `core.commentChar=auto`. |
| R1 | Moved prompt/diff generation sequencing into `pushViaTempRepo` after temporary repository setup and `read-tree`. |
| R1 | Pinned upstream diff command, pathspec handling, binary/color/textconv behavior, and byte-count threshold. |
| R1 | Defined POSIX shell runner behavior, output capture, empty env handling, and unknown-placeholder behavior. |
| R1 | Narrowed the worktree/index guarantee to Braid-owned plumbing and added a temporary index verification before commit. |
| R1 | Accepted and documented existing sequential sync partial-state behavior. |
| R1 | Classified provenance collection failure as optional prompt-section failure, not generator failure. |
| R2 | Specified a single temporary workspace with separate `push-repo/` and `context/` children and one cleanup path. |
| R2 | Defined configured generation as unsupported on Windows unless the env var is unset or empty. |
| R2 | Pinned generator failure diagnostic excerpts to 4 KiB per stream with an explicit truncation marker. |
| R3 | Classified recoverable optional prompt-section failures separately from fatal infrastructure failures. |
| R3 | Added explicit provenance refactor requirement so raw provenance is available for generated prompts with `core.commentChar=auto`. |
| R3 | Added named gitexec write-tree helper requirement for temporary index verification. |
| R3 | Added Bazel metadata and README sync partial-state documentation requirements. |
| R4 | Split recoverable generator output failures from fatal generated-message file I/O failures. |

## Completion Criteria

- All open questions are resolved and reflected in this plan.
- User review of the latest plan is explicitly requested and recorded.
- All planned implementation tasks are completed.
- Evidence is recorded for each completed task.
- Required full gate passes.
- Working tree is clean unless the user explicitly defers commits.
