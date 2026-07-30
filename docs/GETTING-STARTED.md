# Getting started

> [!WARNING]
> **Nothing on this page runs yet.**
>
> Pango is being rebuilt from scratch. At the time of writing there is no
> binary, no published image, no release, and no UI. This chapter documents the
> intended path from clone to first job, so that the shape of the product is
> reviewable before it is built — and so that each command can be promoted to
> "works" individually, rather than the whole page being wrong at once.
>
> Every step below carries a status marker. **📐 Designed** means specified,
> not implemented. Only steps marked **✅ Built** actually work.

## Status at a glance

| Step | Status |
|---|---|
| Clone the repo | ✅ Built |
| `npm run docs:sync` (docs → site) | ✅ Built |
| Build the Go binary | 📐 Designed |
| Build the frontend | 📐 Designed |
| `pango` (run a node) | 📐 Designed |
| `pango --demo` | 📐 Designed |
| Create a building and a unit | 📐 Designed |
| Raise, cost and close a job | 📐 Designed |
| Run an inspection | 📐 Designed |
| Enrol a sync peer | 📐 Designed |
| Send work to a contractor over WRAP | 📐 Designed |
| Docker image | 📐 Designed |

## Prerequisites

- **Go 1.25+** — the backend is pure Go and uses `modernc.org/sqlite`, so there
  is no cgo toolchain, no `libsqlite3`, and cross-compilation to `arm64` (a Pi,
  an Apple-silicon laptop, an ARM VPS) is a plain `GOARCH=arm64 go build`.
- **Node.js 20+** — for the React 19 + Vite + Tailwind frontend.

Nothing else. No database server, no Docker requirement, no message broker, no
account.

## 1. Clone — ✅ Built

```bash
git clone https://github.com/vul-os/pango
cd pango
```

What you will find today: [`docs/ARCHITECTURE.md`](ARCHITECTURE.md) (the binding
contract), this documentation set, the hand-written marketing site under
`site/`, and the skeleton directories the rebuild is landing into.

## 2. Build — 📐 Designed

Intended:

```bash
npm install
npm run build      # frontend bundle, then the Go binary with the site embedded
```

The frontend is embedded into the binary via `embed.FS` so that deployment is a
single file. There is no separate static host, no CDN, and no build-time
dependency on any sibling Vulos repo (`file:` paths into siblings are forbidden
by the product standard).

Backend-only, once `backend/` has code:

```bash
go build ./backend/cmd/pango
```

## 3. Run a node — 📐 Designed

```bash
./pango
```

Intended defaults, all of which are part of the design contract rather than
observed behaviour:

| | Default |
|---|---|
| Listen address | `127.0.0.1:8099` — loopback unless you opt out |
| Database | `./pango.db`, created mode `0600` |
| Attachments | `./attachments/`, content-addressed |
| Outbound network calls | **none** |
| Accounts required | none |

A fresh install talks to nothing. There is no telemetry, no update check, no
licence call, and no registration. If Pango ever makes a network request you
did not configure, that is a bug — please report it.

See [CONFIGURATION.md](CONFIGURATION.md) for every flag and environment
variable, each marked with its own status.

## 4. Demo mode — 📐 Designed

```bash
./pango --demo
```

Intended to seed an in-memory dataset — a handful of buildings, units, jobs,
cost entries, an inspection template, and a completed ingoing inspection — so
that the whole UI is browsable with **no database, no configuration and no
signup**. It is what the screenshotter will run against, and it is meant to be
the first thing a new contributor sees.

Demo mode writes nothing to disk and is discarded on exit.

## 5. Your first building, unit and job — 📐 Designed

The intended flow, in order, because the ordering encodes a design decision:

1. **Create an organisation.** It is the tenancy boundary; every row carries
   `org_id`, and scoping is derived server-side from the authenticated identity,
   never from a parameter the client sends.
2. **Create a building** — name, address, and optionally `lat`/`lon` (used for
   proximity ranking) and a `unit_scheme` describing the local numbering
   convention.
3. **Create or reference a unit.** Units are *real rows*, created on first use,
   with a normalised `key` and the display `label` exactly as typed. The
   building's `unit_scheme` drives normalisation. This is the fix for the legacy
   system's central analytical flaw: it stored units as free text on the job and
   then grouped its per-unit spend reports by that text, so "Flat 3A", "3A" and
   "flat 3a" silently became three different units.
4. **Raise a job** against that unit. It receives a number from the
   **building's** sequence — namespaced per building, so numbers are allocated
   with no coordination between nodes, even offline.
5. **Work it.** Job events are append-only and carry a `visibility` flag;
   `public` events are the ones a tenant can see. One thread serves both
   internal notes and tenant communication, gated by that flag.
6. **Cost it.** Every cost is an insert into `cost_entry` (`amount_minor`,
   `kind`), never an edit. The job's cost is `SUM(amount_minor)` at read time.
   A correction is a **new negative entry**. The audit trail is therefore
   complete by construction.
7. **Close it**, and read spend per building and per unit off the aggregates.

## 6. Inspections — 📐 Designed

Build a template once, run it many times, and diff the outgoing run against the
ingoing one. The comparison is the product's differentiator, and it is specified
in detail in [INSPECTIONS.md](INSPECTIONS.md).

## 7. Add a peer — 📐 Designed

There is no discovery. You type in the peer's URL, exchange a bootstrap secret
once, and from then on both sides authenticate by Ed25519 key. Rounds are
stateless and symmetric — each pushes what the other lacks and pulls what it
lacks — so only one side of any pair needs to be reachable.

If neither side is reachable, point both at a shared folder (or carry a USB
stick between them). The full protocol, including the threat table, is in
[SYNC.md](SYNC.md).

## 8. Send work outside your organisation — 📐 Designed

In-house maintenance never touches WRAP. When work leaves the organisation,
Pango speaks [WRAP](https://github.com/vul-os/wrap) `trades/v0`, so a plumbing
company running **its own** Pango node receives work orders directly from a
managing agent's node — no platform between the landlord and the plumber, and
nobody taking a cut. See [WRAP.md](WRAP.md).

## What to do instead, today

Read [ARCHITECTURE.md](ARCHITECTURE.md). It is short, it is binding, and it is
the thing that is actually finished. If you disagree with something in it, that
disagreement is worth more right now than any patch.
