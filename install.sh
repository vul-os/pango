#!/bin/sh
# =============================================================================
# install.sh — fetch, VERIFY and install a prebuilt propfix binary.
#
# POSIX sh, no bashisms, so it runs under dash/ash/sh as well as bash/zsh.
#
#   curl -fsSLO https://raw.githubusercontent.com/vul-os/propfix/main/install.sh
#   less install.sh          # review before running
#   sh install.sh
#
# THE CONTRACT: FAIL CLOSED
# -------------------------
# Every release publishes a `SHA256SUMS` manifest covering every attached
# asset. This script fetches that manifest, looks up the EXACT entry for the
# binary it is about to install, downloads the binary and compares digests.
# Nothing is written to your PATH, and nothing is made executable, until the
# digests match.
#
# There are exactly two outcomes: installed-and-verified (exit 0), or a
# non-zero exit with a diagnostic naming what was wrong. There is NO
# --skip-verify, NO "warn and continue", and -- the case that matters most --
# NO path where an ABSENT or UNREADABLE manifest means "nothing to check". A
# script that shrugs at a 404 on SHA256SUMS prints a line that looks like
# verification while checking nothing; that is strictly worse than not
# checking, because it converts "I don't know" into "it's fine".
#
# This replaces an earlier version of this file that downloaded the binary,
# chmod +x'd it and moved it onto your PATH having verified NOTHING, while the
# release workflow published a checksums file that consequently nobody read.
#
# FOUR DEFECTS THIS DELIBERATELY DOES NOT HAVE
# --------------------------------------------
#  1. NO FALL-OPEN. If the release version cannot be resolved, this script does
#     NOT guess a tag, fall back to "main", or install anything. It stops and
#     tells you to pin PROPFIX_VERSION yourself.
#  2. NO SILENT DEATH AT A PIPELINE. Under `set -e` a lookup whose grep matches
#     nothing can kill the script before the "not found" guard below it ever
#     runs. Every lookup here ends in `|| true` and its result is then
#     explicitly tested for emptiness. A guard has to be reachable to be a
#     guard.
#  3. NO `\n` INSIDE A `%s`. `die`/`say` take one argument per line and print
#     each with its own printf, message as an ARGUMENT and never as the format
#     string, so no filename or digest can render as a format or an escape.
#  4. NO SUBSTRING / REGEX NAME MATCH. The asset name is compared by awk
#     against FIELD 2 of the manifest, as a string. A substring grep treats the
#     name as a regex -- every "." in "propfix-windows-amd64.exe" is a wildcard
#     -- and would happily return the digest of "...exe.sig".
#
# EXIT CODES
# ----------
#   0   installed, digest verified against the published SHA256SUMS
#   2   usage error
#   3   SHA256SUMS could not be fetched / does not exist
#   4   SHA256SUMS was served but the body is an HTML page, not a manifest
#   5   SHA256SUMS is empty or contains no well-formed "<64 hex>  <name>" line
#   6   SHA256SUMS has no entry for the asset this platform needs
#   7   the binary could not be fetched (HTTP error, or HTML where bytes were
#       expected)
#   8   the download was truncated (origin closed before its Content-Length)
#   9   digest mismatch -- these are not the published bytes
#  10   a required local tool (curl, sha256sum/shasum) is missing
#  12   refusing a plaintext http:// origin (non-loopback)
#  13   the release version could not be resolved and none was pinned
#
# ENVIRONMENT
# -----------
#   PROPFIX_VERSION      pin an exact tag, e.g. v0.1.0 (skips the API lookup)
#   PROPFIX_INSTALL_DIR  where to install (default ~/.local/bin)
#   PROPFIX_REPO         owner/repo (default vul-os/propfix)
#   PROPFIX_BASE_URL     fetch assets from this base instead of GitHub
#                        Releases; must be https:// (or loopback, for tests)
#
#   sh install.sh --selftest    prove the refusals still fire (needs python3)
#
# Provenance is NOT checked here: the digest is checked against the manifest
# the release published, which is trusted as far as its TLS origin and no
# further. To also check the sigstore build provenance GitHub attached at
# release time, run the repo's `scripts/verify.sh --tag vX.Y.Z --attest ASSET`,
# or `gh attestation verify <file> --repo vul-os/propfix`. A pass here never
# implies more than it checked.
#
# NOTE: propfix has no tagged release yet (see CHANGELOG.md -- the rebuild is
# in progress) and release.yml publishes a DRAFT release, which the GitHub
# API's `releases/latest` does not report. Until a release is published, the
# version lookup below fails CLOSED with exit 13 rather than installing
# anything.
# =============================================================================
set -eu

REPO="${PROPFIX_REPO:-vul-os/propfix}"
BINARY="propfix"
MANIFEST="SHA256SUMS"
HTTP_TIMEOUT="${PROPFIX_HTTP_TIMEOUT:-120}"

E_USAGE=2
E_SUMS_FETCH=3
E_SUMS_HTML=4
E_SUMS_MALFORMED=5
E_NO_ENTRY=6
E_ART_FETCH=7
E_TRUNCATED=8
E_MISMATCH=9
E_NO_TOOL=10
E_INSECURE=12
E_NO_VERSION=13

SELF_NAME="install.sh"

# One argument per output line; the message is a printf ARGUMENT rendered with
# %s, never the format string and never %b. Defect 3 lived exactly here.
die() {
  _code="$1"; shift
  printf '%s: FATAL: %s\n' "$SELF_NAME" "$1" >&2
  shift
  for _line in "$@"; do printf '        %s\n' "$_line" >&2; done
  exit "$_code"
}
say() { printf '%s\n' "$*"; }

# -- Preflight: no tool, no verification (never a skip) -----------------------
command -v curl >/dev/null 2>&1 || die "$E_NO_TOOL" \
  "curl is required but is not installed." \
  "  Ubuntu/Debian: sudo apt install curl" \
  "  macOS:         brew install curl" \
  "  Fedora:        sudo dnf install curl"

if command -v sha256sum >/dev/null 2>&1; then
  sha256_of() { sha256sum "$1" 2>/dev/null | awk '{print $1}' | tr 'A-F' 'a-f' || true; }
elif command -v shasum >/dev/null 2>&1; then
  sha256_of() { shasum -a 256 "$1" 2>/dev/null | awk '{print $1}' | tr 'A-F' 'a-f' || true; }
else
  die "$E_NO_TOOL" \
    "neither sha256sum nor shasum is available -- no digest can be computed." \
    "Install GNU coreutils (Linux) or Perl's shasum (ships with macOS)." \
    "Refusing to install a binary whose digest this script cannot check."
fi

# digest_for <name> <manifest> -> the digest recorded for EXACTLY that name, or
# nothing at all. Empty means unverifiable, which means stop. awk compares
# field 2 as a string: "a.exe" cannot match "aXexe" and cannot match
# "a.exe.sig" (defect 4). The result must be exactly 64 hex chars, so an HTML
# body or a truncated line yields nothing rather than garbage to compare.
digest_for() {
  _found="$(awk -v w="$1" '$2 == w || $2 == "*" w { print $1; exit }' "$2" 2>/dev/null || true)"
  printf '%s' "$_found" | tr 'A-F' 'a-f' | grep -Ex '[0-9a-f]{64}' || true
}

looks_like_html() {
  case "${2:-}" in
    text/html*|application/xhtml*) return 0 ;;
  esac
  [ -s "$1" ] || return 1
  if head -c 512 "$1" 2>/dev/null | LC_ALL=C grep -qiE '<(!doctype|html|head|body)\b'; then
    return 0
  fi
  return 1
}

# Plaintext http to anywhere but loopback means the manifest and the binary
# both arrive over an unauthenticated channel, so comparing one against the
# other proves only that one attacker was self-consistent. Loopback is allowed
# so the selftest can stand up a synthetic origin.
require_secure_url() {
  case "$1" in
    https://*) return 0 ;;
    http://127.0.0.1:*|http://127.0.0.1/*|http://localhost:*|http://localhost/*) return 0 ;;
    *) die "$E_INSECURE" \
         "refusing to download over a plaintext, non-loopback origin:" \
         "  $1" \
         "Both the manifest and the binary would arrive unauthenticated, so" \
         "comparing them proves only that one attacker was self-consistent." \
         "Use an https:// URL." ;;
  esac
}

# fetch <url> <dest> -> curl's exit status; sets FETCH_CODE / FETCH_CTYPE /
# FETCH_SIZE / FETCH_ERR. Never aborts the caller: the caller decides.
FETCH_CODE=''; FETCH_CTYPE=''; FETCH_SIZE=''; FETCH_ERR=''
fetch() {
  _rc=0
  : > "${TMP_DIR}/curl.err"
  _out="$(curl --fail --silent --show-error --location \
               --max-time "$HTTP_TIMEOUT" \
               --write-out '%{http_code}|%{content_type}|%{size_download}' \
               --output "$2" "$1" 2>"${TMP_DIR}/curl.err")" || _rc=$?
  FETCH_CODE="${_out%%|*}"
  _rest="${_out#*|}"
  FETCH_CTYPE="${_rest%%|*}"
  FETCH_SIZE="${_rest##*|}"
  FETCH_ERR="$(tr -d '\r' < "${TMP_DIR}/curl.err" | head -3 | tr '\n' ' ' || true)"
  return "$_rc"
}

# =============================================================================
# The installer
# =============================================================================
run_install() {
  say "Installing propfix..."
  say ""

  # -- Platform -------------------------------------------------------------
  OS="$(uname -s)"
  case "$OS" in
    Linux*)  OS="linux";  INSTALL_DIR="${PROPFIX_INSTALL_DIR:-${HOME}/.local/bin}" ;;
    Darwin*) OS="darwin"; INSTALL_DIR="${PROPFIX_INSTALL_DIR:-${HOME}/.local/bin}" ;;
    MINGW*|MSYS*|CYGWIN*)
      OS="windows"
      INSTALL_DIR="${PROPFIX_INSTALL_DIR:-${LOCALAPPDATA:-$HOME/AppData/Local}/propfix}" ;;
    *) die "$E_USAGE" "unsupported operating system: $OS" ;;
  esac

  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64|amd64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) die "$E_USAGE" "unsupported architecture: $ARCH" ;;
  esac

  ASSET="${BINARY}-${OS}-${ARCH}"
  if [ "$OS" = "windows" ]; then ASSET="${ASSET}.exe"; fi

  say "  OS:   $OS"
  say "  Arch: $ARCH"

  # -- Where the assets come from -------------------------------------------
  if [ -n "${PROPFIX_BASE_URL:-}" ]; then
    BASE_URL="${PROPFIX_BASE_URL%/}"
    say "  Source:  $BASE_URL"
  else
    VERSION="${PROPFIX_VERSION:-}"
    if [ -z "$VERSION" ]; then
      # NO FALL-OPEN. If this lookup fails for any reason -- offline, rate
      # limit, DNS, no published release, a draft release the API does not
      # report -- the script stops. It does NOT fall back to a branch, to
      # "latest" as a literal path segment, or to a cached tag. Installing
      # bytes from a release nobody named is unverifiable no matter what any
      # digest says.
      #
      # `|| true` on the lookup, then an explicit emptiness test: under
      # `set -e` a grep that matches nothing must not kill the script before
      # the guard below it runs (defect 2).
      _api="${TMP_DIR}/api.json"
      _api_rc=0
      require_secure_url "$API_BASE"
      fetch "${API_BASE}/${REPO}/releases/latest" "$_api" || _api_rc=$?
      VERSION=""
      if [ "$_api_rc" -eq 0 ]; then
        VERSION="$(grep '"tag_name"' "$_api" 2>/dev/null | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/' | head -1 || true)"
      fi
      if [ -z "$VERSION" ]; then
        die "$E_NO_VERSION" \
          "could not determine the latest published release of ${REPO}." \
          "HTTP status ${FETCH_CODE:-none}; curl exit ${_api_rc}: ${FETCH_ERR:-no detail}" \
          "REFUSING to continue. This script will not guess a tag, fall back to a" \
          "branch, or install an unpinned build." \
          "Either the repository has no published release yet (release.yml publishes" \
          "a DRAFT, which the API's releases/latest does not report), or this machine" \
          "cannot reach the release API." \
          "Check https://github.com/${REPO}/releases and pin one:" \
          "  PROPFIX_VERSION=vX.Y.Z sh install.sh"
      fi
    fi
    case "$VERSION" in
      v[0-9]*) : ;;
      *) die "$E_USAGE" \
           "PROPFIX_VERSION must name a release tag (vX.Y.Z); got: ${VERSION}" \
           "A branch name is not a release: it has no published manifest and its" \
           "bytes change under you. Pin a tag from" \
           "  https://github.com/${REPO}/releases" ;;
    esac
    say "  Version: $VERSION"
    BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
  fi
  require_secure_url "$BASE_URL"
  say ""

  # -- 1. The manifest, or nothing ------------------------------------------
  SUMS="${TMP_DIR}/${MANIFEST}"
  say "  Fetching ${BASE_URL}/${MANIFEST}"
  rc=0
  fetch "${BASE_URL}/${MANIFEST}" "$SUMS" || rc=$?
  if [ "$rc" -ne 0 ]; then
    die "$E_SUMS_FETCH" \
      "could not fetch ${MANIFEST} from:" \
      "  ${BASE_URL}/${MANIFEST}" \
      "HTTP status ${FETCH_CODE:-none}; curl exit ${rc}: ${FETCH_ERR:-no detail}" \
      "REFUSING to install. Without the published manifest there is no digest to" \
      "compare against, so the binary would be UNVERIFIED -- which is not the same" \
      "as verified-and-fine. Nothing has been downloaded or written."
  fi
  if looks_like_html "$SUMS" "$FETCH_CTYPE"; then
    die "$E_SUMS_HTML" \
      "the origin returned an HTML page where ${MANIFEST} was expected." \
      "  ${BASE_URL}/${MANIFEST}" \
      "HTTP ${FETCH_CODE}, content-type ${FETCH_CTYPE:-unknown}, ${FETCH_SIZE:-0} bytes." \
      "That is a captive portal, a login wall, or a CDN answering 200 with an" \
      "error page. It is not a manifest, and parsing it would find no digests" \
      "while looking like it tried."
  fi
  if [ ! -s "$SUMS" ]; then
    die "$E_SUMS_MALFORMED" \
      "${MANIFEST} is empty (0 bytes)." \
      "  source: ${BASE_URL}/${MANIFEST}" \
      "An empty manifest vouches for nothing while looking like a manifest." \
      "Treating it as 'nothing to check' is the exact bug this refuses."
  fi
  valid_lines="$(awk '$1 ~ /^[0-9A-Fa-f]{64}$/ && NF >= 2 { n++ } END { print n+0 }' "$SUMS" 2>/dev/null || true)"
  if [ "${valid_lines:-0}" -eq 0 ]; then
    die "$E_SUMS_MALFORMED" \
      "${MANIFEST} contains no well-formed checksum line." \
      "  source: ${BASE_URL}/${MANIFEST}" \
      "Expected lines of the form '<64 hex digits>  <filename>'; found none." \
      "The file was truncated, is in a foreign format, or is an error body that" \
      "did not announce itself as HTML."
  fi

  # -- 2. The exact entry for THIS platform's asset --------------------------
  EXPECTED="$(digest_for "$ASSET" "$SUMS")"
  if [ -z "$EXPECTED" ]; then
    die "$E_NO_ENTRY" \
      "${MANIFEST} has no entry for '${ASSET}'." \
      "  source: ${BASE_URL}/${MANIFEST}" \
      "The manifest was fetched and parsed (${valid_lines} valid entries), but none" \
      "of them names this platform's asset with a 64-hex digest. Names are matched" \
      "EXACTLY -- '${ASSET}' does not match '${ASSET}.sig', and a '.' in the name is" \
      "a literal dot, not a wildcard. Either this release publishes no build for" \
      "${OS}/${ARCH}, or it attached an asset the manifest does not vouch for." \
      "Both are refusals."
  fi

  # -- 3. The bytes ----------------------------------------------------------
  TMP_FILE="${TMP_DIR}/${ASSET}"
  say "  Fetching ${BASE_URL}/${ASSET}"
  rc=0
  fetch "${BASE_URL}/${ASSET}" "$TMP_FILE" || rc=$?
  if [ "$rc" -eq 18 ]; then
    rm -f "$TMP_FILE"
    die "$E_TRUNCATED" \
      "the download of '${ASSET}' was TRUNCATED." \
      "  ${BASE_URL}/${ASSET}" \
      "The origin declared a Content-Length and then closed the connection early;" \
      "${FETCH_SIZE:-0} bytes arrived (curl exit 18). The partial file is deleted." \
      "Reported separately from a digest mismatch on purpose: a short read is a" \
      "transport failure to retry, not evidence of tampering."
  fi
  if [ "$rc" -ne 0 ]; then
    rm -f "$TMP_FILE"
    die "$E_ART_FETCH" \
      "could not fetch '${ASSET}':" \
      "  ${BASE_URL}/${ASSET}" \
      "HTTP status ${FETCH_CODE:-none}; curl exit ${rc}: ${FETCH_ERR:-no detail}" \
      "The manifest vouches for this asset but the origin did not serve it."
  fi

  ACTUAL="$(sha256_of "$TMP_FILE")"
  if [ -z "$ACTUAL" ]; then
    rm -f "$TMP_FILE"
    die "$E_MISMATCH" \
      "could not compute a SHA-256 digest for the download." \
      "The file is unreadable or the digest tool produced nothing." \
      "Refusing to install a binary whose digest was never computed."
  fi

  if [ "$ACTUAL" != "$EXPECTED" ]; then
    # An HTML body served with a non-HTML content-type surfaces here. Say which
    # it is: "you were served an error page" and "these bytes were tampered
    # with" are different problems with different fixes.
    if looks_like_html "$TMP_FILE" "$FETCH_CTYPE"; then
      rm -f "$TMP_FILE"
      die "$E_ART_FETCH" \
        "the origin returned an HTML page where '${ASSET}' bytes were expected." \
        "  ${BASE_URL}/${ASSET}" \
        "HTTP ${FETCH_CODE}, content-type ${FETCH_CTYPE:-unknown}, ${FETCH_SIZE:-0} bytes." \
        "A CDN error page or a login wall answered 200. Nothing was installed."
    fi
    rm -f "$TMP_FILE"
    die "$E_MISMATCH" \
      "SHA-256 MISMATCH for '${ASSET}' -- these are not the published bytes." \
      "  expected: ${EXPECTED}" \
      "  actual:   ${ACTUAL}" \
      "  source:   ${BASE_URL}/${MANIFEST}" \
      "NOTHING has been installed and the download has been deleted. Either the" \
      "transfer corrupted it or the artifact was substituted."
  fi

  say "  Verified: ${ACTUAL}"

  # -- 4. Only now does anything touch the filesystem ------------------------
  chmod +x "$TMP_FILE"
  mkdir -p "$INSTALL_DIR"
  DEST="${INSTALL_DIR}/${BINARY}"
  if [ "$OS" = "windows" ]; then DEST="${DEST}.exe"; fi
  mv "$TMP_FILE" "$DEST"
  say "  Installed to ${DEST}"

  case ":$PATH:" in
    *":${INSTALL_DIR}:"*) ;;
    *)
      say ""
      say "  Warning: ${INSTALL_DIR} is not in your PATH."
      say "  Run this to add it:"
      say ""
      case "$OS" in
        darwin)  say "    echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.zshrc && source ~/.zshrc" ;;
        linux)   say "    echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.bashrc && source ~/.bashrc" ;;
        windows) say "    setx PATH \"%PATH%;${INSTALL_DIR}\"" ;;
      esac
      ;;
  esac

  say ""
  say "  Done -- the digest matched the release's ${MANIFEST}."
  say "  Build provenance was NOT checked. To check the signature too:"
  say "    gh attestation verify \"${DEST}\" --repo ${REPO}"
  say ""
  say "  Quick start:"
  say "    propfix --demo"
  say "    open http://localhost:8099"
  say ""
}

# =============================================================================
# Selftest -- a synthetic origin that is wrong in exactly one way per route
# =============================================================================
# These guards are only worth having if their refusals are exercised. This is
# the same shape as scripts/verify.sh --selftest and asserts the exit code AND
# that a diagnostic was printed: "aborted with no message" was the real bug in
# the sibling installer this file was rewritten so as not to repeat.
write_origin_server() {
  cat > "$1" <<'PYEOF'
import hashlib, sys, threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

ASSET = sys.argv[1]
GOOD  = b"propfix-binary-bytes\n" * 64
OTHER = b"tampered-substituted-bytes!!!\n" * 64
HTML  = (b"<!DOCTYPE html><html><head><title>404 Not Found</title></head>"
         b"<body><h1>Not Found</h1></body></html>")

def line(name, data):
    return "%s  %s\n" % (hashlib.sha256(data).hexdigest(), name)

ROUTES = {}
def add(case, path, status, ctype, body, declared=None):
    ROUTES["/%s/%s" % (case, path)] = (status, ctype, body, declared)

good = line(ASSET, GOOD).encode()

# everything correct
add("good", "SHA256SUMS", 200, "text/plain", good)
add("good", ASSET,        200, "application/octet-stream", GOOD)

# manifest 404 (the binary is served, so only the manifest is wrong)
add("nosums", ASSET, 200, "application/octet-stream", GOOD)

# manifest served as an HTML error page with HTTP 200
add("htmlsums", "SHA256SUMS", 200, "text/html", HTML)
add("htmlsums", ASSET,        200, "application/octet-stream", GOOD)

add("emptysums", "SHA256SUMS", 200, "text/plain", b"")
add("emptysums", ASSET,        200, "application/octet-stream", GOOD)

add("junksums", "SHA256SUMS", 200, "text/plain", b"Not Found\nerror: no such release\n")
add("junksums", ASSET,        200, "application/octet-stream", GOOD)

add("noentry", "SHA256SUMS", 200, "text/plain", line("some-other-asset", GOOD).encode())
add("noentry", ASSET,        200, "application/octet-stream", GOOD)

# The manifest vouches ONLY for "<ASSET>.sig" while the origin serves those
# same bytes as <ASSET>. Exact matching must refuse; a substring grep would
# find the .sig line, compare against ITS digest, and report success on a
# binary nobody ever vouched for.
add("sigswap", "SHA256SUMS", 200, "text/plain", line(ASSET + ".sig", OTHER).encode())
add("sigswap", ASSET,        200, "application/octet-stream", OTHER)

add("noart", "SHA256SUMS", 200, "text/plain", good)

add("htmlart", "SHA256SUMS", 200, "text/plain", good)
add("htmlart", ASSET,        200, "text/html", HTML)

# declares more bytes than it sends, then hangs up: curl sees a short read
add("truncart", "SHA256SUMS", 200, "text/plain", good)
add("truncart", ASSET,        200, "application/octet-stream", GOOD[:100], 50000)

add("mismatch", "SHA256SUMS", 200, "text/plain", good)
add("mismatch", ASSET,        200, "application/octet-stream", OTHER)

class H(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def log_message(self, *a):
        pass
    def do_GET(self):
        r = ROUTES.get(self.path)
        if r is None:
            self.send_response(404)
            self.send_header("Content-Type", "text/html")
            self.send_header("Content-Length", str(len(HTML)))
            self.end_headers()
            self.wfile.write(HTML)
            return
        status, ctype, body, declared = r
        self.send_response(status)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body) if declared is None else declared))
        self.end_headers()
        try:
            self.wfile.write(body)
            self.wfile.flush()
        except Exception:
            pass
        if declared is not None:
            self.close_connection = True

srv = ThreadingHTTPServer(("127.0.0.1", 0), H)
print("PORT %d" % srv.server_address[1], flush=True)
threading.Thread(target=srv.serve_forever, daemon=True).start()
try:
    threading.Event().wait()
except KeyboardInterrupt:
    pass
PYEOF
}

SELFTEST_FAILURES=0
SELFTEST_CASES=0

# expect_exit <label> <want-code> <want-diagnostic yes|no> -- <cmd...>
expect_exit() {
  _label="$1"; _want="$2"; _want_diag="$3"; shift 3
  if [ "${1:-}" = "--" ]; then shift; fi
  _outf="${TMP_DIR}/case.out"
  _rc2=0
  : > "$_outf"
  "$@" > "$_outf" 2>&1 || _rc2=$?
  _diag="$(grep -m1 'FATAL:' "$_outf" | sed 's/.*FATAL: *//' || true)"
  [ -n "$_diag" ] || _diag="$(head -1 "$_outf" || true)"
  [ -n "$_diag" ] || _diag="(NO DIAGNOSTIC PRINTED)"
  _verdict="ok"
  if [ "$_rc2" -ne "$_want" ]; then
    _verdict="FAIL(exit ${_rc2}, want ${_want})"
    SELFTEST_FAILURES=$((SELFTEST_FAILURES + 1))
  elif [ "$_want_diag" = "yes" ] && [ "$_diag" = "(NO DIAGNOSTIC PRINTED)" ]; then
    # The failure this whole file is about: a guard that aborts with no message
    # tells the user nothing and reads like a crash rather than a refusal.
    _verdict="FAIL(silent)"
    SELFTEST_FAILURES=$((SELFTEST_FAILURES + 1))
  fi
  SELFTEST_CASES=$((SELFTEST_CASES + 1))
  printf '  %-32s exit %-3s %-22s %s\n' "$_label" "$_rc2" "$_verdict" "$_diag"
}

run_selftest() {
  command -v python3 >/dev/null 2>&1 || die "$E_NO_TOOL" \
    "python3 is required to run the synthetic-origin selftest." \
    "(Test-only: a real install needs curl and sha256sum/shasum, nothing else.)"
  [ -f "$SELF_PATH" ] || die "$E_USAGE" \
    "--selftest re-executes this script and so needs it on disk." \
    "Download it first:  curl -fsSLO .../install.sh && sh install.sh --selftest"

  # Use the asset name THIS machine's uname would ask for, so the selftest
  # exercises the very lookup a real run performs rather than a name it made up.
  _os="$(uname -s)"; _arch="$(uname -m)"
  case "$_os" in
    Linux*) _os=linux ;;
    Darwin*) _os=darwin ;;
    MINGW*|MSYS*|CYGWIN*) _os=windows ;;
    *) die "$E_USAGE" "selftest: unsupported OS $_os" ;;
  esac
  case "$_arch" in
    x86_64|amd64) _arch=amd64 ;;
    aarch64|arm64) _arch=arm64 ;;
    *) die "$E_USAGE" "selftest: unsupported arch $_arch" ;;
  esac
  _asset="${BINARY}-${_os}-${_arch}"
  if [ "$_os" = "windows" ]; then _asset="${_asset}.exe"; fi

  _py="${TMP_DIR}/origin.py"; _log="${TMP_DIR}/origin.log"
  write_origin_server "$_py"
  python3 "$_py" "$_asset" > "$_log" 2>&1 &
  _srv=$!
  trap 'kill '"$_srv"' 2>/dev/null || true; rm -rf "$TMP_DIR"' EXIT INT TERM

  _port=""
  for _ in $(seq 1 100); do
    _port="$(awk '/^PORT /{print $2; exit}' "$_log" 2>/dev/null || true)"
    if [ -n "$_port" ]; then break; fi
    sleep 0.1
  done
  [ -n "$_port" ] || die 1 "synthetic origin failed to start"

  _base="http://127.0.0.1:${_port}"
  _bin="${TMP_DIR}/bin"

  printf '\n%s selftest -- synthetic origin on %s (asset %s)\n\n' "$SELF_NAME" "$_base" "$_asset"
  printf '  %-32s %-8s %-22s %s\n' "CASE" "EXIT" "VERDICT" "DIAGNOSTIC (first line)"
  printf '  %s\n' "----------------------------------------------------------------------------"

  run() {
    env PROPFIX_BASE_URL="$1" PROPFIX_INSTALL_DIR="$_bin" PROPFIX_REPO="$REPO" \
        sh "$SELF_PATH"
  }

  expect_exit "happy path (installs)"      0                   no  -- run "$_base/good"
  expect_exit "SHA256SUMS 404"             "$E_SUMS_FETCH"     yes -- run "$_base/nosums"
  expect_exit "SHA256SUMS is HTML"         "$E_SUMS_HTML"      yes -- run "$_base/htmlsums"
  expect_exit "SHA256SUMS empty"           "$E_SUMS_MALFORMED" yes -- run "$_base/emptysums"
  expect_exit "SHA256SUMS has no digests"  "$E_SUMS_MALFORMED" yes -- run "$_base/junksums"
  expect_exit "no entry for this platform" "$E_NO_ENTRY"       yes -- run "$_base/noentry"
  expect_exit "no entry (.sig false-pass)" "$E_NO_ENTRY"       yes -- run "$_base/sigswap"
  expect_exit "binary 404"                 "$E_ART_FETCH"      yes -- run "$_base/noart"
  expect_exit "binary is an HTML page"     "$E_ART_FETCH"      yes -- run "$_base/htmlart"
  expect_exit "binary truncated"           "$E_TRUNCATED"      yes -- run "$_base/truncart"
  expect_exit "digest mismatch"            "$E_MISMATCH"       yes -- run "$_base/mismatch"
  expect_exit "plaintext http origin"      "$E_INSECURE"       yes -- run "http://example.com/rel"
  # NO FALL-OPEN: an unreachable release API must stop, not guess a tag. Port 1
  # on loopback (nothing listens) stands in for "offline".
  expect_exit "release API unreachable"    "$E_NO_VERSION"     yes -- \
    env PROPFIX_INSTALL_DIR="$_bin" PROPFIX_REPO="$REPO" PROPFIX_HTTP_TIMEOUT=5 \
        sh "$SELF_PATH" --api-base "http://127.0.0.1:1/repos"
  expect_exit "a branch is not a release"  "$E_USAGE"          yes -- \
    env PROPFIX_VERSION="main" PROPFIX_INSTALL_DIR="$_bin" sh "$SELF_PATH"
  expect_exit "unknown flag"               "$E_USAGE"          yes -- sh "$SELF_PATH" --skip-verify

  # The happy path must have produced a real, executable file. A selftest whose
  # "pass" case installs nothing would report a working installer that never
  # installs.
  if [ ! -x "${_bin}/${BINARY}" ]; then
    printf '  %-32s %s\n' "happy path left an executable" "FAIL(nothing at ${_bin}/${BINARY})"
    SELFTEST_FAILURES=$((SELFTEST_FAILURES + 1))
  else
    printf '  %-32s %s\n' "happy path left an executable" "ok"
  fi
  SELFTEST_CASES=$((SELFTEST_CASES + 1))

  printf '\n'
  if [ "$SELFTEST_FAILURES" -ne 0 ]; then
    die 1 "selftest: ${SELFTEST_FAILURES} case(s) did not behave as specified."
  fi
  printf '%s: selftest passed -- %d cases, every failure mode refused with its own exit code and its own diagnostic.\n' \
    "$SELF_NAME" "$SELFTEST_CASES"
}

# -- Entry --------------------------------------------------------------------
SELF_PATH="$0"
MODE="install"
API_BASE="https://api.github.com/repos"
while [ $# -gt 0 ]; do
  case "$1" in
    --selftest) MODE="selftest"; shift ;;
    # Test-only: where the version lookup asks. Still subject to
    # require_secure_url, so it cannot be used to downgrade the lookup to a
    # plaintext non-loopback origin.
    --api-base) API_BASE="${2:-}"; shift 2 ;;
    -h|--help)
      sed -n '2,/^# =\{10,\}$/p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) die "$E_USAGE" "unknown option: $1" \
         "There is no --skip-verify and no --no-check: this script has exactly two" \
         "outcomes, verified-and-installed or a non-zero exit." \
         "Run with --help for usage." ;;
  esac
done

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/propfix-install.XXXXXX")"
trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

if [ "$MODE" = "selftest" ]; then
  run_selftest
else
  run_install
fi
