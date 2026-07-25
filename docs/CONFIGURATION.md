# Configuration

> [!WARNING]
> **📐 Designed, not implemented.** No PropFix process reads any of these
> settings today, because there is no PropFix process. This chapter specifies
> the intended configuration surface so it can be reviewed and so it can be
> implemented against a written target rather than invented per-flag.
>
> Status markers below are per-setting and will be promoted individually.

## Principles

These are constraints on the configuration system itself, taken from
[ARCHITECTURE.md](ARCHITECTURE.md) §11.

1. **No required config file.** A bare `./propfix` must be a working single-node
   deployment. Anything that must be configured before first run is a design
   failure.
2. **No default outbound network calls.** Every network destination is something
   an operator explicitly entered. Nothing is on by default.
3. **Secrets are never in argv.** Not in flags, not in `ps` output. Secrets come
   from environment variables or a file path, never a command-line value.
4. **Secrets are never logged**, never in `String()` or `Debug` output, and
   never in an API response.
5. **The database file is `0600`.** `*.db`, `*.sqlite*` and `.env` are treated
   as hazards by `.gitignore`.

## Precedence

Intended order, highest wins:

```
command-line flag  >  PROPFIX_* environment variable  >  built-in default
```

There is deliberately no config-file layer in the first cut. If one is added
later it sits below environment variables, and this section is updated in the
same change.

## Server — 📐 Designed

| Setting | Env | Flag | Default | Notes |
|---|---|---|---|---|
| Listen address | `PROPFIX_ADDR` | `--addr` | `127.0.0.1:8080` | Loopback by default. Binding to `0.0.0.0` is an explicit act. |
| Data directory | `PROPFIX_DATA_DIR` | `--data-dir` | `.` | Holds the database and attachments. |
| Database path | `PROPFIX_DB` | `--db` | `<data-dir>/propfix.db` | Created `0600`. |
| Attachment store | `PROPFIX_ATTACHMENTS` | `--attachments` | `<data-dir>/attachments` | Content-addressed blobs. |
| Demo mode | — | `--demo` | off | In-memory seeded dataset; **never** touches the database. Not a flag to run in production, and it refuses to start alongside `--db`. |
| Log level | `PROPFIX_LOG_LEVEL` | `--log-level` | `info` | `debug` must still never print a secret. |

## Identity — 📐 Designed

Each node generates an **Ed25519 keypair on first run**. That one identity signs
sync requests and the operations the node authors, and its public key is what
HLC ties break on (see [SYNC.md](SYNC.md)).

| Setting | Env | Default | Notes |
|---|---|---|---|
| Node key path | `PROPFIX_NODE_KEY` | `<data-dir>/node.key` | Generated if absent, mode `0600`. Losing it means the node must re-enrol with every peer. |

The node's id **is** its public key. This is deliberate: it means an HLC tie
breaks on the same value whether the built-in engine or a substrate engine is
running, which is the precondition for ever switching engines without changing
who wins a tie. See [ARCHITECTURE.md](ARCHITECTURE.md) §7.

## Sync — 📐 Designed

| Setting | Env | Default | Notes |
|---|---|---|---|
| Sync enabled | `PROPFIX_SYNC` | off | Off means the node is a standalone island, which is a perfectly good deployment. |
| Pairing secret | `PROPFIX_SYNC_SECRET` | unset | **Bootstrap only.** Authorises trust-on-first-use enrolment of a key; it is not the ongoing gate. Never passed as a flag. |
| Enrolled-peer secret fallback | `PROPFIX_SYNC_SECRET_FALLBACK` | **off** | With the default off, an enrolled peer presenting no valid signature is **rejected** — the mesh fails closed. |
| Freshness window | `PROPFIX_SYNC_SKEW` | `300s` | Signed envelopes outside ±window are rejected. |
| Sync folder | `PROPFIX_SYNC_FOLDER` | unset | A shared folder / NAS mount / USB path used as transport. Each node writes only its own `ops-<node>.jsonl`. |
| Sync interval | `PROPFIX_SYNC_INTERVAL` | `60s` | Background round per enabled peer. |

With no secret set **and** no enrolled key, every sync request is rejected.
Unenrolled peers are rejected by default.

## Merge engine — 📐 Designed

| Setting | Env | Default | Notes |
|---|---|---|---|
| Merge engine | `PROPFIX_MERGER` | `builtin` | `builtin` = the HLC oplog engine. `dmtap` = the DMTAP-SYNC substrate engine via the `store.Merger` seam. |

> [!CAUTION]
> **This is a deployment-wide switch, never a gradual rollout.** Two engines can
> both converge correctly and still pick *different winners* for the same
> history, because a tie-break is a property of the engine. Two nodes of one
> deployment must never run different engines. The seam is chosen at boot and
> never mixed.

## WRAP — 📐 Designed

| Setting | Env | Default | Notes |
|---|---|---|---|
| WRAP enabled | `PROPFIX_WRAP` | off | In-house maintenance never touches WRAP. |
| Pool endpoints | `PROPFIX_WRAP_POOLS` | unset | Comma-separated pool URLs. A pool distributes offers; it has no authority over assignment. |

See [WRAP.md](WRAP.md).

## Optional seams — 📐 Designed, and optional by contract

A hard runtime dependency on Ephor, a control plane, or DMTAP is
**forbidden**. Each of the following is a feature that lights up when configured
and is absent — not degraded, not broken — when it is not.

| Seam | Env | Default | If unset |
|---|---|---|---|
| Relay reachability | `PROPFIX_RELAY_URL` | unset | No relay is contacted. Peers reach each other directly, over a folder, or not at all. |
| Vulos OS host mode | `PROPFIX_DEPLOY_MODE` | `standalone` | `os` lets the Vulos OS supply identity and scoped storage in front of the same binary. |
| Map tiles | `PROPFIX_TILES_URL` | unset | Maps render without a basemap. MapLibre + Protomaps need **no API key**, and tiles can be served from a local file. |

## What is intentionally absent

- **No account system with us.** There is no signup, no licence key, no seat
  count, and no phone-home. There is nothing to configure because there is
  nothing there.
- **No telemetry setting**, because there is no telemetry to disable.
- **No cloud storage credentials.** Attachments are files on your disk.
