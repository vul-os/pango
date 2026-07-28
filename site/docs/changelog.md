# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

> [!NOTE]
> PropFix is being **rebuilt from scratch**. There is no released, runnable
> software: no binary, no image, no UI. Entries below describe design and
> documentation work, and say so. A feature only appears under **Added** once it
> is implemented — a design document for it is documentation, not a feature.

## [Unreleased]

### Added

- `docs/ARCHITECTURE.md` — the binding contract: non-negotiables, stack, domain
  model, authority model, append-only money rule, sync design, WRAP binding,
  layering, migrations, security posture, and the status-honesty rule.
- Documentation set, all clearly marked as design specifications rather than
  descriptions of running software:
  - `docs/GETTING-STARTED.md` — intended path from clone to first job, with a
    per-step status table.
  - `docs/CONFIGURATION.md` — intended flags and `PROPFIX_*` environment
    variables, each marked with its status.
  - `docs/SYNC.md` — deep protocol specification: HLC stamps, merge rules,
    append-only money, authority, stateless symmetric rounds, envelope
    authentication and its threat table, folder/USB transport, the merge-engine
    seam, and open questions.
  - `docs/WRAP.md` — how PropFix maps onto the WRAP `trades/v0` profile for
    cross-organisation work, including what must never cross the boundary.
  - `docs/INSPECTIONS.md` — templates, append-only findings, and the
    ingoing/outgoing comparison, including the "not captured ingoing" case.
  - `docs/SELFHOST.md` — deployment shape, exposure, backup, and upgrades.
  - `docs/THREAT-MODEL.md` — assets, adversaries, what an attacker gets, and
    seven unmitigated residual risks.
  - `docs/SCREENSHOTS.md` — the shot list and screenshotter plan. No images
    exist and none are faked.
  - `docs/FAQ.md` — starting with "Can I use PropFix today?" ("No").
- `README.md` — with a status banner stating that the product is not usable
  today, and a feature table in which every row is marked *Designed*.
- `site/index.html` and `site/docs.html` — hand-written marketing site and docs
  viewer, zero external fetches (vendored `marked` and `mermaid`, no CDN, no
  web fonts), dark theme with a light mode via `prefers-color-scheme`.
- `scripts/sync-docs.mjs` + `npm run docs:sync` / `npm run docs:check` — copies
  `docs/*.md` into `site/docs/`, removes copies whose source is gone, and fails
  in `--check` mode when the two have diverged.
- `CONTRIBUTING.md` and `SECURITY.md`.

- `conformance/hlc_vectors.json` + `conformance/README.md` — the HLC stamp
  format, total order and tie-break rule pinned as data, so PropFix's HLC and
  FlowStock's near-identical copy of it can be held to one rule instead of two
  drifting sets of hand-written tests. Run by
  `backend/internal/store/hlc_vectors_test.go`.
- `make conformance` / `make conformance-status` — a gate that refuses to pass
  unless the WRAP vectors were actually found and run, and an informational
  target (also wired into `make test-go` and CI) that surfaces the harness's
  skip banner, which `go test` otherwise hides completely.

### Fixed

- **HLC stamps could invert their own causal order.** The counter field is four
  hex digits; nothing bounded it, and `Observe` folds a remote counter forward,
  so a peer sending `ffff` drove this node to `10000` — five characters, which
  sorts *before* every `?fff` stamp in the same millisecond. Nothing errored;
  the mesh just stopped agreeing. Both numeric fields are now bounded, `ParseHLC`
  rejects out-of-width stamps instead of adopting them, and minting spills into
  the next millisecond rather than widening the field. Pinned by the new
  conformance vectors; `docs/SYNC.md` §2 documents the rule.
- **The WRAP conformance harness skipped silently.** It loads its vectors from a
  sibling `vul-os/wrap` checkout, which is usually absent, and a skipped Go test
  prints nothing at all — so PropFix reported a green suite while verifying none
  of its WRAP claims. It now prints a banner naming every vector that went
  unverified, asserts that the vectors it claims to cover are present in the
  file and finished in the expected state, logs the file's SHA-256 for
  provenance (pinnable via `WRAP_VECTORS_SHA256`), and fails outright under
  `WRAP_VECTORS_REQUIRED=1`.
- `docs/SYNC.md` §2 claimed 13-digit stamps sort correctly "until the year
  33658". Thirteen digits run out in 2286.

### Changed

- `backend/internal/store/hlc.go`, `store/identity.go`, `sync/folder.go` and
  `sync/transport_auth.go` now say in their own headers that they are near-copies
  of FlowStock files, and point at `docs/SYNC.md` §11 ("Duplicated sync
  substrate"), which records the relationship file by file, why neither product
  may import the other, and which direction convergence runs in.

### Removed

- Dead code, all confirmed to have no caller in the repository:
  `wrap.asArray`, `sync.Engine.TestPeer` (whose comment claimed a UI action that
  does not exist), `store.Store.Tick` (callers use `s.clock.Tick()` directly),
  `repo.SameToken`, and `repo.Repo.PeerPubkey` (the sync transport queries the
  `peer` table directly and documents why).
- The hard-coded `/Users/pc/code/vulos/wrap/...` search path in the WRAP
  conformance harness — on the machine it was written for it resolved to the
  same file as the sibling-checkout path, and everywhere else it was noise.
- The legacy cloud-coupled implementation is being replaced rather than patched.
  Credentials found in the repository history are catalogued in
  `SECURITY-AUDIT.md`, including items still marked unresolved — rebuilding the
  code does not rotate a key.

### Not yet implemented

Listed explicitly so that the absence is a record rather than an omission: the
Go backend, the React frontend, migrations, the HLC oplog, peer sync, folder
transport, WRAP support, inspections, reporting, demo mode, the screenshotter,
and any released artefact.

[Unreleased]: https://github.com/vul-os/propfix/commits/main
