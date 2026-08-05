import { useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { jobsApi, buildingsApi, partiesApi, type JobStatus } from '../lib/api'
import { useAsync } from '../lib/useAsync'
import { nextStatuses, label } from '../lib/domain'
import { formatMoney, sumMinor, formatMinutes } from '../lib/money'
import { formatDateTime } from '../lib/format'
import { StatusPill, PriorityPill, CategoryTag } from '../components/JobBadges'
import Card, { CardHeader } from '../components/ui/Card'
import { Select } from '../components/ui/Field'
import Button from '../components/ui/Button'
import { ErrorState, LoadingState, InlineError } from '../components/ui/States'
import { ChevronLeftIcon } from '../components/icons'
import EventThread from '../components/job/EventThread'
import CostLedger from '../components/job/CostLedger'
import TimeLedger from '../components/job/TimeLedger'
import PhotoPicker from '../components/PhotoPicker'

type Tab = 'thread' | 'costs' | 'photos'

export default function JobDetailPage() {
  // This page only mounts under the "jobs/:id" route (see App.tsx), so the
  // param is always present.
  const { id } = useParams() as { id: string }
  const navigate = useNavigate()
  const [tab, setTab] = useState<Tab>('thread')
  const [photos, setPhotos] = useState<File[]>([])

  const jobQ = useAsync(() => jobsApi.get(id), [id])
  const eventsQ = useAsync(() => jobsApi.events(id), [id])
  const costsQ = useAsync(() => jobsApi.costs(id), [id])
  const timeQ = useAsync(() => jobsApi.time(id), [id])
  const partiesQ = useAsync(() => partiesApi.list(), [])

  const job = jobQ.data?.job
  const totals = jobQ.data?.totals

  const buildingQ = useAsync(() => (job ? buildingsApi.get(job.building_id) : Promise.resolve(null)), [job?.building_id])
  const unitsQ = useAsync(() => (job ? buildingsApi.units(job.building_id) : Promise.resolve([])), [job?.building_id])
  const unit = useMemo(() => (unitsQ.data || []).find((u) => u.id === job?.unit_id), [unitsQ.data, job?.unit_id])

  const parties = partiesQ.data || []

  const [statusBusy, setStatusBusy] = useState(false)
  const [statusError, setStatusError] = useState('')
  const [assignBusy, setAssignBusy] = useState(false)

  if (jobQ.loading) return <LoadingState label="Loading job…" />
  if (jobQ.error) return <ErrorState message={jobQ.error.message} onRetry={jobQ.reload} />
  if (!job) return null

  async function changeStatus(next: string) {
    if (!next || next === job!.status) return
    setStatusBusy(true)
    setStatusError('')
    try {
      const updated = await jobsApi.setStatus(id, { status: next as JobStatus, note: '', actor_party_id: '' })
      jobQ.setData((d) => ({ ...d, job: updated }))
    } catch (err) {
      setStatusError((err instanceof Error && err.message) || 'Could not change status.')
    } finally {
      setStatusBusy(false)
    }
  }

  async function changeAssignee(partyId: string) {
    setAssignBusy(true)
    try {
      const updated = await jobsApi.assign(id, partyId)
      jobQ.setData((d) => ({ ...d, job: updated }))
    } catch (err) {
      setStatusError((err instanceof Error && err.message) || 'Could not assign the job.')
    } finally {
      setAssignBusy(false)
    }
  }

  return (
    <div>
      <button
        type="button"
        // navigate() can return a Promise (react-router view-transition
        // support); void makes the discard explicit for
        // @typescript-eslint/no-misused-promises.
        onClick={() => void navigate('/jobs')}
        className="mb-3 flex items-center gap-1 text-xs font-medium text-ink-muted hover:text-ink"
      >
        <ChevronLeftIcon width={14} height={14} />
        Back to jobs
      </button>

      <div className="mb-5 flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="mb-1 flex items-center gap-2 text-xs text-ink-faint">
            <span className="tab-num">#{job.number}</span>
            {buildingQ.data && (
              <>
                <span>·</span>
                <Link to={`/buildings/${buildingQ.data.id}`} className="hover:text-accent">
                  {buildingQ.data.name}
                </Link>
              </>
            )}
            {unit && (
              <>
                <span>·</span>
                <span>{unit.label}</span>
              </>
            )}
          </div>
          <h1 className="text-2xl font-semibold tracking-tight text-ink">{job.title}</h1>
          {job.description && <p className="mt-1.5 max-w-2xl text-sm text-ink-muted">{job.description}</p>}
          <div className="mt-2.5 flex flex-wrap items-center gap-2">
            <StatusPill status={job.status} />
            <PriorityPill priority={job.priority} />
            <CategoryTag category={job.category} />
          </div>
        </div>

        <div className="w-64 shrink-0 rounded-md border border-line bg-surface-raised p-3">
          <InlineError message={statusError} />
          <label className="mb-1 block text-2xs font-medium text-ink-faint">Status</label>
          {/* changeStatus catches its own errors into statusError, rendered
              via InlineError above — this wrap only discards the resolved
              Promise<void>. */}
          <Select value={job.status} onChange={(e) => { void changeStatus(e.target.value) }} disabled={statusBusy} className="mb-3">
            <option value={job.status}>{label(job.status)} (current)</option>
            {nextStatuses(job.status).map((s) => (
              <option key={s} value={s}>
                {label(s)}
              </option>
            ))}
          </Select>

          <label className="mb-1 block text-2xs font-medium text-ink-faint">Assignee</label>
          {/* changeAssignee catches its own errors into statusError (shared
              with changeStatus above), rendered via InlineError — this wrap
              only discards the resolved Promise<void>. */}
          <Select value={job.assignee_party_id || ''} onChange={(e) => { void changeAssignee(e.target.value) }} disabled={assignBusy}>
            <option value="">Unassigned</option>
            {parties.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name} ({p.kind})
              </option>
            ))}
          </Select>

          <div className="mt-3 grid grid-cols-2 gap-2 border-t border-line pt-3 text-center">
            <div>
              <p className="tab-num text-sm font-semibold text-ink">
                {formatMoney(totals?.cost_minor ?? sumMinor(costsQ.data || []), costsQ.data?.[0]?.currency)}
              </p>
              <p className="text-2xs text-ink-faint">Cost</p>
            </div>
            <div>
              <p className="tab-num text-sm font-semibold text-ink">
                {formatMinutes(totals?.minutes ?? (timeQ.data || []).reduce((s, t) => s + t.minutes, 0))}
              </p>
              <p className="text-2xs text-ink-faint">Time</p>
            </div>
          </div>

          <p className="mt-3 border-t border-line pt-2 text-2xs text-ink-faint">
            Opened {formatDateTime(job.opened_at)}
            {job.closed_at ? ` · Closed ${formatDateTime(job.closed_at)}` : ''}
          </p>
        </div>
      </div>

      <div className="mb-4 flex gap-1 border-b border-line">
        {(
          [
            { key: 'thread', label: 'Event thread' },
            { key: 'costs', label: 'Costs & time' },
            { key: 'photos', label: 'Photos' },
          ] as { key: Tab; label: string }[]
        ).map((t) => (
          <button
            key={t.key}
            type="button"
            onClick={() => setTab(t.key)}
            className={`-mb-px border-b-2 px-3 py-2 text-sm font-medium transition-colors ${
              tab === t.key ? 'border-accent text-ink' : 'border-transparent text-ink-muted hover:text-ink'
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {tab === 'thread' &&
        (eventsQ.error ? (
          <ErrorState message={eventsQ.error.message} onRetry={eventsQ.reload} />
        ) : eventsQ.loading ? (
          <LoadingState label="Loading the thread…" />
        ) : (
          <EventThread
            jobId={id}
            events={eventsQ.data || []}
            parties={parties}
            onPosted={(ev) => eventsQ.setData((d) => [...(d || []), ev])}
          />
        ))}

      {tab === 'costs' && (
        <div className="grid gap-6 md:grid-cols-2">
          <Card className="p-4">
            {costsQ.error ? (
              <ErrorState message={costsQ.error.message} onRetry={costsQ.reload} />
            ) : costsQ.loading ? (
              <LoadingState />
            ) : (
              <CostLedger
                jobId={id}
                costs={costsQ.data || []}
                parties={parties}
                onAdded={(c) => costsQ.setData((d) => [...(d || []), c])}
              />
            )}
          </Card>
          <Card className="p-4">
            {timeQ.error ? (
              <ErrorState message={timeQ.error.message} onRetry={timeQ.reload} />
            ) : timeQ.loading ? (
              <LoadingState />
            ) : (
              <TimeLedger
                jobId={id}
                entries={timeQ.data || []}
                parties={parties}
                onAdded={(t) => timeQ.setData((d) => [...(d || []), t])}
              />
            )}
          </Card>
        </div>
      )}

      {tab === 'photos' && (
        <Card className="p-4">
          <CardHeader title="Attachments" subtitle="Photo evidence for this job" />
          <div className="pt-3">
            <PhotoPicker files={photos} onChange={setPhotos} />
          </div>
        </Card>
      )}
    </div>
  )
}
