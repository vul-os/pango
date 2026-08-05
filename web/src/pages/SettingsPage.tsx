import { useState, type FormEvent } from 'react'
import { useAuth } from '../lib/auth-context'
import { partiesApi, peersApi, templatesApi } from '../lib/api'
import { useAsync } from '../lib/useAsync'
import { PARTY_KINDS, label, type PartyKind } from '../lib/domain'
import Card, { CardHeader } from '../components/ui/Card'
import Button from '../components/ui/Button'
import Pill from '../components/ui/Pill'
import { Input, Select } from '../components/ui/Field'
import { EmptyState, ErrorState, LoadingState, InlineError } from '../components/ui/States'
import { PlusIcon } from '../components/icons'

export default function SettingsPage() {
  const { org, user } = useAuth()

  return (
    <div className="flex flex-col gap-6">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>
        <p className="mt-0.5 text-sm text-ink-muted">Organisation, people, checklist templates and sync peers.</p>
      </header>

      <Card className="p-4">
        <CardHeader title="Organisation" />
        <div className="grid grid-cols-2 gap-4 pt-3 text-sm">
          <div>
            <p className="text-2xs font-medium uppercase tracking-wide text-ink-faint">Name</p>
            <p className="mt-0.5 text-ink">{org?.name}</p>
          </div>
          <div>
            <p className="text-2xs font-medium uppercase tracking-wide text-ink-faint">Signed in as</p>
            <p className="mt-0.5 text-ink">
              {user?.name} <span className="text-ink-faint">({user?.email})</span>
            </p>
          </div>
        </div>
        <p className="mt-3 text-2xs text-ink-faint">
          Registration on this node closed after the first account was created (docs/ARCHITECTURE.md — "no default
          open anything"). There is no API yet to invite additional login accounts or rename the organisation from
          here.
        </p>
      </Card>

      <PeopleSection />
      <TemplatesSection />
      <PeersSection />
    </div>
  )
}

function PeopleSection() {
  const partiesQ = useAsync(() => partiesApi.list(), [])
  const [name, setName] = useState('')
  const [kind, setKind] = useState<PartyKind>('staff')
  const [email, setEmail] = useState('')
  const [phone, setPhone] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!name.trim()) {
      setError('A name is required.')
      return
    }
    setBusy(true)
    setError('')
    try {
      const p = await partiesApi.create({ kind, name: name.trim(), email: email.trim(), phone: phone.trim(), pubkey: '' })
      partiesQ.setData((d) => [...(d || []), p])
      setName('')
      setEmail('')
      setPhone('')
    } catch (err) {
      setError((err instanceof Error && err.message) || 'Could not add that person.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card className="p-4">
      <CardHeader title="People" subtitle="Staff, contractors and tenants — assignable to jobs and inspections." />
      <div className="pt-3">
        {/* submit catches its own errors into `error`, rendered via
            InlineError below — this wrap only discards the resolved
            Promise<void>. */}
        <form onSubmit={(e) => { void submit(e) }} className="mb-4 flex flex-wrap items-end gap-2">
          <div className="w-28">
            <label className="mb-1 block text-2xs font-medium text-ink-faint">Kind</label>
            <Select value={kind} onChange={(e) => setKind(e.target.value as PartyKind)}>
              {PARTY_KINDS.map((k) => (
                <option key={k} value={k}>
                  {label(k)}
                </option>
              ))}
            </Select>
          </div>
          <div className="min-w-[140px] flex-1">
            <label className="mb-1 block text-2xs font-medium text-ink-faint">Name</label>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Full name" />
          </div>
          <div className="min-w-[140px] flex-1">
            <label className="mb-1 block text-2xs font-medium text-ink-faint">Email</label>
            <Input value={email} onChange={(e) => setEmail(e.target.value)} placeholder="optional" />
          </div>
          <div className="w-36">
            <label className="mb-1 block text-2xs font-medium text-ink-faint">Phone</label>
            <Input value={phone} onChange={(e) => setPhone(e.target.value)} placeholder="optional" />
          </div>
          <Button type="submit" variant="secondary" disabled={busy}>
            <PlusIcon width={15} height={15} />
            Add
          </Button>
        </form>
        <InlineError message={error} />

        {partiesQ.error ? (
          <ErrorState message={partiesQ.error.message} onRetry={partiesQ.reload} />
        ) : partiesQ.loading ? (
          <LoadingState />
        ) : (partiesQ.data || []).length === 0 ? (
          <EmptyState title="Nobody added yet" description="Add staff and contractors so jobs can be assigned." />
        ) : (
          <ul className="divide-y divide-line">
            {partiesQ.data!.map((p) => (
              <li key={p.id} className="flex items-center justify-between py-2 text-sm">
                <div>
                  <span className="font-medium text-ink">{p.name}</span>
                  {p.email && <span className="ml-2 text-xs text-ink-faint">{p.email}</span>}
                </div>
                <Pill tone="neutral">{label(p.kind)}</Pill>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Card>
  )
}

interface DraftItem {
  section: string
  label: string
  sort: number
}

function TemplatesSection() {
  const templatesQ = useAsync(() => templatesApi.list(), [])
  const [creating, setCreating] = useState(false)
  const [name, setName] = useState('')
  const [tkind, setTkind] = useState('tenancy')
  const [items, setItems] = useState<DraftItem[]>([{ section: 'General', label: '', sort: 1 }])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  function updateItem(i: number, patch: Partial<DraftItem>) {
    setItems((arr) => arr.map((it, idx) => (idx === i ? { ...it, ...patch } : it)))
  }
  function addRow() {
    setItems((arr) => [...arr, { section: arr[arr.length - 1]?.section || 'General', label: '', sort: arr.length + 1 }])
  }
  function removeRow(i: number) {
    setItems((arr) => arr.filter((_, idx) => idx !== i))
  }

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const cleanItems = items.filter((it) => it.label.trim())
    if (!name.trim() || cleanItems.length === 0) {
      setError('Give the template a name and at least one checklist item.')
      return
    }
    setBusy(true)
    setError('')
    try {
      const t = await templatesApi.create({
        name: name.trim(),
        kind: tkind,
        items: cleanItems.map((it, i) => ({ section: it.section.trim() || 'General', label: it.label.trim(), sort: i + 1 })),
      })
      templatesQ.setData((d) => [...(d || []), t])
      setCreating(false)
      setName('')
      setItems([{ section: 'General', label: '', sort: 1 }])
    } catch (err) {
      setError((err instanceof Error && err.message) || 'Could not create the template.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card className="p-4">
      <CardHeader
        title="Inspection templates"
        subtitle="Reusable checklists — set out sections and items once, run them on every inspection."
        actions={
          <Button variant="secondary" size="sm" onClick={() => setCreating((v) => !v)}>
            {creating ? 'Cancel' : 'New template'}
          </Button>
        }
      />
      <div className="pt-3">
        {creating && (
          // submit catches its own errors into `error`, rendered via
          // InlineError below — this wrap only discards the resolved
          // Promise<void>.
          <form onSubmit={(e) => { void submit(e) }} className="mb-4 rounded-md border border-line bg-surface-sunk/60 p-3">
            <InlineError message={error} />
            <div className="mb-2 flex gap-2">
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Template name" className="flex-1 bg-surface-raised" />
              <Input value={tkind} onChange={(e) => setTkind(e.target.value)} placeholder="Kind (e.g. tenancy)" className="w-40 bg-surface-raised" />
            </div>
            <div className="flex flex-col gap-1.5">
              {items.map((it, i) => (
                <div key={i} className="flex gap-1.5">
                  <Input
                    value={it.section}
                    onChange={(e) => updateItem(i, { section: e.target.value })}
                    placeholder="Section"
                    className="w-28 bg-surface-raised"
                  />
                  <Input
                    value={it.label}
                    onChange={(e) => updateItem(i, { label: e.target.value })}
                    placeholder="Checklist item"
                    className="flex-1 bg-surface-raised"
                  />
                  <Button type="button" variant="ghost" size="sm" onClick={() => removeRow(i)} disabled={items.length === 1}>
                    Remove
                  </Button>
                </div>
              ))}
            </div>
            <div className="mt-2 flex items-center justify-between">
              <Button type="button" variant="ghost" size="sm" onClick={addRow}>
                + Add item
              </Button>
              <Button type="submit" variant="primary" size="sm" disabled={busy}>
                {busy ? 'Creating…' : 'Create template'}
              </Button>
            </div>
          </form>
        )}

        {templatesQ.error ? (
          <ErrorState message={templatesQ.error.message} onRetry={templatesQ.reload} />
        ) : templatesQ.loading ? (
          <LoadingState />
        ) : (templatesQ.data || []).length === 0 ? (
          <EmptyState title="No templates yet" description="Create one so inspections have a checklist to run." />
        ) : (
          <ul className="divide-y divide-line">
            {templatesQ.data!.map((t) => (
              <li key={t.id} className="flex items-center justify-between py-2 text-sm">
                <span className="font-medium text-ink">{t.name}</span>
                <span className="text-xs text-ink-faint">{t.kind}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Card>
  )
}

function PeersSection() {
  const peersQ = useAsync(() => peersApi.list(), [])
  const [name, setName] = useState('')
  const [url, setUrl] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!name.trim() || !url.trim()) {
      setError('A peer needs a name and a URL.')
      return
    }
    setBusy(true)
    setError('')
    try {
      const p = await peersApi.save({ id: '', name: name.trim(), url: url.trim(), pubkey: '', enabled: true })
      peersQ.setData((d) => [...(d || []), p])
      setName('')
      setUrl('')
    } catch (err) {
      setError((err instanceof Error && err.message) || 'Could not save that peer.')
    } finally {
      setBusy(false)
    }
  }

  async function remove(id: string) {
    try {
      await peersApi.remove(id)
      peersQ.setData((d) => (d || []).filter((p) => p.id !== id))
    } catch {
      // deletion failing is surfaced via the list simply not shrinking; a
      // full retry affordance is not worth the weight here.
    }
  }

  return (
    <Card className="p-4">
      <CardHeader title="Sync peers" subtitle="Manually enrolled nodes this org syncs its oplog with directly — no hub, no control plane." />
      <div className="pt-3">
        {/* submit catches its own errors into `error`, rendered via
            InlineError below — this wrap only discards the resolved
            Promise<void>. */}
        <form onSubmit={(e) => { void submit(e) }} className="mb-4 flex flex-wrap items-end gap-2">
          <div className="min-w-[140px] flex-1">
            <label className="mb-1 block text-2xs font-medium text-ink-faint">Name</label>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Branch office" />
          </div>
          <div className="min-w-[200px] flex-[2]">
            <label className="mb-1 block text-2xs font-medium text-ink-faint">URL</label>
            <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://peer.example:8080" />
          </div>
          <Button type="submit" variant="secondary" disabled={busy}>
            <PlusIcon width={15} height={15} />
            Enroll
          </Button>
        </form>
        <InlineError message={error} />

        {peersQ.error ? (
          <ErrorState message={peersQ.error.message} onRetry={peersQ.reload} />
        ) : peersQ.loading ? (
          <LoadingState />
        ) : (peersQ.data || []).length === 0 ? (
          <EmptyState title="No peers enrolled" description="Sync is opt-in and manual — nothing talks to another node until you enroll one." />
        ) : (
          <ul className="divide-y divide-line">
            {peersQ.data!.map((p) => (
              <li key={p.id} className="flex items-center justify-between py-2 text-sm">
                <div>
                  <span className="font-medium text-ink">{p.name}</span>
                  <span className="ml-2 text-xs text-ink-faint">{p.url}</span>
                </div>
                <div className="flex items-center gap-2">
                  <Pill tone={p.enabled ? 'good' : 'neutral-muted'}>{p.enabled ? 'Enabled' : 'Disabled'}</Pill>
                  {/* remove() already swallows its own failure deliberately
                      (see the comment on its definition above) — pre-existing
                      behaviour, unchanged here. This wrap only discards the
                      resolved Promise<void> for
                      @typescript-eslint/no-misused-promises. */}
                  <button type="button" onClick={() => { void remove(p.id) }} className="text-xs text-critical hover:underline">
                    Remove
                  </button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Card>
  )
}
