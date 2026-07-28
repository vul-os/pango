# Conformance vectors

Normative test data for behaviour that more than one implementation has to
agree on. A vector file is the statement of the rule; the prose in `docs/` is
the explanation of it. Where the two disagree, the vectors are right.

Everything here is plain JSON with no runtime and no network dependency. It is
checked in, not fetched. Nothing in PropFix's build or runtime reads it — only
tests do — so this directory can be copied, forked or ignored without affecting
whether PropFix builds or runs.

## `hlc_vectors.json` — HLC stamp format, order and tie-break

Run by `backend/internal/store/hlc_vectors_test.go`.

### Why this file exists

PropFix's `internal/store/hlc.go` is a near-copy of FlowStock's
`backend/internal/store/hlc.go`. The agreed convergence path for the Vulos
suite is a published substrate crate plus a spec and vectors — **that crate
does not exist yet**, and until it does neither product may import the other
(see `docs/SYNC.md` §11, "Duplicated sync substrate").

So the two copies cannot be deduplicated, but they can be made to *verifiably*
agree. This file is that agreement, narrowed to the part where a disagreement
is silent and expensive: two engines that both converge and still pick
different winners for the same history.

### What it pins

| Group | Pins |
|---|---|
| `parse` | which stamps are valid, including the fixed-width bounds that must fail closed |
| `order` | that plain string comparison equals `(wall, counter, tiebreak)` comparison |
| `sorted` | the same property over a whole set, sorted as raw strings |
| `tick` | minting: counter advance, seeding past the oplog, backwards wall clocks, counter spill, saturation at the top of the wall range |
| `observe` | folding a remote stamp in, and refusing to be dragged out of width by one |

### Running it against another implementation

The runner (`hlc_vectors_test.go`) is deliberately small and depends on four
things only: `NewHLC(tiebreak, lastSeen)`, `Tick()`, `Observe(remote)` and
`ParseHLC(ts)`, plus an injectable clock. FlowStock's `internal/store` exposes
the same four under the same names, so adopting this file there is a copy of
the runner with the package's own field names, not a port.

One naming difference, no behavioural one: PropFix calls the third field the
**author public key** and FlowStock calls it the **node id**. FlowStock's node
ids *are* public keys, so the same bytes end up in the same position and the
vectors apply verbatim to both.

### Changing a vector

A vector change is a protocol change. Change it in both repositories in the
same session, or not at all — a stamp minted under the old rule is already in
somebody's oplog.
