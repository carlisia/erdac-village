#!/usr/bin/env bash
# One exit code for the whole repository. Fixes nothing; reports everything.
#
# Two test tiers. The fast tier runs anywhere, with no database and no network.
# The live tier needs a running VillageSQL server and is skipped when
# TEST_MYSQL_DSN is unset -- which this script reports, so a green run never
# quietly means half of it did not run.
#
# That promise needs guarding rather than asserting. `go test` exits zero when
# a name pattern matches nothing, so a live tier that had been deleted, or
# renamed out of the pattern, would report success having executed nothing.
#
# The guard counts passing tests from `go test -json`, which is machine
# readable. An earlier version matched the human-readable summary with a
# regular expression and got it wrong: "[no tests to run]" and a real result
# differ only in text a pattern has to be careful about, and that one was not.
#
# No temporary files: each step's output is captured in a variable. That keeps
# the script free of any deletion and free of a predictable shared path.
set -uo pipefail
cd "$(dirname "$0")" || exit 1

fail=0
built=0

report_fail() {
  printf '  FAIL  %s\n' "$1"
  printf '%s\n' "$2" | sed 's/^/        /'
  fail=1
}

step() {
  local name="$1"; shift
  local out
  if out=$("$@" 2>&1); then
    printf '  ok    %s\n' "$name"
    return 0
  fi
  report_fail "$name" "$out"
  return 1
}

echo "erdac-village"

if step "build" go build ./...; then built=1; fi
step "vet"          go vet ./...
step "gofmt"        bash -c '[ -z "$(gofmt -l .)" ] || { gofmt -l .; false; }'

# Only meaningful once the build is known good: `go run` exits 1 for a compile
# failure and for a rejected configuration alike, so without this guard the
# label would blame the configuration for a broken build.
if [ "$built" -eq 1 ]; then
  step "config"     go run ./cmd/checkconfig siteconfig.toml
else
  printf '  SKIP  config - the build failed, so a failure here would be mislabelled\n'
fi

step "tests (fast)" go test -short -count=1 ./...

if [ -n "${TEST_MYSQL_DSN:-}" ]; then
  live=$(go test -run Live -count=1 -json ./... 2>&1)
  status=$?
  passed=$(printf '%s\n' "$live" | grep -c '"Action":"pass","Package":"[^"]*","Test":')
  if [ "$status" -ne 0 ]; then
    report_fail "tests (live)" "$live"
  elif [ "$passed" -eq 0 ]; then
    report_fail "tests (live) - no test matched the pattern, so nothing ran" "$live"
  else
    printf '  ok    tests (live) - %s tests\n' "$passed"
  fi
else
  printf '  SKIP  tests (live) - set TEST_MYSQL_DSN to run them\n'
fi

exit $fail
