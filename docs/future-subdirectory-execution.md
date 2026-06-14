# Future Subdirectory Execution

Braid v1 intentionally runs only from the root of the downstream Git worktree.

Supporting subdirectory execution is a future enhancement because it requires every command to normalize local mirror paths, config paths, Git pathspecs, diff prefixes, temporary indexes, and conflict paths relative to both the process directory and the repository root. The Go port keeps the initial behavior explicit: repository commands fail before mutation when `git rev-parse --show-prefix` reports a non-empty prefix.
