# Architecture Notes

## Release and Versioning policy

* Releases publish raw binaries plus `SHA256SUMS`.
* Version tags use `vX.Y.Z`; binaries print `X.Y.Z`.

## Git And Tests

- Product code that invokes Git should stay behind `internal/gitexec`.
- Tests must not depend on the user's global Git identity, real Braid cache, or
  network remotes.
- Integration tests should create local upstream/downstream repositories in
  `t.TempDir()`, configure local user identity, and disable GPG signing unless
  the test explicitly covers signing config propagation.
