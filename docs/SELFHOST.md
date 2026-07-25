# Self-hosting

> [!WARNING]
> **📐 Designed, not implemented.** There is no binary and no image to deploy.
> This chapter records the intended operational shape of a PropFix deployment.
> Do not treat any command here as runnable yet.

Self-hosting is not a deployment option for PropFix. It is the only mode there
is. There is no hosted PropFix, no account with us, and no control plane that a
node checks in with.

## 1. What a deployment is

**One static binary and one SQLite file.**

That is the whole design target from [ARCHITECTURE.md](ARCHITECTURE.md) §1. The
frontend is embedded in the binary via `embed.FS`; the database is a file; the
attachments are a directory of content-addressed blobs. There is no database
server, no cache, no queue, no object store, and no reverse-proxy requirement.

Adequate hardware:

| Deployment | Notes |
|---|---|
| A laptop | Fine. This is a legitimate production deployment for a small landlord. |
| A tablet | Fine, and the intended field device. |
| A Raspberry Pi | Fine. `modernc.org/sqlite` is pure Go, so `GOARCH=arm64 go build` is the whole cross-compilation story — no cgo, no toolchain. |
| An office NAS | Fine, and a good fit for the folder sync transport. |
| A small VPS | Fine. Nothing about PropFix wants a big machine. |

## 2. Running it — 📐 planned

```bash
./propfix --data-dir /var/lib/propfix
```

Intended defaults worth knowing before you deploy:

- Binds **`127.0.0.1:8080`**. Exposing it is an explicit act
  (`--addr 0.0.0.0:8080`), not something that happens because you forgot a flag.
- Creates the database **mode `0600`**.
- Makes **no outbound network calls**. A fresh install talks to nothing: no
  update check, no licence call, no telemetry, no registration.

## 3. Exposing it safely

PropFix's sync signatures authenticate peers; they do **not** encrypt traffic
([SYNC.md](SYNC.md) §8). Neither does its web UI on plain HTTP. Options, best
first:

1. **Don't expose it.** A LAN-only node with folder sync is a complete,
   correct deployment for a lot of real businesses.
2. **A VPN or overlay you run** — WireGuard, Tailscale, Netbird. The node stays
   on loopback or a private interface; the overlay carries reachability.
3. **A reverse proxy you run**, terminating TLS with your own certificate.
4. **A tunnel.** [Ephor](https://github.com/vul-os/ephor) is one
   option and is a **purely optional convenience** — a hard runtime dependency
   on it is forbidden by the product standard. Be clear-eyed about what it is: a
   relay is a content-visible L7 hop that terminates TLS. It buys reachability,
   not confidentiality.

## 4. Backup

The intended model: **your data is a directory, and you back it up.**

```
/var/lib/propfix/
  propfix.db        your entire dataset
  node.key          this node's Ed25519 identity — 0600
  attachments/      content-addressed photos and files
```

- Back it up the way you back up anything else — rsync, restic, Borg, a NAS
  snapshot, a synced folder, your own bucket. PropFix ships **no backup
  service** and has no opinion about which you use.
- **Take a consistent snapshot**, not a copy of a live file. For SQLite that
  means the online backup API or stopping the process. Copying an in-use
  database with `cp` is how people acquire a corrupt backup that looks fine
  until they need it. A documented `propfix backup` command is 📐 designed and
  does not exist.
- **`node.key` is not the same kind of secret as the database.** Losing the
  database loses your data. Losing `node.key` loses the node's identity: it must
  re-enrol with every peer. Back it up, restrict it, and do not copy it to a
  second machine expecting two nodes — two nodes sharing one identity is a
  broken deployment, not a cluster.
- The **folder transport is not a backup.** `ops-*.jsonl` files are a replayable
  log and a fresh node can rebuild from them, but they are transport, never
  truth ([SYNC.md](SYNC.md) §9).

## 5. Multi-site

There is no cluster mode, because there is no cluster. Every site runs its own
complete node, and the nodes sync peer-to-peer with manual enrolment. Head office
as a hub, a full mesh, or two nodes and a USB stick are all supported topologies
— see [SYNC.md](SYNC.md) §6.

Nothing coordinates them. Nothing has to.

## 6. Upgrades

Intended: replace the binary and restart. Migrations are embedded, applied in
order, **each in its own transaction**, and recorded in `schema_migrations`. No
external migration tool.

Take a backup before an upgrade anyway. Migrations are code.

Downgrades are not supported. A migration that has run is not reversed by
running an older binary — take the backup.

## 7. As a Vulos OS app — 📐 planned

The same binary is intended to install as an app on a Vulos OS box, with the OS
supplying identity and scoped storage in front of it (`PROPFIX_DEPLOY_MODE=os`).

Two things must remain true, by contract:

- It is the **same binary**. There is no OS-only build and no standalone-only
  crippling.
- Self-hosting it yourself is the **default path and never second-class.**

## 8. What you will not find

- A licence server, a seat count, or an activation step.
- A "community edition" with features removed. There is one edition.
- A managed offering. Vulos bills for exactly two things — Relay reachability and
  backup storage buckets — and PropFix requires neither.
