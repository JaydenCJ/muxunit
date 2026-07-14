# The report algebra

Every muxunit verb operates on one canonical, format-neutral model; JUnit XML
and TAP are just projections of it. This document pins down the semantics that
the CLI flags allude to.

## The model

A **report** is an ordered list of **suites**; a suite has a name, optional
metadata (timestamp, hostname, properties), and **cases**. A case has a name,
an optional classname, a duration, and exactly one status:

| Status | JUnit source | TAP source |
|---|---|---|
| `pass` | `<testcase>` with no child | `ok` |
| `fail` | `<failure>` | `not ok` |
| `error` | `<error>` | `Bail out!`, or a planned point that never arrived |
| `skip` | `<skipped>` | `# SKIP`, `# TODO` |

A case's **identity key** is the triple *(suite name, classname, name)*,
printed as `suite/classname/name` with empty segments collapsed. Two cases
with the same key — in different shards, different files, or different report
versions — are *the same test*. Everything else (order, duration, message)
is payload, not identity.

Severity orders statuses `skip < pass < fail < error`; the dedupe policies
below and the writer's counter recomputation both rely on it.

## Merge

`merge` unifies same-named suites and resolves duplicate keys by policy:

| Policy | Keeps | Use it when |
|---|---|---|
| `all` (default) | every occurrence | you want raw, lossless concatenation |
| `first` | first in input order | earlier shards are authoritative |
| `last` | last in input order | reruns overwrite the original |
| `prefer-pass` | least severe outcome | retried flakes should count as green |
| `prefer-fail` | most severe outcome | any red run must keep the test red |

Ties under `prefer-*` resolve to the **last** occurrence. Suite properties
merge first-wins per key. Output is always sorted by suite name, then
(classname, name) — so merged output is byte-identical regardless of shard
arrival order.

## Diff

`diff old new` joins both reports on the identity key and buckets every
difference:

| Bucket | Meaning | Trips the gate? |
|---|---|---|
| `new-failure` | was green/skipped, now `fail`/`error` | yes |
| `added` (red) | new test that lands already failing | yes |
| `added` (green), `removed` | membership changes | no |
| `fixed` | was red, now passes | no |
| `still-failing` | red in both (incl. `fail`→`error`) | no |
| `status-changed` | remaining transitions (e.g. `pass`→`skip`) | no |

The gate philosophy: a diff fails CI only for *new* red. Pre-existing
failures are the baseline's problem — they stay visible as `still-failing`
but never re-trip the gate. `--fail-on any-change` and `--fail-on nothing`
override this.

## TAP mapping notes

- A TAP stream has no suite concept; the suite is named after the input file
  (`shard-3.tap` → `shard-3`, stdin → `stdin`).
- `# TODO` on a failing point is an *expected* failure and maps to `skip`;
  an unexpectedly passing TODO maps to `pass`.
- A plan that promises more points than the stream delivers produces one
  synthetic `error` case per missing point — a truncated shard must never
  masquerade as green.
- YAML diagnostic blocks are preserved verbatim in the case detail; a
  top-level `message:` key is additionally promoted to the case message.
- On output, multi-suite reports prefix descriptions with `suite > ` since
  TAP is flat; single-suite reports round-trip with bare names. Literal `#`
  is escaped as `\#` both ways.

## Determinism contract

For any inputs, the same invocation produces byte-identical output:
sorted suites and cases, stable JSON field order, fixed-precision durations,
and no timestamps invented by muxunit. This is what makes muxunit artifacts
diffable and cacheable in CI.
