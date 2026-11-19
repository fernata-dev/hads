# Contributing to hads

Thanks for helping. hads is small on purpose: one Go binary, standard library only, no cgo.

## Setup

```bash
git clone https://github.com/fernata-dev/hads && cd hads
make test     # vet + tests (+ race on Linux)
make build    # ./hads
```

Try changes without touching your real setup by pointing hads at a scratch data folder:

```bash
export HADS_HOME=$(mktemp -d)
./hads daemon && ./hads suggest
```

## Ground rules

- **Standard library only.** A dependency needs a very good reason.
- **Every OS.** CI runs on Linux, macOS and Windows; `make dist` must cross-compile all targets.
- **The hot path stays fast.** `hads predict` runs on every keystroke. Run
  `go test -bench . ./internal/model/` before and after touching the model.
- **Privacy first.** Nothing from prediction leaves the machine. Anything sent in smart mode must be
  documented in the README's Privacy section.
- **Safety guard changes need tests** in `internal/agent/safety_test.go`, for both what is blocked and
  what must still be allowed.
- **Shell scripts:** zsh/bash/fish must pass `-n`; `hads.ps1` must stay ASCII (Windows PowerShell 5.1).
- User-facing strings come in English and Egyptian Arabic.

## Pull requests

Keep them focused, describe what changes for the user, and add or update tests. Conventional commit
prefixes (`feat:`, `fix:`, `docs:`) are appreciated.

## Releases

Push a tag `vX.Y.Z`: the release workflow tests, builds every platform, writes `checksums.txt` and
publishes the GitHub release. The installers always fetch the latest release.
