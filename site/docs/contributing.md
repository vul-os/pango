# Contributing to PropFix

Thanks for looking. PropFix is being rebuilt from scratch, so contributions
land on a mostly empty floor — which is the good part.

## Read this first

[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) is **the binding contract.** It
records the non-negotiables. Read it before changing anything structural, and
change it deliberately rather than letting the code drift away from it.

Right now, a well-argued objection to something in that document is worth more
than a patch.

## The one rule that matters most

**Do not describe unbuilt work as built.**

Anything partially built MUST say so where a reader will meet it — a README
bullet, a doc chapter, or the UI itself. "Designed but not yet implemented" is
an acceptable state to ship. A feature that silently does nothing is not.

Concretely:

- A PR that implements half a feature updates the status marker to reflect the
  half that works, in the same PR.
- A PR that finishes a feature promotes its marker from 📐 to ✅, in the same PR.
- Adding a design document does **not** entitle a feature to appear under
  *Added* in the changelog. Documentation is documentation.
- Never commit a mockup or a render as if it were a screenshot of the app.

This is not a style preference. A reader must never come away believing
something works that does not.

## Frozen invariants

These come from the contract. A PR that breaks one needs to change the contract
first, in a separate, deliberate commit.

1. **Money is `int64` minor units.** Floats never touch a money path. There is a
   test that fails if a `float64` appears in one.
2. **`cost_entry` and `time_entry` are immutable and insert-only.** A job's cost
   is `SUM(amount_minor)` at read time, never a stored column. A correction is a
   new negative entry, never an edit. This is the rule most likely to be broken
   by someone adding a feature — see [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) §6.
3. **`domain/` must not import `repo/`, `api/` or `store/`.** Dependencies point
   inward. There is no god-service module; logic lives with its aggregate.
4. **The building is the authority.** Its owning organisation is the single
   writer for the job record, the job-number sequence, assignment, and
   inspection scheduling. Do not introduce a consensus protocol, leader
   election, or a distributed lock — the design exists specifically so that none
   is needed.
5. **Units are rows, never free text.** Normalised `key`, display `label`.
6. **Tenant isolation is derived server-side from the authenticated identity**,
   never from a client-supplied scope parameter.
7. **No hard runtime dependency on Ephor, a control plane, or DMTAP.**
   Optional seams only. Feature-scoped degradation is the intended design.
8. **No default outbound network calls.** A fresh install talks to nothing.
9. **Secrets** are never in argv, never logged, never in `String()`/`Debug`
   output, never in an API response.
10. **Pure Go, no cgo.** `modernc.org/sqlite`, so an arm64 cross-compile stays a
    plain `go build`.

## Development

```bash
# Backend (Go 1.25)
go build ./...
go test ./...
go vet ./...

# Docs -> site (works today)
npm run docs:sync      # copy docs/*.md into site/docs/
npm run docs:check     # fail if the copies are out of date
```

If you edit anything in `docs/`, run `npm run docs:sync` and commit the result.
The site's docs viewer reads only what that script produced — a sibling repo let
its published docs drift from its repo docs by copying them by hand, and readers
were served stale text with no signal.

**Every command a README or docs page tells a reader to run has to actually
work**, and be wired into CI wherever CI exists. If a documented command cannot
run yet, say so beside the command — as this repository does throughout — rather
than letting the reader discover it.

## Migrations

Embedded via `embed.FS`, applied in order, each in **its own transaction**,
recorded in `schema_migrations`. No external tool.

Version numbers go by **feature epoch** — `1`, `100`, `200`, `300` — with `+1`
for follow-ups inside an epoch. That leaves room to insert, and makes the list
read as a changelog.

A migration that has shipped is never edited. Write a new one.

## Commits and pull requests

- Small, logical commits with a clear subject line.
- For a substantial change, open an issue first so the design conversation
  happens before the code.
- No CLA required.

## Where help is most useful

- `store/` — migrations, the HLC oplog, the `Merger` seam.
- `domain/` — entities and invariants, with tests that pin the append-only and
  authority rules.
- `inspect/` — the ingoing/outgoing comparison engine, including the honest
  "not captured ingoing" case.
- The open questions recorded in [`docs/SYNC.md`](docs/SYNC.md) §11 and
  [`docs/INSPECTIONS.md`](docs/INSPECTIONS.md) §3.

## Security

Do not open a public issue for a vulnerability. See [`SECURITY.md`](SECURITY.md).

## License

By contributing you agree that your contributions are licensed under the
[MIT License](LICENSE).
