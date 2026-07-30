#!/usr/bin/env sh
# Build the single-binary release: the built app and the marketing site are
# copied next to their embed directives and compiled in (build tag
# `embed_frontend`).
#
# The copies are needed because //go:embed cannot reach outside its own package
# directory, while dist/ and site/ live at the repo root where the frontend and
# site tooling expect them. A plain `go build ./...` skips all of this and uses
# app_dev.go / site_dev.go, which serve from disk — so developers and CI never
# need to run this.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
site_staging="$root/backend/cmd/pango/site"
app_staging="$root/backend/cmd/pango/dist"

if [ ! -d "$root/site" ]; then
  echo "build-embedded: no site/ at the repo root — nothing to embed" >&2
  exit 1
fi

# The app is what makes this a product rather than an API with a landing page,
# so a missing build is a hard error here rather than a silent degradation.
if [ ! -f "$root/dist/index.html" ]; then
  echo "build-embedded: no dist/index.html — run 'npm run build' first" >&2
  exit 1
fi

cleanup() {
  # The staging copies are build input, not source. Leaving them behind would
  # make `git status` dirty and tempt somebody to commit a second copy of the
  # app. Run on exit so a failed compile does not strand them either.
  rm -rf "$site_staging" "$app_staging"
}
trap cleanup EXIT INT TERM

rm -rf "$site_staging" "$app_staging"
mkdir -p "$site_staging" "$app_staging"
cp -R "$root/site/." "$site_staging/"
cp -R "$root/dist/." "$app_staging/"

cd "$root/backend"
go build -tags embed_frontend -o "$root/backend/pango" ./cmd/pango

echo "build-embedded: wrote $root/backend/pango"
