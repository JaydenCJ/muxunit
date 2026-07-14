#!/usr/bin/env bash
# A realistic post-shard CI step: merge every shard the runners uploaded,
# publish one JUnit artifact, then gate the pipeline on regressions against
# the last green run. Usage:
#
#   bash examples/ci-gate.sh <shard-dir> <baseline.xml> <out.xml>
#
# Exit code is muxunit's own: 0 = clean, 1 = regressions, 2/3 = errors.
set -euo pipefail
shopt -s nullglob

SHARDS="${1:?usage: ci-gate.sh <shard-dir> <baseline.xml> <out.xml>}"
BASELINE="${2:?missing baseline report}"
OUT="${3:?missing output path}"

FILES=("$SHARDS"/*.xml "$SHARDS"/*.tap)
if [ ${#FILES[@]} -eq 0 ]; then
  echo "ci-gate: no shard reports in $SHARDS" >&2
  exit 3
fi

# Retried shards may contain the same test twice; a run that eventually
# passed should count as a pass.
muxunit merge --dedupe prefer-pass -o "$OUT" "${FILES[@]}"

# Human-readable roll-up in the job log.
muxunit summary "$OUT"

# Fail the job only on NEW red — pre-existing failures are the baseline's
# problem, not this PR's.
muxunit diff "$BASELINE" "$OUT"
