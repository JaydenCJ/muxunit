# Contributing to muxunit

Issues, discussions and pull requests are all welcome.

## Getting started

You need Go ≥1.22; nothing else — no external services, no test data to
download.

```bash
git clone https://github.com/JaydenCJ/muxunit && cd muxunit
go build ./...
go test ./...
bash scripts/smoke.sh
```

`scripts/smoke.sh` builds the binary, fabricates JUnit and TAP shards in a
temp dir, and asserts on real CLI output across every subcommand; it must
finish by printing `SMOKE OK`.

## Before you open a pull request

1. `gofmt -l .` reports nothing (formatting is enforced).
2. `go vet ./...` passes with no findings.
3. `go test ./...` passes (89 deterministic tests, no network).
4. `bash scripts/smoke.sh` prints `SMOKE OK`.
5. Add tests for behavior changes; keep logic in pure, unit-testable
   modules (parsers and algebra never touch the filesystem — only
   `internal/cli` does I/O).

## Ground rules

- Keep dependencies at zero — muxunit is a single static binary built from
  the Go standard library, and staying that way is the point.
- No network calls, ever. No telemetry. Inputs are files and stdin; outputs
  are files and stdout.
- Determinism first: identical inputs must produce byte-identical reports,
  including all orderings. Any new output path needs a determinism test.
- Be liberal in what you parse (real-world JUnit/TAP is messy), strict in
  what you write; new dialect quirks belong in the parser with a test
  reproducing the real producer's output.
- Exit codes 0/1/2/3 are a public API — never repurpose them.
- Code comments and doc comments are written in English.

## Reporting bugs

Include the output of `muxunit version`, the full command you ran, and a
minimal input report that reproduces the problem (redact test names if
needed — structure matters more than names). For parse bugs, note which
tool produced the file (pytest, Gradle, prove, node-tap, …) and its version.

## Security

Please do not open public issues for security problems; use GitHub's
private vulnerability reporting on this repository instead.
