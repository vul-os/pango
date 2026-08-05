#!/usr/bin/env bash
# Single verification gate. Every wave cycle / PR must end with this passing.
#
# Mirrors wede's scripts/check.sh. Every step is invoked unconditionally — if
# one is missing it fails loudly here rather than silently passing, which is
# the point of the gate.
#
# Status: the `frontend: lint` step is green, and with it this script exits 0 —
# verified end to end against a clean checkout of HEAD plus the eslint fixes
# (all ten steps, exit 0).
#
# It previously exited non-zero on 7 eslint errors in src/, left failing on
# purpose rather than silenced. Those are fixed at the source — see
# eslint.config.js (`no-unused-vars` now exempts capitalised *parameters* too,
# because core ESLint cannot see JSX references and this config carries no
# eslint-plugin-react), src/lib/useAsync.js (a caller-supplied `deps` array can
# never be a hook dependency list, so it is serialised to a scalar and
# `loading` is derived rather than set from an effect), src/lib/auth.jsx +
# src/lib/auth-context.js, src/lib/useUnitsIndex.js and
# src/components/PhotoPicker.jsx. Nothing is `eslint-disable`d.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

fail=0
step() { printf '\n\033[1m== %s ==\033[0m\n' "$1"; }

step "backend: gofmt"
unformatted="$(cd backend && gofmt -l .)"
if [ -n "$unformatted" ]; then
  echo "$unformatted"
  fail=1
fi

step "backend: go vet"
( cd backend && go vet ./... ) || fail=1

step "backend: go build"
( cd backend && go build ./... ) || fail=1

step "backend: go test"
( cd backend && go test ./... ) || fail=1

# Conformance visibility, not a gate.
#
# Two harnesses load their vectors from a checkout this repository does not
# contain — internal/wrap from vul-os/wrap, internal/sync/substrate from
# vul-os/kotva — and skip when it is absent. `go test` without -v prints NOTHING
# for a skipped test, so the step above goes green while saying nothing about
# whether either claim was checked. These re-run just those two verbosely so
# their banners land in the place people actually look.
#
# Neither fails this script. `make conformance` and
# `make sync-conformance KOTVA_DIR=...` are the gates that do, and they are what
# a conformance claim has to be made behind.
step "conformance: what was NOT verified"
make --no-print-directory conformance-status || true
make --no-print-directory sync-conformance-status || true

# site/docs/ is a generated mirror of docs/ (scripts/sync-docs.mjs) that the
# docs viewer fetches at runtime. Nothing else re-runs the copy, so editing
# docs/ without re-running it publishes stale text with no signal — the exact
# drift the script's header describes. --check exits 1 on drift, 0 in sync.
#
# The frontend project (package.json and everything npm) lives under web/.
step "docs: site mirror in sync"
( cd web && npm run docs:check ) || fail=1

step "frontend: lint"
( cd web && npm run lint ) || fail=1

step "frontend: test"
( cd web && npm test ) || fail=1

step "frontend: build"
( cd web && npm run build ) || fail=1

# The release verifier and the installer both refuse to install unverified
# bytes. Those refusals are the whole product; run the synthetic-origin matrix
# here so a guard that has quietly stopped failing shows up as a red gate
# rather than as a green run that checked nothing.
step "release: verify.sh failure matrix"
bash scripts/verify.sh --selftest || fail=1

step "release: install.sh failure matrix"
sh install.sh --selftest || fail=1

if [ "$fail" -ne 0 ]; then
  printf '\n\033[31mCHECK FAILED\033[0m\n'
  exit 1
fi
printf '\n\033[32mCHECK PASSED\033[0m\n'
