# muxunit examples

Runnable scripts that exercise the CLI on fabricated but realistic CI data.
Both are offline and idempotent.

| Script | What it shows |
|---|---|
| `make-shards.sh <dir>` | Fabricates the demo inputs: two JUnit shards, one TAP e2e stream, and a retry shard. Used by the README quickstart. |
| `ci-gate.sh <shard-dir> <baseline.xml> <out.xml>` | The everyday pipeline step: merge all shards with `--dedupe prefer-pass`, print a summary, and `diff` against the last green run to gate on regressions. |

Quick run-through:

```bash
go build -o muxunit ./cmd/muxunit && export PATH="$PWD:$PATH"
bash examples/make-shards.sh /tmp/muxunit-demo/shards
muxunit merge --dedupe prefer-pass -o /tmp/muxunit-demo/baseline.xml \
  /tmp/muxunit-demo/shards/*.xml /tmp/muxunit-demo/shards/*.tap
bash examples/ci-gate.sh /tmp/muxunit-demo/shards \
  /tmp/muxunit-demo/baseline.xml /tmp/muxunit-demo/merged.xml
```

The last command exits `0` because nothing regressed against the baseline;
edit one of the shards to flip a test red and watch it exit `1`.
