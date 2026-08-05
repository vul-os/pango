import { useState, type FormEvent } from 'react'
import { jobsApi, type JobTimeEntry, type Party } from '../../lib/api'
import { formatMinutes } from '../../lib/money'
import { formatDate } from '../../lib/format'
import { Input, Select } from '../ui/Field'
import Button from '../ui/Button'
import { InlineError, EmptyState } from '../ui/States'

interface TimeLedgerProps {
  jobId: string
  entries: JobTimeEntry[]
  parties: Party[]
  onAdded: (entry: JobTimeEntry) => void
}

export default function TimeLedger({ jobId, entries, parties, onAdded }: TimeLedgerProps) {
  const [open, setOpen] = useState(false)
  const [minutes, setMinutes] = useState('')
  const [note, setNote] = useState('')
  const [partyId, setPartyId] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const total = entries.reduce((sum, e) => sum + (Number(e.minutes) || 0), 0)

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const n = Number(minutes)
    if (!Number.isFinite(n) || !Number.isInteger(n) || n === 0) {
      setError('Enter a whole number of minutes — use a negative number for a correction.')
      return
    }
    setBusy(true)
    setError('')
    try {
      const created = await jobsApi.addTime(jobId, { minutes: n, note: note.trim(), party_id: partyId })
      onAdded(created)
      setMinutes('')
      setNote('')
      setOpen(false)
    } catch (err) {
      setError((err instanceof Error && err.message) || 'Could not add the time entry.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div>
      <div className="mb-3 flex items-center justify-between">
        <div>
          <h3 className="text-sm font-semibold text-ink">Time</h3>
          <p className="tab-num text-xs text-ink-muted">{formatMinutes(total)} total</p>
        </div>
        <Button variant="secondary" size="sm" onClick={() => setOpen((v) => !v)}>
          {open ? 'Cancel' : 'Log time'}
        </Button>
      </div>

      {open && (
        // submit catches its own errors into `error`, rendered via
        // InlineError below — this wrap only discards the resolved
        // Promise<void>.
        <form onSubmit={(e) => { void submit(e) }} className="mb-3 rounded-md border border-line bg-surface-sunk/60 p-3">
          <InlineError message={error} />
          <div className="mb-2 flex gap-2">
            <Input
              value={minutes}
              onChange={(e) => setMinutes(e.target.value)}
              placeholder="Minutes, e.g. 45"
              inputMode="numeric"
              className="w-32 bg-surface-raised"
            />
            <Select value={partyId} onChange={(e) => setPartyId(e.target.value)} className="flex-1 bg-surface-raised">
              <option value="">No party</option>
              {parties.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </Select>
          </div>
          <Input
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="What was done"
            className="mb-2 bg-surface-raised"
          />
          <Button type="submit" variant="primary" size="sm" disabled={busy}>
            {busy ? 'Adding…' : 'Add entry'}
          </Button>
        </form>
      )}

      {entries.length === 0 ? (
        <EmptyState title="No time logged" description="Log minutes as work happens — entries are append-only." />
      ) : (
        <ul className="divide-y divide-line rounded-md border border-line">
          {entries.map((t) => (
            <li key={t.id} className="flex items-center justify-between gap-3 px-3 py-2">
              <div className="min-w-0">
                <p className="truncate text-sm text-ink">{t.note || 'Time logged'}</p>
                <p className="text-2xs text-ink-faint">{formatDate(t.created_at)}</p>
              </div>
              <span className={`tab-num shrink-0 text-sm font-medium ${t.minutes < 0 ? 'text-critical' : 'text-ink'}`}>
                {formatMinutes(t.minutes)}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
