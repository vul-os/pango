#!/usr/bin/env bash
# Single verification gate. Every wave cycle / PR must end with this passing.
#
# Mirrors wede's scripts/check.sh. Every step is invoked unconditionally — if
# one is missing it fails loudly here rather than silently passing, which is
# the point of the gate.
#
# Status as of this commit: backend (gofmt/vet/build/test), the docs mirror
# check and the frontend test+build all pass; src/ has app code and `npm run
# build` succeeds. `npm run lint` FAILS on 7 pre-existing eslint errors in
# src/, so this script exits non-zero. That is a real failure to fix, not a
# step to silence.
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

# site/docs/ is a generated mirror of docs/ (scripts/sync-docs.mjs) that the
# docs viewer fetches at runtime. Nothing else re-runs the copy, so editing
# docs/ without re-running it publishes stale text with no signal — the exact
# drift the script's header describes. --check exits 1 on drift, 0 in sync.
step "docs: site mirror in sync"
npm run docs:check || fail=1

step "frontend: lint"
npm run lint || fail=1

step "frontend: test"
npm test || fail=1

step "frontend: build"
npm run build || fail=1

if [ "$fail" -ne 0 ]; then
  printf '\n\033[31mCHECK FAILED\033[0m\n'
  exit 1
fi
printf '\n\033[32mCHECK PASSED\033[0m\n'
