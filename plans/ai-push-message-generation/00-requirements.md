# AI Push Message Generation Requirements

Status: accepted
Date: 2026-06-21

## Mission

Add opt-in AI-assisted upstream commit-message generation for upstream commits
created by `braid push` and the push phase of `braid sync`.

When configured, Braid should build a prompt from the push context, run a
user-provided shell command to generate a proposed commit message, then open
Git's commit editor so the user reviews and edits the generated message before
the upstream commit is created.

## Evidence From Existing Code

- `internal/command/push.go` computes the recorded upstream base revision,
  constructs the upstream `newTree` from downstream `HEAD:<mirror path>`, and
  opens Git's commit editor from an isolated temporary repository.
- `internal/command/push_provenance.go` already collects downstream commits
  that touched the mirror path and formats full commit messages as commit-editor
  guidance.
- `internal/command/sync.go` reuses `PushHandler.push` for each pushed mirror,
  so shared push-path integration covers explicit `push` and sync push actions.
- `internal/gitexec/gitexec.go` exposes commit helpers for editor/template
  flows, but does not yet expose a helper that opens the editor with a generated
  message file.
- Braid's existing user-level runtime configuration uses environment variables
  such as `BRAID_LOCAL_CACHE_DIR` and `BRAID_USE_LOCAL_CACHE`.
- Product code that invokes Git must stay behind `internal/gitexec`.

## Locked Decisions

- Message generation is automatic when the user-level configuration is present.
- The user must review every generated upstream commit message in Git's editor
  before Braid creates the upstream commit.
- The generator is user-supplied external tooling; Braid should not embed a
  model provider or API client.
- Generation applies only after Braid has determined an upstream push commit
  will be attempted.
- Existing no-local-changes and not-up-to-date early stops remain unchanged.

## Scope

In scope:

- Add environment-variable based configuration for the external generation
  command.
- Generate a temporary prompt file for each attempted upstream push commit.
- Provide the command with enough context to write a proposed commit message.
- Include downstream commit provenance, the mirror path, upstream remote path,
  downstream repository root, and the upstream tree diff in the prompt.
- Expose the temporary context directory to the command template so sandboxed
  generator commands can read prompt-adjacent files such as large diffs.
- Open the editor with the generated message prefilled for user review.
- Preserve existing commented provenance guidance when useful.
- Cover `braid push <local_path>` and each pushed mirror in `braid sync`.
- Ensure temporary prompt/message files and the temporary push repository are
  cleaned up through a single temporary workspace cleanup.
- Update README and tests.

Out of scope:

- No built-in AI vendor, authentication, or model configuration.
- No persistence of generated messages or prompts in `.braids.json`.
- No generation for Braid's automatic downstream add/update/remove commits.
- No behavior change for `sync --pull-only`.
- No use of uncommitted mirror changes; push continues to use downstream `HEAD`
  only.
- No non-review mode that commits an AI-generated message without opening the
  editor.

## Command Surface

No CLI syntax changes are planned in the initial design.

User-level configuration:

```bash
BRAID_PUSH_COMMIT_MESSAGE_COMMAND='codex exec -C {REPO_DIR} --add-dir {CONTEXT_DIR} --model gpt-5.5 -c '\''model_reasoning_effort="low"'\'' -o {MESSAGE_FILE} < {PROMPT_FILE}'
```

Supported command-template placeholders:

- `{REPO_DIR}`: downstream repository root.
- `{CONTEXT_DIR}`: temporary directory containing prompt, message, and large
  context files for this push commit.
- `{PROMPT_FILE}`: generated prompt file path.
- `{MESSAGE_FILE}`: path where the generator must write the proposed commit
  message.

Braid shell-quotes substituted placeholder values. The configured command is
trusted local POSIX shell code.

On Windows, configured generation is not supported in the initial design. If
`BRAID_PUSH_COMMIT_MESSAGE_COMMAND` is set on Windows, Braid must fail before
running the generator or opening the editor with a clear diagnostic explaining
that AI message generation requires POSIX `/bin/sh` support. If the environment
variable is unset or empty, Windows behavior remains unchanged.

Affected commands:

```bash
braid push <local_path> [--branch|-b <branch>] [--keep]
braid sync [local_path...] [--autostash] [--keep]
```

Unaffected commands:

```bash
braid sync --pull-only [local_path...] [--autostash] [--keep]
braid add
braid update
braid diff
braid status
braid remove
```

## Behavior Requirements

1. If no generation command is configured, Braid keeps the current editor and
   provenance-template behavior.
2. If a generation command is configured, Braid runs it only when a push commit
   will be attempted.
3. Braid must create one temporary workspace per upstream push commit and remove
   it with one cleanup path.
4. The temporary workspace must contain separate `push-repo/` and `context/`
   child directories.
5. The temporary push repository must be created under `push-repo/`.
6. The temporary context directory must contain the prompt file, message output
   file path, and any large diff context files for that push commit.
7. The prompt must identify the mirror path, upstream URL, upstream path,
   recorded base revision, target branch, and downstream repository root.
8. The prompt must include downstream commits that contributed to the push
   context using the existing provenance collection semantics where possible.
   Provenance collection failure is optional prompt-section failure, not
   generation failure; Braid must warn and still generate from mirror metadata
   and diff context.
9. Prompt-section failures are recoverable only when Braid can still create the
   context directory, prompt file, seed file, and editor seed required to open
   Git's editor safely.
10. Temporary workspace creation, context directory creation, prompt file writes,
    seed file writes, context file writes, and temporary push repository index
    verification are infrastructure failures; Braid must fail before opening
    the editor if any of those operations fail.
11. Diff generation failure is an infrastructure failure unless Braid already
    has enough diff context to generate a prompt safely; the first
    implementation treats diff generation failure as fatal.
12. The prompt must include the diff between the recorded upstream mirror content
   and the synthetic upstream tree that Braid will commit when that diff is at
   most 5 KiB.
13. When that diff is larger than 5 KiB, Braid must write the full diff to a
   temporary context file and reference that file path from the prompt instead
   of inlining the diff.
14. The command template must support `{REPO_DIR}`, `{CONTEXT_DIR}`,
   `{PROMPT_FILE}`, and `{MESSAGE_FILE}` placeholders.
15. Braid must shell-quote all substituted placeholder values.
16. Braid substitutes only documented placeholders; unknown placeholder-like
    text remains literal shell input.
17. An unset or empty `BRAID_PUSH_COMMIT_MESSAGE_COMMAND` disables generation.
18. The command must be run from the downstream repository root through
    `/bin/sh -c` on Unix-like systems, inheriting Braid's process environment.
19. Generator stdout and stderr must be captured, not emitted to Braid stdout.
    On failure, Braid must include at most 4 KiB from stdout and at most 4 KiB
    from stderr in the commented diagnostic.
20. If either captured stream exceeds 4 KiB, Braid must truncate that stream to
    the first 4 KiB and append `[truncated after 4096 bytes]`.
21. The command must be able to write the proposed message to a known temporary
    message file.
22. If the generator exits nonzero, does not create the message file, or writes
    only whitespace, Braid must treat generation as failed and open the normal
    editor template with a commented diagnostic.
23. If Braid cannot inspect the generated-message file because of an I/O error
    other than ordinary non-existence, that is an infrastructure failure and
    Braid must fail before opening the editor.
24. Braid must create a combined commit-message seed file for Git's editor.
25. On generation success, the seed file starts with the generated message,
    followed by any commented provenance guidance.
26. On generation failure, the seed file contains the normal commented guidance
    plus a commented diagnostic describing the generation error.
27. Braid must open Git's editor with:

    ```bash
    git commit --cleanup=strip -v -F <seed-file> -e
    ```

28. The editor-reviewed content is the only content that may become the upstream
    commit message.
29. Braid must not push if the editor exits unsuccessfully.
30. Braid's own generation plumbing must not touch the user's worktree or index.
    The configured command is trusted local shell code and is not sandboxed by
    Braid.
31. Generation must not change pushed tree construction, upstream freshness
    checks, author identity handling, signing config propagation, or remote
    cleanup behavior.
32. Before committing, Braid must verify the temporary push repository index
    still writes the expected synthetic `newTree`; if not, it must fail before
    committing or pushing.
33. Temporary index verification must use a named `internal/gitexec` helper for
    `git write-tree`; product command code must not call generic Git args for
    that verification.
34. `sync` must generate and review a message independently for each mirror that
    the push phase actually pushes.
35. `sync` keeps the existing sequential push behavior: if a later mirror's
    generator or editor fails, earlier upstream pushes may already have
    completed and the update phase is skipped.
36. Prompt or generator failures must open the normal commit editor template
    with an actionable diagnostic included as commented guidance.
37. Generator failure diagnostics must be stripped from the final commit message
    if the user leaves them in place.
38. Generated-message seed comments must use a deterministic temporary-repository
    `core.commentChar`: the source repository's single-character value when it
    is set and not `auto`, otherwise `#`.
39. Generated-message and generator-failure comment cleanup must work even when
    the source repository has `core.commentChar=auto` or
    `commit.cleanup=whitespace`.
40. The generation prompt and diff must be prepared inside `pushViaTempRepo`
    after the temporary repository is initialized, alternates are configured,
    `HEAD` points at the recorded base revision, sparse checkout is configured,
    and `read-tree -um <newTree>` has populated the temporary index.
41. The upstream diff used for the prompt must be produced from the temporary
    push repository with:

    ```bash
    git diff --cached --no-color --no-ext-diff --no-textconv --binary HEAD -- [<remote-path>]
    ```

    When the mirror has no remote path, Braid must omit the final pathspec and
    diff the entire staged upstream tree.
42. The 5 KiB inline threshold is measured as bytes of the raw diff output after
    the Git command completes.
43. Raw provenance collection must be separated from commented-template
    formatting so generation can use provenance data even when the source
    repository has `core.commentChar=auto`.

## Acceptance Criteria

- [ ] With no generation command configured, existing `push` and `sync` tests
      continue to pass unchanged.
- [ ] With generation configured, `braid push` writes a prompt, runs the command,
      opens the editor with the generated message, and commits the user's
      editor-reviewed final message.
- [ ] Command-template substitution supports shell-quoted `{REPO_DIR}`,
      `{CONTEXT_DIR}`, `{PROMPT_FILE}`, and `{MESSAGE_FILE}` placeholders.
- [ ] Diffs up to 5 KiB are included inline in the prompt.
- [ ] Diffs larger than 5 KiB are written to a temporary context file and
      referenced by path from the prompt.
- [ ] The upstream diff command disables color, external diff, and textconv,
      includes binary patch information, applies the remote-path pathspec when
      needed, and uses the documented byte-count threshold.
- [ ] Generated-message review works with commented provenance guidance and does
      not accidentally commit comment guidance when left in the editor.
- [ ] Generated-message review uses
      `git commit --cleanup=strip -v -F <seed-file> -e`.
- [ ] Raw provenance collection is usable by generation with source
      `core.commentChar=auto`; tests cover generation success with
      `core.commentChar=auto` and `commit.cleanup=whitespace`.
- [ ] Fatal infrastructure failures stop before editor open, while recoverable
      generator output failures open the editor with diagnostics and optional
      provenance failures warn and generation proceeds.
- [ ] `braid sync` applies the same generation-and-review flow once per pushed
      mirror.
- [ ] Multi-mirror sync documents and tests the existing partial-state behavior
      when a later generator or editor fails after an earlier push completed.
- [ ] No generation command runs for no-local-changes, not-up-to-date, or
      `sync --pull-only` cases.
- [ ] Generator failures open the normal commit editor template with a
      commented diagnostic, and leaving the diagnostic in place does not include
      it in the upstream commit message.
- [ ] Generator failure diagnostics are stripped with source
      `core.commentChar=auto` and `commit.cleanup=whitespace`.
- [ ] Optional provenance failures warn and still allow generation using mirror
      metadata and diff context.
- [ ] Tests cover empty env var, nonzero generator exit, no message file,
      whitespace-only message file, placeholder quoting with spaces and single
      quotes, 5 KiB diff boundary behavior, and large-diff file readability via
      `{CONTEXT_DIR}`.
- [ ] Tests cover generator stdout/stderr diagnostic truncation at 4 KiB per
      stream.
- [ ] Tests or documented platform checks cover configured generation failing
      clearly on Windows while unset generation remains unchanged.
- [ ] README documents configuration, placeholders, prompt contents, review
      behavior, and failure behavior.
- [ ] README documents per-mirror sync generator/editor prompts and the existing
      multi-mirror partial-state behavior where earlier upstream pushes may
      already have completed if a later generator or editor fails.
- [ ] Targeted command tests, gitexec tests, `git diff --check`, and
      `bazel test //...` pass.

## Quality Gates

Required full gate:

```bash
bazel test //...
```

Targeted gates:

```bash
bazel test //internal/gitexec:gitexec_test
bazel test //internal/command:command_test
git diff --check
```

## Known Intentional Divergences

- This feature is opt-in through user-level configuration; Braid will not choose
  or invoke an AI provider by default.
- The AI-generated message is never trusted without human review in the editor.
- The prompt is generated from committed downstream `HEAD` state, matching
  existing push semantics.

## Open Questions Register

The implementation plan is not final while any question remains open.

| id | status | question | context | options | tradeoffs | recommended_default | user_decision | artifacts_updated |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Q-01 | resolved | What exact environment variable and command-template contract should configure generation? | Existing Braid env vars use the `BRAID_` prefix. The command must receive temporary prompt and message paths and likely needs shell features such as redirection. | `BRAID_PUSH_COMMIT_MESSAGE_COMMAND` with shell-expanded placeholders `{REPO_DIR}`, `{PROMPT_FILE}`, and `{MESSAGE_FILE}`; shorter `BRAID_PUSH_MESSAGE_COMMAND`; or a non-shell argv parser. | The explicit name is verbose but clear. Shell templates fit the user's example but make the env var trusted code. Non-shell argv parsing is safer but less ergonomic for redirection and wrapper commands. | Use `BRAID_PUSH_COMMIT_MESSAGE_COMMAND` as trusted shell template with `{REPO_DIR}`, `{PROMPT_FILE}`, and `{MESSAGE_FILE}` placeholders. | Accepted recommendation: use `BRAID_PUSH_COMMIT_MESSAGE_COMMAND` as trusted shell template with shell-quoted `{REPO_DIR}`, `{PROMPT_FILE}`, and `{MESSAGE_FILE}` placeholders. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml` |
| Q-02 | resolved | What should happen if the generation command fails, writes no message, or produces an invalid message? | The user expects automatic generation plus editor review, but the existing editor-only path could still let a user proceed manually. | Fail the push before opening the editor; warn and fall back to the existing editor/provenance flow; or open the editor with a commented failure note. | Failing is explicit and avoids silent loss of configured automation. Falling back preserves manual progress but can hide broken configuration. A failure note is helpful but requires reliable comment cleanup. | Fail the push before opening the editor when configured generation fails. | User chose normal commit editor template with a commented description of the generation error. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml` |
| Q-03 | resolved | How should Braid bound prompt size when downstream provenance or the upstream diff is large? | The requested prompt may include full commit messages and full diffs. External AI commands may have model-specific limits that Braid cannot know. | Include everything; cap with explicit truncation markers; store large sections as referenced files; or expose size-related env configuration later. | Including everything is simplest but can create oversized prompts. Truncation is predictable but may omit important context. Referenced files preserve full diff content but require the generator to have filesystem read access to the temp context directory. Extra configuration adds surface area. | Cap prompt sections with explicit omitted-content markers and choose conservative fixed limits. | User chose to assume generated commit messages are small, inline diffs up to 5 KiB, and store larger diffs as referenced temporary files. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml` |
| Q-04 | resolved | Should generated-message support apply to `braid sync` by default because sync reuses the push path? | Existing code reuses `PushHandler.push`, so shared implementation naturally covers sync push actions. Sync can push multiple mirrors and therefore may run several AI commands and editor sessions. | Apply to sync by default; restrict to explicit `braid push`; or add separate sync opt-in later. | Default sync coverage is consistent with provenance behavior and existing code reuse. Restricting sync avoids surprise multi-prompt flows but creates a split behavior. | Apply to each actual sync push action by default. | Accepted recommendation: apply generation and editor review to each actual upstream push commit created by `braid push` or the push phase of `braid sync`. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml` |
| Q-05 | resolved | Should the command-template contract expose the temporary context directory for prompt-adjacent files such as large diffs? | Q-03 requires large diffs to be written as files and referenced from the prompt. Tools that sandbox around `{REPO_DIR}` may need an explicit directory argument such as `--add-dir {CONTEXT_DIR}` to read those files. | Add `{CONTEXT_DIR}` placeholder; rely only on absolute paths in the prompt; or place all context files under the downstream repo. | `{CONTEXT_DIR}` keeps temp files outside the worktree while making sandbox access configurable. Prompt-only paths keep the contract smaller but may break sandboxed generators. Writing context files into the repo creates avoidable worktree churn and cleanup risk. | Add `{CONTEXT_DIR}` as a shell-quoted placeholder, with prompt, message, and large-diff files stored under that directory. | Accepted recommendation: add `{CONTEXT_DIR}` as a shell-quoted placeholder and store prompt, message, and large diff files under that temporary context directory. | `00-requirements.md`, `10-implementation-plan.md`, `20-task-board.yaml` |
