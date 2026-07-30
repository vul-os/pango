# Security audit — repository merge

Record of the secret scan performed when four Pango repositories were merged
into this one, and what was removed. Dated 2026-07-20.

## ⚠️ Action required: revoke these credentials

**Stripping secrets from this repository does not un-expose them.** The
original repositories still exist on GitHub with full history, and anything
committed there must be treated as compromised regardless of what was done
here. Cloning, forking, and GitHub's own caches mean the material may persist
even after the source repositories are deleted.

Two Google Cloud **service-account private keys** were committed in plaintext
and were present at `HEAD` of `pango-backend-go`:

| File | GCP project | Private key ID |
|---|---|---|
| `keyfile.json` | `pango` | `d24455062861d89d9e5753eeca9c483a46399c27` |
| `firebase-keyfile.json` | `prop-fix` | `dbf18a7268b14ee19e3d6642ed05ad84a145d673` |

These grant whatever IAM roles were bound to those service accounts. They must
be **revoked in the Google Cloud console**, not merely rotated in code:

1. IAM & Admin → Service Accounts → locate the account in each project.
2. Keys tab → delete the key with the ID above.
3. Review audit logs for use by an unrecognised principal or from an
   unexpected address, covering the period since first commit (2023-07-26).
4. Check for resources created by these accounts — Firestore/Storage writes,
   Cloud Build triggers, billable API usage.

If the projects are dormant or abandoned, deleting the service accounts (or
the projects) is simpler and stronger than key rotation.

### Mailgun API key and Neon Postgres credentials

Found by GitHub push protection after the first rewrite passes had already been
run — see *Limitations* below, because this is the most important lesson in
this document. All were in `backend/internal/server/server.go`, hardcoded in
`Server()`, across at least five commits.

| Credential | Detail | Action |
|---|---|---|
| **Mailgun API key** | prefix `787a7e4f…`, domain `mail.pango.co`, sender `noreply@mail.pango.co` | Revoke in Mailgun → Sending → API keys. Check sending logs for abuse; a leaked key sends mail as your domain and burns its reputation. |
| **Neon Postgres** | host `ep-autumn-math-44120355.us-east-2.aws.neon.tech`, database `neondb`, user `exolutiontech`, **two** distinct passwords over time | Rotate the role password or delete the Neon project. Check for unexpected connections. |

A leaked mail-sending key is worth treating as urgently as a database
credential: it allows sending authenticated mail as `pango.co`, which means
phishing under your own domain and lasting deliverability damage.

### Supabase project (separate system — not covered by GCP deletion)

A second-pass scan for JWT-shaped credentials found a Supabase project
referenced in the frontend history:

| Project ref | URL | Key role | Issued | Expires |
|---|---|---|---|---|
| `tcgmonunzroeujvmqcir` | `https://tcgmonunzroeujvmqcir.supabase.co` | `anon` | 2023-07-04 | 2033-07-04 |

No `service_role` key is present anywhere in the history — only the `anon` key,
which is public by design and shipped in client bundles.

**However**, an `anon` key is only safe when Row Level Security is enabled and
correct on every table. Without RLS it grants full read and write access to the
database. Verify RLS on project `tcgmonunzroeujvmqcir`, or delete/pause the
project if it is no longer needed. Deleting the GCP projects does **not** affect
this — Supabase is a separate provider.

## Credentials that were never committed

These live in service settings rather than git history, and survive repository
deletion:

- **GitHub Actions secrets** — three `firebase-hosting-*` workflows in the
  frontend consume a `FIREBASE_SERVICE_ACCOUNT_*` repository secret.
- **Cloud Build** — `backend/cloudbuild.yaml`; check substitutions and any
  Secret Manager references.
- **Gitpod** — `.gitpod.yml` in backend and frontend; Gitpod stores workspace
  environment variables per user and repository.
- **Deploy keys and webhooks** on the four source repositories.

## Unresolved

Three distinct Firebase Web API keys were found but only two GCP projects were
identified. Either a third project exists, or one of the keys is a Google
**Maps** key — an unrestricted Maps key is directly billable by anyone who
finds it. The third key (prefix `AIzaSyB0GV…`, suffix `…p1_4I`) appeared only
once and should be traced to its project. It is fingerprinted rather than
quoted here: this file is published, and reproducing a possibly-live key in it
would recreate the exposure the document exists to record.

## What was removed from history

**Service-account keyfiles** — `keyfile.json` and `firebase-keyfile.json`
removed from every commit in `pango-backend-go` via `git filter-repo`
(3 blob versions across 297 commits).

**Firebase Web API keys** — three `AIza…` values replaced with
`***REMOVED-FIREBASE-WEB-KEY***` throughout `pango-frontend-react` history
(16 blobs affected), last seen in `src/contexts/auth.js`.

These are a lower severity: Firebase Web API keys are *public identifiers by
design*, shipped in client bundles and secured by Firebase Security Rules and
API key restrictions rather than by secrecy. They were scrubbed for hygiene.
The meaningful control is that Firebase Security Rules are correctly
restrictive and the keys are domain-restricted — **verify this**, because a
project that relied on the key being unknown is misconfigured.

If any of the three was a Google **Maps** key rather than a Firebase key, an
unrestricted Maps key is directly billable by anyone who finds it and should be
restricted or rotated.

## What was scanned

All reachable blobs under 2 MB across the complete history of all four
repositories, before and after the merge, for:

private keys (`BEGIN … PRIVATE KEY`), GCP service-account JSON, AWS access key
IDs (`AKIA…`), Google API keys (`AIza…`), Stripe live and test keys, GitHub
personal access tokens (`ghp_`, `github_pat_`), Slack tokens (`xox…`),
SendGrid keys (`SG.…`), and Postgres/MongoDB connection strings containing
inline credentials. Filenames were separately matched against `.env`, `*.pem`,
`*.key`, `*.p12`, `*.pfx`, `*keyfile*`, `*credential*`, `id_rsa`, `*.jks`.

A lower-signal pass over the Go backend for hardcoded assignments to
`jwt_secret`, `api_key`, `password`, `secret_key`, `access_token` and
`client_secret` returned nothing.

## Verification

After the merge, the same scan was re-run against the unified history and
returned no matches on any pattern, and no suspicious filenames. The scrub
markers are present, confirming replacement rather than accidental omission.

## Limitations

Stated so the clean result is not over-read:

- **Pattern-based scanning missed real secrets here — three times.** This is
  documented rather than glossed, because it is the practical lesson:
  1. The **Supabase anon JWT** survived the first rewrite: it was found by a
     later scan for JWT shapes, but the rewrite had only been given the
     Google API keys.
  2. The **Mailgun API key** was missed because the pattern assumed a `key-`
     prefix, which Mailgun does not always use.
  3. The **Neon Postgres password** was missed because the pattern matched
     URI-style `postgres://user:pass@host` and the code used libpq keyword
     form, `user=… password=…`.

  The last two were caught only by **GitHub push protection**, which rejected
  the push. Do not treat the clean result of a hand-rolled scan as evidence of
  absence. No commercial scanner (`gitleaks`, `trufflehog`) was available in
  this environment; run one before treating this repository as audited, and
  leave GitHub secret scanning and push protection enabled.
- **Blobs over 2 MB were skipped** for performance.
- **Only reachable objects were scanned.** Anything unreferenced was excluded
  and is in any case dropped by the rewrite.
- **This audit covers the four merged repositories only** — not deployment
  environments, CI secrets, or any credential that was never committed.
