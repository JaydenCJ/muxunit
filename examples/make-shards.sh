#!/usr/bin/env bash
# Fabricates the demo CI shards used by the README quickstart: two JUnit
# shards from a sharded unit-test run, one TAP stream from an e2e runner,
# and a retry shard where the flaky test passed. Usage:
#
#   bash examples/make-shards.sh /tmp/muxunit-demo
set -euo pipefail

DEST="${1:?usage: make-shards.sh <dest-dir>}"
mkdir -p "$DEST"

cat > "$DEST/shard-1.xml" <<'XML'
<testsuites><testsuite name="api">
  <testcase classname="Auth" name="creates a user" time="0.41"/>
  <testcase classname="Auth" name="rejects bad email" time="0.09">
    <failure message="expected 422, got 500">handler.go:71</failure>
  </testcase>
</testsuite></testsuites>
XML

cat > "$DEST/shard-2.xml" <<'XML'
<testsuites><testsuite name="api">
  <testcase classname="Billing" name="charges once" time="0.33"/>
</testsuite><testsuite name="web">
  <testcase name="renders home" time="0.12"/>
</testsuite></testsuites>
XML

cat > "$DEST/e2e.tap" <<'TAP'
TAP version 13
1..3
ok 1 - login flow
not ok 2 - checkout flow
  ---
  message: button not found
  ...
ok 3 - logout flow # SKIP flaky on headless
TAP

cat > "$DEST/retry.xml" <<'XML'
<testsuite name="api">
  <testcase classname="Auth" name="rejects bad email" time="0.08"/>
</testsuite>
XML

echo "wrote shard-1.xml shard-2.xml e2e.tap retry.xml to $DEST"
