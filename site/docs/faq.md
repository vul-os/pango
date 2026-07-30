# FAQ

## Can I use Pango today?

**No.** It is being rebuilt from scratch and there is no runnable software: no
binary, no release, no image, no UI. What exists is the design contract in
[ARCHITECTURE.md](ARCHITECTURE.md) and this documentation set.

If you came here looking for maintenance software to deploy this month, Pango
is not it yet. Come back, or better, read the architecture and tell us where it
is wrong.

## Why rebuild rather than fix what was there?

The previous implementation was cloud-coupled and had structural problems that
were not patch-shaped. Two are worth naming because they shaped the new design:

1. **Units were free text on the job** (`unitIdentifier`) with no table — while
   the analytics grouped by that text. "Flat 3A", "3A" and "flat 3a" silently
   became three different units, fragmenting the per-unit cost reporting that is
   the product's main analytical claim. Units are now real rows with a
   normalised key.
2. **Tenant isolation was taken from the client.** The frontend was trusted to
   send `organization_id` filters. That is not isolation. Scoping is now derived
   server-side from the authenticated identity.

Separately, the repository history contained credentials. That is documented in
[`SECURITY-AUDIT.md`](../SECURITY-AUDIT.md), including items still marked
unresolved.

## Is there a hosted version? A free tier? A price?

There is no hosted Pango, no account with us, no free tier, and no price,
because there is nothing to sell you. It is MIT-licensed software you run.

Vulos publishes one thing: the OS, free and open-source, dual MIT OR
Apache-2.0, run by you on your own hardware or a VPS you rent. There is no
billing anywhere in the stack — no compute/box billing, no per-seat charge,
and no app-store subscription.

## Do I need Vulos anything to run it?

No. A hard runtime dependency on Ephor, a control plane, or DMTAP is
**forbidden** by the product standard. They are optional seams. A Pango node
with none of them configured is a complete deployment, not a degraded one.

## Does it phone home?

No. A fresh install makes **no outbound network calls at all** — no telemetry,
no update check, no licence call, no analytics, no font CDN, and no map-tile
provider (MapLibre + Protomaps need no API key). Every network destination is
one you entered.

If a build ever contacts something you did not configure, that is a bug worth
reporting loudly.

## How does it work offline?

Every surface is designed to accept writes while partitioned and converge
afterwards. Two rules make that correct rather than merely possible:

- **Money and hours are append-only.** Two people costing the same job offline
  produce two immutable rows that **add**. A stored `cost` column would keep the
  last write and silently lose the other.
- **The building is the authority.** The only contended decision — who does the
  work — has exactly one legitimate writer, so there is no consensus protocol,
  no leader election, and no distributed lock anywhere.

Details in [SYNC.md](SYNC.md).

## There is really no server?

Really. Peers are enrolled by hand and sync directly, in stateless symmetric
rounds where each side pushes what the other lacks and pulls what it lacks. Only
one side of a pair needs to be reachable.

And if neither is: point both at a shared folder. Each node appends only its own
`ops-<node>.jsonl`, so a NAS, a synced drive or a **USB stick carried between
sites** is a valid transport with no possible write conflict.

## How do peers find each other?

They don't. **Discovery is manual** — an operator types the peer's URL. No mDNS,
no DHT, no rendezvous, no directory. That costs you a human action per peer, and
buys you a node that advertises nothing and no discovery infrastructure to
compromise.

## Is my sync traffic encrypted?

**Not by Pango.** Mutual Ed25519 signatures authenticate peers; they do not
encrypt the payload. Run sync over a LAN, a VPN/overlay you control, or TLS you
terminate. A relay tunnel buys reachability, not confidentiality — it terminates
TLS and can see what passes through. See [THREAT-MODEL.md](THREAT-MODEL.md).

## Is the database encrypted at rest?

No, and that is a decision rather than an oversight. A single-file product
managing its own encryption keys is a product that loses people's data. Use
full-disk encryption — it is the OS's job and the OS does it better.

## What is WRAP, and do I have to use it?

[WRAP](https://github.com/vul-os/wrap) is an open work-coordination protocol.
Pango uses its `trades/v0` profile so a contractor can run **their own**
Pango node and receive work orders from a managing agent's node — no platform
between the landlord and the plumber, and nobody taking a cut.

It is **optional** and off by default. In-house maintenance never touches it.
See [WRAP.md](WRAP.md).

## Do tenants need accounts?

No. A tenant is a **participant, not an account** — attached to a unit or a job,
seeing only events marked `visibility = 'public'`. They hold no key, install
nothing, and sign up for nothing in order to report a leak and be told it is
fixed.

## What does the inspection comparison actually decide?

Nothing. It surfaces per-item deltas between the ingoing and outgoing runs, with
both sets of photos and comments. It explicitly distinguishes "deteriorated"
from "**not captured ingoing**" and does not present a missing baseline as
evidence of damage.

Fair wear and tear is a legal judgement that varies by jurisdiction and lease.
Software asserting it would be confidently wrong. A human decides; Pango makes
the evidence comparable. See [INSPECTIONS.md](INSPECTIONS.md).

## Why Go and SQLite?

Because the deployment target is one file on a Pi, a tablet, or a NAS.
`modernc.org/sqlite` is pure Go, so there is no cgo, no `libsqlite3`, and
cross-compiling to arm64 is a plain `go build`. A Postgres dependency would make
"one binary and one file" a lie.

## Will there be a mobile app?

The frontend is a web app and the field device is a tablet browser. A native app
is not planned. Offline capture is a requirement of the web app, not a reason
for a second codebase.

## Can I run Pango on a Vulos OS box?

That is the intent — the **same binary**, with the OS wiring identity and scoped
storage in front of it. Self-hosting it yourself remains the default path and is
never second-class. Not implemented yet.

## How can I help?

Read [ARCHITECTURE.md](ARCHITECTURE.md) and argue with it. Right now a good
objection to the contract is worth more than a patch. After that: `store/`
migrations and the HLC oplog, the `domain/` invariants, and the inspection
comparison engine. See [`CONTRIBUTING.md`](../CONTRIBUTING.md).

One rule above all others: **do not describe unbuilt work as built.**
