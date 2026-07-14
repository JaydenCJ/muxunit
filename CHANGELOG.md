# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-07-13

### Added

- Canonical format-neutral report model (suites, cases, four statuses,
  identity keys) with deterministic sorting, so every operation is
  independent of shard arrival order.
- JUnit XML reader/writer: `<testsuites>` and bare `<testsuite>` roots,
  nested-suite flattening, failure/error/skipped precedence, properties,
  system-out/err, locale-tolerant durations, recomputed counters on output.
- TAP reader/writer (v12–14 as produced by prove, node-tap, bats): plans,
  SKIP/TODO directives, YAML diagnostic blocks with `message:` promotion,
  bail-outs, `\#` escaping, and synthetic errors for truncated streams.
- `merge` subcommand with dedupe policies `all`, `first`, `last`,
  `prefer-pass`, `prefer-fail` for retried-shard resolution; single-input
  merge doubles as a junit/tap/json converter.
- `diff` subcommand bucketing changes into new-failures, fixed,
  still-failing, status-changed, added, removed — with a regression gate
  (exit 1) tunable via `--fail-on regressions|any-change|nothing`.
- `filter` subcommand selecting cases by status (`--status`,
  `--only-failed`) and by `*`/`**`/`?` globs over `suite/class/name` IDs,
  with `--invert` for quarantine workflows.
- `rewrite` subcommand: exact suite renames, prefix add/trim, sed-style
  `--sub /re/repl/` case-name substitutions with capture groups, classname
  scrubbing, and `--strip-times` for reproducible artifacts.
- `summary` subcommand with an aligned per-suite table, JSON counts, and a
  `--check` gate that exits 1 on any red test.
- Format auto-detection (extension, then content sniffing), stdin via `-`,
  `-o` file output, and stable exit codes (0/1/2/3).
- Versioned JSON envelope (`schema_version: 1`) shared by report, summary,
  and diff output.
- Runnable examples (`examples/make-shards.sh`, `examples/ci-gate.sh`) and
  a semantics reference (`docs/report-algebra.md`).
- 89 deterministic offline tests (unit + in-process CLI integration) and
  `scripts/smoke.sh`.

[0.1.0]: https://github.com/JaydenCJ/muxunit/releases/tag/v0.1.0
