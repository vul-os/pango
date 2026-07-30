# Running a Pango node on a public address

> [!WARNING]
> **📐 Designed, not implemented.** This chapter records the intended operational
> shape of a publicly reachable Pango node. The sync protocol and merge engines
> exist and are tested; the deployment surface around them — the settings in
> [CONFIGURATION.md](CONFIGURATION.md), the enrolment flow, the release
> artifacts — is specified rather than shipped. Do not treat any command here as
> runnable yet.

Every other chapter assumes a **trusted path** — a laptop, an office LAN, a NAS,
a VPN you run yourself. This one is the other case: a Pango node on a rented box
or behind a tunnel, reachable from the open internet, so a tablet on mobile data
or a contractor across town can sync to it without a third party holding your
data.

**It is a different threat model, and the difference is not a footnote.** On a LAN
the network is a boundary. On a public address there is no boundary, and every
property below has to hold on its own. Read the whole page before you open the
port.

---

## 1. A cloud node is a convenience, never an authority

Pango sync is **dial-out only**. A round is stateless and symmetric, and the
caller always initiates, so **only one side of a pair has to be reachable**
([SYNC.md](SYNC.md) §6). A tablet behind CGNAT syncs by dialing the office; the
office never dials the tablet.

That is the whole reason to run a public node, and it is the whole limit of what
one is for:

- it is **not** a hub, a coordinator, a directory, a rendezvous service, or a
  source of truth;
- nothing elects it, and no node is configured to prefer it;
- nothing in Pango has a default endpoint — not disabled by default, **absent**.
  A missing default endpoint is a centralisation that cannot happen, where a
  disabled one is a centralisation you can only opt out of.

Delete the cloud node and the mesh still converges by any other path it has,
including a USB stick ([SYNC.md](SYNC.md) §9).

## 2. The shapes a node comes in

**Same binary, every time.** The shape is a deployment choice, not a role in a
protocol, and none of them is load-bearing.

| Shape | What it is | What it is *not* |
|---|---|---|
| **Laptop / tablet** | The intended field device. Loopback bind, syncs by dialing out. | Not a client. It holds the whole database. |
| **Office LAN box / NAS** | `pango --addr 0.0.0.0:8099` on a machine that stays awake. Good fit for the folder transport. | Not a server in the client-server sense. Nothing depends on it being up. |
| **Rented VPS** | The same binary on hardware you rent instead of own. Reachable, so NAT'd nodes can dial it. | Not *our* infrastructure. There is no Pango-operated anything, and renting a box does not create one. |
| **Container** | The image built from [`Dockerfile`](../Dockerfile): `PANGO_PORT=8099`, data on a volume at `/data`. | Not an orchestration story. One container, one SQLite file. |
| **Tunnelled node** | A node on a private machine made reachable by a tunnel (§5). Cheapest way to get one reachable endpoint without renting anything. | Not private. The tunnel operator is in the content path — see §5. |

## 3. Which merge engine — decide this before you expose anything

A publicly reachable node **should** run the substrate engine
(`--merge-engine substrate`, or `PANGO_MERGER=substrate`), not the built-in one.

|  | `builtin` (default) | `substrate` |
|---|---|---|
| Sync **request** authenticated by node key | yes | yes |
| Op **batch** signed by the sending node | yes | yes |
| Each **op** signed by its **author** (`COSE_Sign1`) | **no** | yes |
| A relayed op is attributable to its author | **no** | partly — see the caution below |
| Merge algebra | Pango's HLC oplog | the shared, vector-verified DMTAP-SYNC algebra |

The row that decides it is the third. `signBatch` signs the **batch**, not each
op (`backend/internal/sync/sync.go`), and an op's `Author` field is a plain
string claim on the record. So under the built-in engine a peer relaying a third
node's changes vouches for them with its own key, and **nothing distinguishes a
relayed op from one that peer invented**. On a LAN that is a tolerable
simplification. On a public address it is not: the point of exposing a node is
that others can reach it, and an enrolled peer that turns hostile — or is
compromised — must not be able to author history in another node's name.

Unlike the rest of the suite this is a **runtime flag, not a build tag**: the
substrate algebra is a WebAssembly module run by wazero, so `CGO_ENABLED=0` and
single-static-binary cross-compilation are unaffected and there is no separate
artifact to ship.

> [!CAUTION]
> **The engine is a deployment-wide choice, and switching it is not gradual.**
> Two engines can both converge correctly and still pick *different winners* for
> the same history, because a tie-break is a property of the engine. Move every
> node together. An unrecognised value is fatal rather than defaulted, which is
> deliberate — a typo that quietly ran the other algebra is the one mistake that
> cannot be detected afterwards. See
> [CONFIGURATION.md](CONFIGURATION.md#merge-engine--implemented).

> [!WARNING]
> **Known gap: `substrate` does not currently fail closed on an unsigned op.**
> `substrate.Ingest` refuses an op carrying no envelope, but the apply path never
> hands it one: `backend/internal/sync/ops.go` sees `op.Cose == ""`, calls
> `NoteLegacy()`, and journals and applies the op anyway. The reasoning is sound
> for a half-migrated fleet — refusing would present a misconfiguration as a
> transport failure — but on a public address it means a hostile enrolled peer can
> still author history in another node's name by simply **omitting the envelope**
> and setting `Author` to whoever it likes. The batch signature proves only who
> relayed it.
>
> Until that is closed, treat a public Pango node as trusted-peers-only: enrol
> nodes you control or contractors you would already trust with the data, and do
> not treat enrolment as a security boundary against a peer that turns hostile.

## 4. Bind address

The listener binds whatever `--addr` says, and the default is **loopback**
(`127.0.0.1:8099`) so a fresh install is not accidentally public.

```bash
PANGO_ADDR=0.0.0.0:8099 ./pango --db /var/lib/pango/pango.db
```

`0.0.0.0` binds every interface. If your provider also gives you a private
interface, prefer binding the one address you mean:

```bash
./pango --addr 10.0.0.4:8099      # private interface only
./pango --addr 127.0.0.1:8099     # proxy on the same host, nothing else
```

Binding loopback and putting a reverse proxy in front is the better shape
whenever the proxy is on the same host, because then the only thing on the public
interface is a process whose job is to terminate TLS.

## 5. TLS — Pango does not terminate it

Sync signatures authenticate peers; they do **not encrypt the payload**
([SYNC.md](SYNC.md) §8). Neither does the web UI on plain HTTP. Sync traffic is
job detail, tenant names, unit addresses and costs. On a public address, plain
HTTP is not an option you get to weigh.

Options, best first:

1. **A VPN or overlay you run** — WireGuard, Tailscale, Netbird. The node stays
   on loopback or a private interface and the overlay carries reachability. No
   third party is in the content path. This is the recommended shape.
2. **A reverse proxy you run**, terminating TLS with your own certificate:

   ```
   # /etc/caddy/Caddyfile
   pango.example.com {
       reverse_proxy 127.0.0.1:8099
   }
   ```

   Caddy gets a certificate itself; with nginx or HAProxy, bring your own.
3. **A tunnel** — ngrok, cloudflared, or the suite's own
   [Ephor](https://github.com/vul-os/ephor). Be clear-eyed about the trade:

   ```bash
   ngrok http 8099          # then enrol peers on the https://… URL it prints
   cloudflared tunnel --url http://127.0.0.1:8099
   ```

   A tunnel **buys reachability, not confidentiality.** It is a content-visible
   L7 hop: it terminates TLS and the operator can see everything that passes
   through, including tenant names and costs. It is also a moving endpoint on the
   free tiers — a URL that changes on restart means re-enrolling every peer, so
   use a reserved domain for anything that has to keep working.

   Ephor is an **optional convenience only**. A hard runtime dependency on it is
   forbidden by the product standard, nothing about sync consults it, and it is
   not a default anywhere.

## 6. Firewall

Only two things need to be reachable, and on the proxy shape only one:

| Port | Who needs it |
|---|---|
| 443 | Peers and browsers, via your proxy or tunnel. |
| 8099 | **Nobody**, if a proxy on the same host is doing TLS. Do not open it. |
| 22 | You. Key auth only. |

Default-deny inbound, then allow those. If the node is bound to `0.0.0.0`
*without* a proxy, you have published a plain-HTTP database — go back to §5.

## 7. Enrol the other nodes

Discovery is manual and stays manual ([SYNC.md](SYNC.md) §7). Nothing scans,
broadcasts, or looks anything up.

On the public node, set a pairing secret and enable sync:

```bash
PANGO_SYNC=1 PANGO_SYNC_SECRET=<generated> ./pango --addr 127.0.0.1:8099
```

The secret is a **bootstrap, not a gate**: it authorises trust-on-first-use
enrolment of a key, after which the node authenticates by key and the secret is
no longer consulted. Never pass it as a flag — it belongs in the environment or a
unit file, because a flag is visible in `ps`.

On each other node, enrol the public one by URL. `https://` on anything public:

```
peer name  head-office
peer url   https://pango.example.com
```

Then, on the public node:

- leave **`PANGO_SYNC_SECRET_FALLBACK` off** (the default) so an enrolled peer
  that presents no valid signature is **rejected** — the mesh fails closed;
- **rotate the secret once enrolment is done.** Anyone who learns it can enrol a
  *new* key. They cannot forge requests as an existing enrolled node, but a new
  key is enough to read.
- to revoke a contractor, **delete the peer row *and* rotate the secret.** The
  row alone is not enough: the secret would let them bootstrap a fresh key.

## 8. Durability

A public node holds the same complete database as every other node, so it is a
backup of the mesh and not a thing that needs special treatment — but it is also
the node most likely to be destroyed by someone else's billing system.

Stop, copy every `pango.db*` file **together** (the WAL and shm matter) *and*
`node.key`, start:

```bash
systemctl stop pango
cp /var/lib/pango/pango.db* /var/lib/pango/node.key /backup/
systemctl start pango
```

Skipping `node.key` is how a restore silently mints a new identity instead of
resuming the old one — see [SELFHOST.md](SELFHOST.md) §4. Attachments are
content-addressed blobs in a directory; copy it too. See
[SELFHOST.md](SELFHOST.md) §4.

## 9. What this setup still does not protect you from

- **The tunnel or relay operator**, if you used one. Content-visible L7 hop. See
  [THREAT-MODEL.md](THREAT-MODEL.md) §4.
- **Your hosting provider.** They hold the disk. Full-disk encryption on a VPS
  protects against a decommissioned drive, not against a running hypervisor.
- **An enrolled peer that turns hostile.** See the §3 warning. This is the gap
  worth understanding before you enrol anyone you would not already trust.
- **A stolen node key.** It is the identity. `<data-dir>/node.key`, mode `0600`;
  losing it means re-enrolling with every peer, and leaking it means someone can
  be that node until you delete its row everywhere.
- **Anyone with the pairing secret**, until you rotate it.

---

**See also:** [SELFHOST.md](SELFHOST.md) for the single-node case ·
[SYNC.md](SYNC.md) for the protocol · [CONFIGURATION.md](CONFIGURATION.md) for
every setting · [THREAT-MODEL.md](THREAT-MODEL.md) for who you are trusting.
