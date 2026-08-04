import { useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { buildingsApi, jobsApi } from '../lib/api'
import { useAsync } from '../lib/useAsync'
import { StatusPill, PriorityPill } from '../components/JobBadges'
import Card, { CardHeader } from '../components/ui/Card'
import Button from '../components/ui/Button'
import { Input } from '../components/ui/Field'
import { EmptyState, ErrorState, LoadingState, InlineError } from '../components/ui/States'
import { ChevronLeftIcon, PlusIcon } from '../components/icons'

export default function BuildingDetailPage() {
  // This page only mounts under the "buildings/:id" route (see App.tsx), so
  // the param is always present — react-router's own types cannot express
  // that per-route guarantee.
  const { id } = useParams() as { id: string }
  const navigate = useNavigate()
  const buildingQ = useAsync(() => buildingsApi.get(id), [id])
  const unitsQ = useAsync(() => buildingsApi.units(id), [id])
  const jobsQ = useAsync(() => jobsApi.list({ building_id: id }), [id])
  const [unitLabel, setUnitLabel] = useState('')
  const [unitError, setUnitError] = useState('')
  const [unitBusy, setUnitBusy] = useState(false)

  if (buildingQ.loading) return <LoadingState label="Loading building…" />
  if (buildingQ.error) return <ErrorState message={buildingQ.error.message} onRetry={buildingQ.reload} />
  const b = buildingQ.data
  if (!b) return null

  async function addUnit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!unitLabel.trim()) return
    setUnitBusy(true)
    setUnitError('')
    try {
      const u = await buildingsApi.ensureUnit(id, unitLabel.trim())
      unitsQ.setData((d) => (d!.some((x) => x.id === u.id) ? d! : [...d!, u]))
      setUnitLabel('')
    } catch (err) {
      setUnitError((err instanceof Error && err.message) || 'Could not add the unit.')
    } finally {
      setUnitBusy(false)
    }
  }

  return (
    <div>
      <button
        type="button"
        onClick={() => navigate('/buildings')}
        className="mb-3 flex items-center gap-1 text-xs font-medium text-ink-muted hover:text-ink"
      >
        <ChevronLeftIcon width={14} height={14} />
        Back to buildings
      </button>

      <div className="mb-6 flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{b.name}</h1>
          <p className="mt-0.5 text-sm text-ink-muted">{b.address || 'No address on file'}</p>
          {b.lat != null && b.lon != null && (
            <p className="tab-num mt-1 text-xs text-ink-faint">
              {b.lat.toFixed(5)}, {b.lon.toFixed(5)}
            </p>
          )}
        </div>
        <Link to={`/jobs/new`}>
          <Button variant="secondary">
            <PlusIcon width={15} height={15} />
            Raise a job here
          </Button>
        </Link>
      </div>

      <div className="grid gap-6 lg:grid-cols-[320px_1fr]">
        <Card>
          <CardHeader title="Units" subtitle={`${(unitsQ.data || []).length} on record`} />
          <div className="p-4">
            <form onSubmit={addUnit} className="mb-3 flex gap-2">
              <Input
                value={unitLabel}
                onChange={(e) => setUnitLabel(e.target.value)}
                placeholder="Add a unit, e.g. Flat 3A"
                className="flex-1"
              />
              <Button type="submit" variant="secondary" disabled={unitBusy}>
                Add
              </Button>
            </form>
            <InlineError message={unitError} />

            {unitsQ.error ? (
              <ErrorState message={unitsQ.error.message} onRetry={unitsQ.reload} />
            ) : unitsQ.loading ? (
              <LoadingState />
            ) : (unitsQ.data || []).length === 0 ? (
              <EmptyState title="No units yet" description="Units are created here, or automatically the first time a job or inspection names one." />
            ) : (
              <ul className="divide-y divide-line">
                {unitsQ.data!.map((u) => (
                  <li key={u.id} className="flex items-center justify-between py-2">
                    <span className="text-sm text-ink">{u.label}</span>
                    <span className="tab-num rounded-xs bg-surface-sunk px-1.5 py-0.5 text-2xs text-ink-faint" title="Normalised key">
                      {u.key}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </Card>

        <Card>
          <CardHeader title="Jobs" subtitle={`${(jobsQ.data || []).length} against this building`} />
          <div className="p-4">
            {jobsQ.error ? (
              <ErrorState message={jobsQ.error.message} onRetry={jobsQ.reload} />
            ) : jobsQ.loading ? (
              <LoadingState />
            ) : (jobsQ.data || []).length === 0 ? (
              <EmptyState title="No jobs raised here yet" />
            ) : (
              <ul className="divide-y divide-line">
                {jobsQ.data!.map((j) => (
                  <li key={j.id}>
                    <Link to={`/jobs/${j.id}`} className="flex items-center justify-between gap-3 py-2.5 hover:text-accent">
                      <div className="min-w-0">
                        <span className="tab-num mr-2 text-2xs text-ink-faint">#{j.number}</span>
                        <span className="text-sm text-ink">{j.title}</span>
                      </div>
                      <div className="flex shrink-0 items-center gap-2">
                        <PriorityPill priority={j.priority} />
                        <StatusPill status={j.status} />
                      </div>
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </Card>
      </div>
    </div>
  )
}
