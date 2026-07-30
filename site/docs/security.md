# Security policy

## Reporting a vulnerability

**Please report vulnerabilities privately. Do not open a public issue.**

Use GitHub's private vulnerability reporting on
[`vul-os/propfix`](https://github.com/vul-os/propfix/security/advisories/new),
or contact the maintainers privately if that is unavailable.

Please include:

- what the issue is and how to reproduce it;
- the commit or version you tested;
- the impact as you see it, and any mitigation you already know of.

You will get an acknowledgement. If the report is valid you will be credited in
the fix unless you would rather not be.

## Supported versions

| Version | Supported |
|---|---|
| `main` | Yes — the rebuild in progress |
| Legacy pre-rebuild implementation | **No.** Not maintained, not patched, being replaced. |

There is no released, runnable version of the rebuilt product yet — see
[`CHANGELOG.md`](CHANGELOG.md). Reports against the design in
[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) and
[`docs/THREAT-MODEL.md`](docs/THREAT-MODEL.md) are welcome and useful: a design
flaw found now is cheaper than one found later.

## Scope

**In scope**

- Tenant / organisation isolation failures — anything where scoping could be
  influenced by a client-supplied value rather than derived from the
  authenticated identity.
- Tenant visibility failures — any path that returns job events without applying
  the `visibility = 'public'` filter. That flag is a security control, not a UI
  preference.
- Sync authentication: signature verification, the freshness window, the replay
  nonce cache, key confusion between presented and recorded keys, and anything
  that lets an unenrolled peer through.
- Secret handling: secrets appearing in argv, logs, `String()`/`Debug` output,
  or API responses.
- Unexpected outbound network calls from a default install.
- File permissions on the database, `node.key`, or attachments.

**Known and documented, not a finding**

These are recorded decisions, explained in
[`docs/THREAT-MODEL.md`](docs/THREAT-MODEL.md) §6. A report that restates one
without new information will be closed with a pointer there — though an argument
that a decision is *wrong* is a legitimate issue, just not a vulnerability
report.

- The database is **not encrypted at rest**. Full-disk encryption is the correct
  layer.
- Sync payloads are **authenticated but not encrypted**. Confidentiality is
  delegated to the transport you choose.
- A relay or tunnel in the path is a **content-visible L7 hop**.
- **A full peer replicates the dataset.** Enrolment is trust.
- **Root on the box is game over.** PropFix does not defend against its own host.

## Legacy credential exposure

[`SECURITY-AUDIT.md`](SECURITY-AUDIT.md) catalogues credentials found in the
repository history of the legacy implementation, including items marked
unresolved. **Rebuilding the code does not rotate a key.** If you operate
anything referenced there, read it directly rather than assuming the rebuild
supersedes it.

## Verifying a release download

Every release attaches a `SHA256SUMS` manifest covering every published asset, and a
[sigstore build-provenance attestation](https://docs.github.com/actions/security-guides/using-artifact-attestations)
signed with a short-lived certificate minted from the release workflow's OIDC token. There
is no long-lived signing key: nothing to leak, nothing to rotate, nothing whose absence
quietly downgrades a check.

`install.sh` now performs that check itself. It fetches `SHA256SUMS`, looks up the **exact**
entry for your platform's binary, downloads it, and only then — if the digests match —
makes it executable and moves it onto your PATH. An earlier version of that script
downloaded, `chmod +x`'d and installed the binary having verified **nothing**, while the
release workflow published a checksums file that consequently nobody read. That is fixed.

To verify by hand instead, or to check the provenance signature as well:

```sh
curl -fsSLO https://raw.githubusercontent.com/vul-os/propfix/vX.Y.Z/scripts/verify.sh
bash verify.sh --tag vX.Y.Z --attest propfix-linux-amd64
```

Both scripts have two outcomes: verified, or a non-zero exit with a diagnostic naming what
was wrong. There is deliberately no `--skip-verify`, and **no path where a missing
`SHA256SUMS` means "nothing to check"** — a script that shrugs at a 404 prints a line that
looks like verification while checking nothing, which is worse than no check at all.

| Exit | Meaning |
|---|---|
| 0 | verified |
| 2 | usage error |
| 3 | `SHA256SUMS` unfetchable or absent |
| 4 | HTML page served where the manifest was expected |
| 5 | manifest empty or with no well-formed digest line |
| 6 | manifest has no entry for that asset |
| 7 | asset unfetchable, or HTML served where bytes were expected |
| 8 | truncated download |
| 9 | digest mismatch |
| 10 | `curl` or a digest tool missing |
| 11 | `--attest` requested and provenance did not verify (`verify.sh` only) |
| 12 | plaintext non-loopback origin refused |
| 13 | release version unresolvable and none pinned (`install.sh` only) |

Exit 13 is the no-fall-open guard: if the release API cannot be reached, `install.sh` will
not guess a tag or fall back to a branch. Pin one with `PROPFIX_VERSION=vX.Y.Z`.

Re-prove the refusals at any time — `bash scripts/verify.sh --selftest` (24 cases) and
`sh install.sh --selftest` (16 cases). Each stands up a synthetic origin broken in exactly
one way per route and asserts the exit code *and* that a diagnostic was printed. CI runs
both on every push, and the release workflow runs both again before publishing.

Binaries are **not** signed for macOS Gatekeeper or Windows SmartScreen. Build provenance
answers "did these bytes come from this repo's release workflow", which is a different
question from "does this OS trust the publisher".

## Disclosure

Coordinated disclosure. Please give us a reasonable window to ship a fix before
publishing. If a report is not acted on in reasonable time, publishing is your
call and we will not object to it.
