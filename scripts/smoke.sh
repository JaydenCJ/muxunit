#!/usr/bin/env bash
# End-to-end smoke test for muxunit: builds the binary, fabricates JUnit and
# TAP shards in a temp dir, and asserts on the real CLI output of every
# subcommand. No network, idempotent, finishes in seconds.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

fail() {
  echo "SMOKE FAIL: $*" >&2
  exit 1
}

BIN="$WORKDIR/muxunit"

echo "1. build"
(cd "$ROOT" && go build -o "$BIN" ./cmd/muxunit) || fail "go build failed"

echo "2. version matches manifest"
"$BIN" --version | grep -qx "muxunit 0.1.0" || fail "--version mismatch"

echo "3. fabricate CI shards (2x JUnit + 1x TAP)"
cat > "$WORKDIR/shard-1.xml" <<'XML'
<testsuites><testsuite name="api">
  <testcase classname="Auth" name="creates a user" time="0.41"/>
  <testcase classname="Auth" name="rejects bad email" time="0.09">
    <failure message="expected 422, got 500">handler.go:71</failure>
  </testcase>
</testsuite></testsuites>
XML
cat > "$WORKDIR/shard-2.xml" <<'XML'
<testsuites><testsuite name="api">
  <testcase classname="Billing" name="charges once" time="0.33"/>
</testsuite><testsuite name="web">
  <testcase name="renders home" time="0.12"/>
</testsuite></testsuites>
XML
cat > "$WORKDIR/e2e.tap" <<'TAP'
TAP version 13
1..3
ok 1 - login flow
not ok 2 - checkout flow
  ---
  message: button not found
  ...
ok 3 - logout flow # SKIP flaky on headless
TAP

echo "4. merge unifies shards across both formats"
"$BIN" merge -o "$WORKDIR/merged.xml" \
  "$WORKDIR/shard-1.xml" "$WORKDIR/shard-2.xml" "$WORKDIR/e2e.tap" \
  || fail "merge failed"
grep -q '<testsuites tests="7" failures="2" errors="0" skipped="1"' "$WORKDIR/merged.xml" \
  || fail "merged counters wrong"
grep -q 'name="e2e"' "$WORKDIR/merged.xml" || fail "TAP suite not labelled from file name"
grep -q 'button not found' "$WORKDIR/merged.xml" || fail "TAP diagnostics lost"

echo "5. summary reads the merged artifact"
OUT="$("$BIN" summary "$WORKDIR/merged.xml")"
echo "$OUT" | grep -q "TOTAL" || fail "summary total row missing"
echo "$OUT" | grep -Eq 'api +3' || fail "api suite row wrong"
if "$BIN" summary --check "$WORKDIR/merged.xml" >/dev/null; then
  fail "summary --check should exit 1 with red tests"
fi

echo "6. dedupe prefer-pass turns a retried flake green"
cat > "$WORKDIR/retry.xml" <<'XML'
<testsuite name="api">
  <testcase classname="Auth" name="rejects bad email" time="0.08"/>
</testsuite>
XML
"$BIN" merge --dedupe prefer-pass -o "$WORKDIR/retried.xml" \
  "$WORKDIR/shard-1.xml" "$WORKDIR/retry.xml" || fail "dedupe merge failed"
grep -q 'failures="0"' "$WORKDIR/retried.xml" || fail "prefer-pass did not resolve retry"

echo "7. diff gates on regressions with exit 1"
"$BIN" diff "$WORKDIR/shard-1.xml" "$WORKDIR/shard-1.xml" >/dev/null \
  || fail "self-diff should exit 0"
if "$BIN" diff "$WORKDIR/retry.xml" "$WORKDIR/shard-1.xml" > "$WORKDIR/diff.txt"; then
  fail "diff should exit 1 on a new failure"
fi
grep -q "new failures (1)" "$WORKDIR/diff.txt" || fail "diff bucket missing"
grep -q "diff: REGRESSIONS" "$WORKDIR/diff.txt" || fail "diff verdict missing"
DIFFJSON="$("$BIN" diff --format json "$WORKDIR/retry.xml" "$WORKDIR/shard-1.xml" || true)"
echo "$DIFFJSON" | grep -q '"regressions": true' || fail "diff json flag missing"

echo "8. filter extracts only the red cases as TAP"
FOUT="$("$BIN" filter --only-failed --to tap "$WORKDIR/merged.xml")"
echo "$FOUT" | grep -q "1..2" || fail "filter should keep exactly the 2 red cases"
echo "$FOUT" | grep -q "checkout flow" || fail "TAP failure missing from filter output"
if echo "$FOUT" | grep -q "login flow"; then
  fail "passing case leaked through --only-failed"
fi

echo "9. rewrite renames suites and strips times"
"$BIN" rewrite --rename-suite "web=frontend" --strip-times \
  -o "$WORKDIR/rewritten.xml" "$WORKDIR/merged.xml" || fail "rewrite failed"
grep -q 'name="frontend"' "$WORKDIR/rewritten.xml" || fail "suite rename missing"
grep -q 'time="0.410"' "$WORKDIR/rewritten.xml" && fail "times not stripped"

echo "10. stdin and json output compose in a pipe"
"$BIN" merge --to json - < "$WORKDIR/e2e.tap" \
  | grep -q '"schema_version": 1' || fail "stdin→json pipe broken"

echo "11. usage errors exit 2"
set +e
"$BIN" merge --dedupe newest "$WORKDIR/shard-1.xml" >/dev/null 2>&1
[ $? -eq 2 ] || fail "bad --dedupe should exit 2"
set -e

echo "SMOKE OK"
