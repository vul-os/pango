# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

> [!NOTE]
> Pango is being **rebuilt from scratch**, and nothing has been **released**:
> there is no tag, no published binary and no published image. The tree does
> build and run — `npm run build:all` produces a single binary with the frontend
> embedded, and `pango --demo` serves a seeded in-memory instance — so "not
> released" is the claim here, not "not runnable". A feature only appears under
> **Added** once it is implemented; a design document for it is documentation,
> not a feature.

## [Unreleased]

### Added

- **The shared merge engine, in the seam that was built for it.**
  `backend/internal/sync/substrate` implements `store.Merger` over
  `github.com/vul-os/kotva/bindings/go@v0.2.1` — the same compiled algebra the
  rest of the suite runs, executed through wazero, so it is pure Go and
  `CGO_ENABLED=0` cross-compilation is unaffected (verified for linux
  amd64/arm64/arm, darwin amd64/arm64, windows amd64, freebsd amd64). Select it
  with `--merge-engine=substrate` (or `PANGO_MERGER`); an unrecognised value is
  fatal rather than defaulted. Default is still `builtin`.
  - It replaces the **algebra**, not the clock: `store.Journal` still mints the
    stamp, because in Pango a stamp is also the oplog's primary key and every
    replicated row's `hlc` column. `store/hlc.go`, `store/identity.go`,
    `sync/folder.go` and `sync/transport_auth.go` are all deliberately kept —
    `docs/SYNC.md` §11 now records the reason per file rather than as a blanket
    "the crate does not exist".
  - **Size cost, measured on this repository, not quoted from a sibling:**
    linux/amd64 grew 17,000,275 → 20,832,783 bytes, **+3,832,508 bytes
    (+3.65 MiB, +22.5%)**. Only 427,731 bytes of that is the algebra module; the
    remaining ~89% is the wazero runtime, so the increment is paid once and is
    nearly flat as more of the algebra is used.
  - No migration was required. The `oplog.cose` column and the `store.Merger`
    interface already existed, and Pango's op identity is its HLC stamp — not a
    UUID — so it does not disagree with the engine's content address.
- `backend/internal/sync/classes.go` — which replicated table is a
  last-writer-wins register and which is add-only, as data, in one place read by
  both engines. `classes_test.go` fails if a migration adds a replicated table
  nobody classified, which would otherwise silently inherit last-writer-wins and
  lose concurrent append-only entries.
- A **forward drift bound** on the HLC (`store.DefaultMaxDrift`, 5 minutes).
  `Observe` previously folded any in-width remote stamp, so one stamp dated 2085
  would out-order every honest write on a node permanently, with no error and no
  recovery short of editing the database. Seeding from the node's own journal
  deliberately bypasses the bound; folding a peer's stamp does not. The bound sits
  *above* the substrate engine because that engine's `Clock.Observe` takes no
  receiver reading and structurally cannot check one.
- `docs/ARCHITECTURE.md` — the binding contract: non-negotiables, stack, domain
  model, authority model, append-only money rule, sync design, WRAP binding,
  layering, migrations, security posture, and the status-honesty rule.
- Documentation set, all clearly marked as design specifications rather than
  descriptions of running software:
  - `docs/GETTING-STARTED.md` — intended path from clone to first job, with a
    per-step status table.
  - `docs/CONFIGURATION.md` — intended flags and `PANGO_*` environment
    variables, each marked with its status.
  - `docs/SYNC.md` — deep protocol specification: HLC stamps, merge rules,
    append-only money, authority, stateless symmetric rounds, envelope
    authentication and its threat table, folder/USB transport, the merge-engine
    seam, and open questions.
  - `docs/WRAP.md` — how Pango maps onto the WRAP `trades/v0` profile for
    cross-organisation work, including what must never cross the boundary.
  - `docs/INSPECTIONS.md` — templates, append-only findings, and the
    ingoing/outgoing comparison, including the "not captured ingoing" case.
  - `docs/SELFHOST.md` — deployment shape, exposure, backup, and upgrades.
  - `docs/THREAT-MODEL.md` — assets, adversaries, what an attacker gets, and
    seven unmitigated residual risks.
  - `docs/SCREENSHOTS.md` — the shot list and screenshotter plan. No images
    exist and none are faked.
  - `docs/FAQ.md` — starting with "Can I use Pango today?" ("No").
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
  format, total order and tie-break rule pinned as data, so Pango's HLC and
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
  prints nothing at all — so Pango reported a green suite while verifying none
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

Listed explicitly so that the absence is a record rather than an omission:

- **A released artefact.** No tag has ever been cut (`git tag` is empty) and no
  build output is committed, so there is nothing to download and nothing to
  install. `.github/workflows/release.yml`, `install.sh` and `scripts/verify.sh`
  exist and their failure matrices are exercised by `scripts/check.sh`, but they
  have never run against a real release.
- **A published container image.** There is a `Dockerfile`; it is not pushed
  anywhere.

Everything else this list used to claim was missing has since been built, and
the claim was left standing after it stopped being true. For the record, with
where to look:

| Was listed absent | Actually |
| --- | --- |
| The Go backend | `backend/` — 68 Go files across `api`, `domain`, `inspect`, `repo`, `report`, `store`, `sync`, `wrap`; `go build ./...` and `go test ./...` both pass |
| The React frontend | `src/` — 47 modules, five routed pages, `npm run build` succeeds, 52 vitest tests pass |
| Migrations | `backend/internal/store/migrations/` — `1_core`, `100_property`, `200_maintenance`, `300_inspections`, applied by `store/migrate.go` |
| The HLC oplog | `backend/internal/store/hlc.go` + the `oplog` table in `1_core.sql`; vectors in `store/hlc_vectors_test.go` |
| Peer sync | `backend/internal/sync/sync.go`, `apply.go`, `ops.go`, with `transport_auth.go` for peer authentication |
| Folder transport | `backend/internal/sync/folder.go` (+ `folder_test.go`) |
| WRAP support | `backend/internal/wrap/` — `cbor.go`, `kinds.go`, `object.go`, `pango.go`, plus the `conformance/` vector harness |
| Inspections | `backend/internal/inspect/compare.go` and `src/pages/InspectionsPage.jsx` / `InspectionDetailPage.jsx`, including the ingoing/outgoing comparison endpoint |
| Reporting | `backend/internal/report/report.go` and `src/pages/ReportsPage.jsx` |
| Demo mode | `backend/cmd/pango/demo.go` (347 lines of seed data) behind the `--demo` flag in `cmd/pango/main.go` |
| The screenshotter | `scripts/screenshots.mjs` (and `scripts/qa-shots.mjs`), wired up as `npm run screenshots` |

[Unreleased]: https://github.com/vul-os/pango/commits/main
