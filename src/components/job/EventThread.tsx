import { useState, type FormEvent } from 'react'
import { jobsApi, type EventVisibility, type JobEvent, type Party } from '../../lib/api'
import { formatDateTime } from '../../lib/format'
import { Select, Textarea } from '../ui/Field'
import Button from '../ui/Button'
import Pill from '../ui/Pill'
import { InlineError, EmptyState } from '../ui/States'

const KINDS = ['note', 'status', 'assignment', 'access', 'other']

interface EventThreadProps {
  jobId: string
  events: JobEvent[]
  parties: Party[]
  onPosted: (event: JobEvent) => void
}

export default function EventThread({ jobId, events, parties, onPosted }: EventThreadProps) {
  const [filter, setFilter] = useState<'all' | 'public'>('all')
  const [body, setBody] = useState('')
  const [kind, setKind] = useState('note')
  const [visibility, setVisibility] = useState<EventVisibility>('internal')
  const [actorId, setActorId] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const visible = filter === 'public' ? events.filter((e) => e.visibility === 'public') : events
  const sorted = [...visible].sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime())
  const partyMap = new Map(parties.map((p) => [p.id, p]))

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!body.trim()) {
      setError('Write something before posting.')
      return
    }
    setBusy(true)
    setError('')
    try {
      const created = await jobsApi.addEvent(jobId, {
        kind,
        body: body.trim(),
        visibility,
        actor_party_id: actorId,
      })
      onPosted(created)
      setBody('')
    } catch (err) {
      setError((err instanceof Error && err.message) || 'Could not post the update.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div>
      <div className="mb-3 flex items-center justify-between">
        <h3 className="text-sm font-semibold text-ink">Event thread</h3>
        <div className="flex rounded-sm border border-line bg-surface-raised p-0.5 text-2xs">
          <button
            type="button"
            onClick={() => setFilter('all')}
            className={`rounded-xs px-2 py-1 font-medium ${filter === 'all' ? 'bg-accent-tint text-accent-ink' : 'text-ink-muted'}`}
          >
            All ({events.length})
          </button>
          <button
            type="button"
            onClick={() => setFilter('public')}
            className={`rounded-xs px-2 py-1 font-medium ${filter === 'public' ? 'bg-accent-tint text-accent-ink' : 'text-ink-muted'}`}
          >
            Tenant-visible ({events.filter((e) => e.visibility === 'public').length})
          </button>
        </div>
      </div>

      {sorted.length === 0 ? (
        <EmptyState
          title={filter === 'public' ? 'No tenant-visible updates yet' : 'No updates yet'}
          description="Post the first update below."
        />
      ) : (
        <ol className="mb-4 flex flex-col gap-2.5">
          {sorted.map((ev) => {
            const actor = partyMap.get(ev.actor_party_id)
            return (
              <li key={ev.id} className="rounded-sm border border-line bg-surface-raised p-3">
                <div className="mb-1 flex flex-wrap items-center justify-between gap-2">
                  <div className="flex items-center gap-2 text-xs text-ink-muted">
                    <span className="font-medium text-ink">{actor?.name || 'Unattributed'}</span>
                    <span className="text-ink-faint">·</span>
                    <span className="capitalize">{ev.kind}</span>
                  </div>
                  <div className="flex items-center gap-2">
                    <Pill tone={ev.visibility === 'public' ? 'info' : 'neutral'}>
                      {ev.visibility === 'public' ? 'Tenant-visible' : 'Internal'}
                    </Pill>
                    <span className="tab-num text-2xs text-ink-faint">{formatDateTime(ev.created_at)}</span>
                  </div>
                </div>
                <p className="whitespace-pre-wrap text-sm text-ink">{ev.body}</p>
              </li>
            )
          })}
        </ol>
      )}

      {/* submit catches its own errors into `error`, rendered via
          InlineError below — this wrap only discards the resolved
          Promise<void>. */}
      <form onSubmit={(e) => { void submit(e) }} className="rounded-md border border-line bg-surface-sunk/60 p-3">
        <InlineError message={error} />
        <Textarea
          value={body}
          onChange={(e) => setBody(e.target.value)}
          placeholder="Add an update…"
          className="mb-2.5 bg-surface-raised"
          rows={3}
        />
        <div className="flex flex-wrap items-center gap-2">
          <Select value={kind} onChange={(e) => setKind(e.target.value)} className="w-32">
            {KINDS.map((k) => (
              <option key={k} value={k}>
                {k}
              </option>
            ))}
          </Select>

          <div
            role="group"
            aria-label="Visibility"
            className="flex rounded-sm border border-line bg-surface-raised p-0.5"
          >
            <button
              type="button"
              onClick={() => setVisibility('internal')}
              className={`rounded-xs px-2.5 py-1.5 text-xs font-medium transition-colors ${visibility === 'internal' ? 'bg-surface-sunk text-ink' : 'text-ink-muted'}`}
            >
              Internal only
            </button>
            <button
              type="button"
              onClick={() => setVisibility('public')}
              className={`rounded-xs px-2.5 py-1.5 text-xs font-medium transition-colors ${visibility === 'public' ? 'bg-accent-tint text-accent-ink' : 'text-ink-muted'}`}
            >
              Tenant-visible
            </button>
          </div>

          <Select value={actorId} onChange={(e) => setActorId(e.target.value)} className="w-44">
            <option value="">Attribute to…</option>
            {parties.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </Select>

          <Button type="submit" variant="primary" size="sm" disabled={busy} className="ml-auto">
            {busy ? 'Posting…' : 'Post update'}
          </Button>
        </div>
        {visibility === 'public' && (
          <p className="mt-2 text-2xs text-serious">
            This update will be visible to the tenant on this job — internal-only detail belongs above the toggle.
          </p>
        )}
      </form>
    </div>
  )
}
