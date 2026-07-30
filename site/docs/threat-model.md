# Threat model

> [!WARNING]
> **📐 Designed, not implemented.** This describes the security posture Pango
> is being built to, from [ARCHITECTURE.md](ARCHITECTURE.md) §11. **None of it
> is currently enforced by running code**, because there is no running code. A
> threat model for software that does not exist is a specification, and should
> be read as one.
>
> A separate document, [`SECURITY-AUDIT.md`](../SECURITY-AUDIT.md), records real
> findings against the **legacy** implementation and the repository history. That
> one is a report of facts, not a plan.

## 1. What is being protected

| Asset | Why it matters |
|---|---|
| Tenant and unit records | Names, addresses, occupancy. Personal data by any definition. |
| Job detail and history | Who was in whose home, when, and why. |
| Inspection findings and photos | Evidence used in deposit disputes; the *inside* of people's homes. |
| Cost and time records | Commercial data; the basis for billing. |
| The node's Ed25519 key | Its identity to every peer. |
| The pairing secret | Authorises a new key to be enrolled. |

Inspection photos deserve emphasis. A Pango database is a set of photographs
of the interiors of dwellings, keyed to their addresses and the names of the
people who live in them. It should be handled accordingly.

## 2. Who is in scope

| Adversary | In scope? |
|---|---|
| A stranger on the same network as a node | **Yes** |
| Someone who steals the device (laptop, tablet, Pi) | **Partially** — see §5 |
| A former contractor whose peer row was deleted | **Yes** |
| A malicious enrolled peer | **Partially** — see §5 |
| A user of one organisation trying to read another's data | **Yes** |
| A tenant trying to read internal notes | **Yes** |
| The operator of a relay or tunnel in the path | **Yes**, and the answer is unflattering — see §4 |
| A hostile host / cloud operator with root on the box | **No.** Root on the box is game over; Pango does not pretend otherwise. |
| A nation-state adversary targeting a specific user | **No.** |

## 3. Design decisions that carry security weight

### Tenant isolation is derived, never supplied

Every row carries `org_id`, and scoping is enforced **server-side from the
authenticated identity**. It is never taken from a client-supplied parameter.

This is a direct correction of a legacy defect: the previous implementation
trusted the frontend to send `organization_id` filters. That is not isolation —
it is a suggestion, enforced by the attacker.

### Tenants are participants, not accounts

A tenant is attached to a unit or a job and sees only events with
`visibility = 'public'`. One thread serves internal notes and tenant
communication, gated by that flag.

The flag is therefore a **security control, not a UI preference**. Any code path
that returns events without applying it is a vulnerability, and the visibility
filter belongs on the server, next to the query, not in a component.

### Append-only records

Costs, times, job events and findings are insert-only. This is primarily a
correctness property ([SYNC.md](SYNC.md) §4) but it has a security consequence:
**the audit trail is complete by construction.** There is no edit path to abuse
and no history to quietly rewrite. A correction is a new, attributable entry.

### No default outbound network calls

A fresh install talks to nothing. No telemetry, no update check, no licence
call, no analytics, no font CDN, no map-tile provider (MapLibre + Protomaps need
no API key). Every network destination is one an operator entered.

This shrinks the attack surface to what you configured, and it means a node on
an air-gapped network is fully functional rather than degraded.

### Secrets handling

- Never in argv — so never in `ps`, never in shell history.
- Never logged, never in `String()` / `Debug` output, never in an API response.
- The database file is `0600`; `node.key` is `0600`.
- `.gitignore` treats `*.db`, `*.sqlite*` and `.env` as hazards.

## 4. Sync: what the signatures do and do not do

The full transport analysis, including the per-threat table, is in
[SYNC.md](SYNC.md) §8. The two sentences that matter most here:

**Mutual Ed25519 signatures authenticate peers. They do not encrypt the
payload.** Sync traffic is job detail, tenant names, unit addresses and costs, in
the clear unless you put it on a path that encrypts.

Run sync over a LAN, a VPN/overlay you control, or TLS you terminate. A **Vulos
Relay** tunnel is an optional convenience and is a **content-visible L7 hop** —
it terminates TLS and can see what passes through. It buys reachability, not
confidentiality. Saying otherwise would be the kind of claim this project exists
to avoid.

### Revocation is two steps

Deleting a peer row removes its recorded key, so that node can no longer
authenticate. But a node that still knows the **pairing secret** can bootstrap a
fresh key. Complete revocation is therefore **delete the peer row *and* rotate
the pairing secret.** Any UI that offers "remove peer" without saying this is
misleading, and that is a documentation requirement on the eventual UI.

## 5. What an attacker gets

Stated plainly, because a threat model that only lists mitigations is marketing.

| Situation | What they get |
|---|---|
| **Stolen unlocked device** | Everything on it. Pango does not encrypt the database at rest. Use full-disk encryption; it is the OS's job and the OS does it better. |
| **Stolen locked device, no FDE** | The database file and the attachments. `0600` stops another *user*, not someone holding the disk. |
| **Root on the box** | Everything, including `node.key` — so the ability to impersonate that node to its peers until it is revoked. |
| **A malicious enrolled peer** | Everything that node is meant to receive, which for a full peer is the replica. **Enrolment is trust.** Enrol a contractor's node only if you would give that contractor the data. |
| **Someone who learns the pairing secret** | The ability to enrol a *new* key. They cannot forge requests as an existing enrolled node — those verify against the recorded key. Rotate the secret. |
| **A relay / tunnel operator in the path** | Whatever crosses it, unless you terminated TLS yourself end to end. |
| **Passive observer on a LAN with plain HTTP** | The traffic. |

## 6. Residual risks, unmitigated

1. **No encryption at rest.** Deliberate: a single-binary, single-file product
   that manages its own encryption keys is a product that loses people's data.
   Full-disk encryption is the correct layer. This is a decision, not an
   oversight, and it is recorded here so nobody has to guess.
2. **Sync payloads are not encrypted end-to-end.** Confidentiality is delegated
   to the transport you choose.
3. **A full peer replicates the whole dataset.** There is no partial replication
   or per-building peer scoping in the design. A contractor's node enrolled as a
   full peer is a full peer. Whether WRAP (which crosses the boundary with
   *messages*, not replication — [WRAP.md](WRAP.md) §6) is always the right tool
   for contractors, rather than peer enrolment, is a design question worth
   settling before the first cross-organisation deployment.
4. **Photo metadata.** Inspection photos may carry EXIF including GPS. Stripping
   it is 📐 designed and not implemented, and until it is, photos carry whatever
   the camera wrote.
5. **No rate limiting specified** on the sync or API surface.
6. **No account lockout, password policy, or 2FA specified.** The authentication
   model for human users is not yet written down at all — that is a gap, and it
   is listed here rather than left to be discovered.
7. **The legacy findings.** Credentials exposed by the previous implementation
   are catalogued in [`SECURITY-AUDIT.md`](../SECURITY-AUDIT.md), including items
   marked unresolved. Rebuilding the code does not rotate a key; read that
   document, do not assume this one supersedes it.

## 7. Reporting

Please report vulnerabilities privately. See [`SECURITY.md`](../SECURITY.md). Do
not open a public issue for a security report.
