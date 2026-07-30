import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { jobsApi, buildingsApi, partiesApi } from '../lib/api.js'
import { useAsync } from '../lib/useAsync.js'
import { useUnitsIndex } from '../lib/useUnitsIndex.js'
import { BOARD_COLUMNS, JOB_STATUSES, JOB_PRIORITIES, label } from '../lib/domain.js'
import { StatusPill, PriorityPill, CategoryTag } from '../components/JobBadges.jsx'
import { Select, Input } from '../components/ui/Field.jsx'
import Button from '../components/ui/Button.jsx'
import Card from '../components/ui/Card.jsx'
import { EmptyState, ErrorState, Skeleton } from '../components/ui/States.jsx'
import { WrenchIcon, PlusIcon, SearchIcon } from '../components/icons.jsx'

export default function JobsBoardPage() {
  const [view, setView] = useState('board') // board | list
  const [buildingId, setBuildingId] = useState('')
  const [unitId, setUnitId] = useState('')
  const [status, setStatus] = useState('')
  const [priority, setPriority] = useState('')
  const [assignee, setAssignee] = useState('')
  const [q, setQ] = useState('')

  const buildings = useAsync(() => buildingsApi.list(), [])
  const parties = useAsync(() => partiesApi.list(), [])
  const jobs = useAsync(
    () => jobsApi.list({ building_id: buildingId || undefined, unit_id: unitId || undefined, status: view === 'list' ? status || undefined : undefined }),
    [buildingId, unitId, status, view],
  )

  const units = useAsync(() => (buildingId ? buildingsApi.units(buildingId) : Promise.resolve([])), [buildingId])

  const buildingMap = useMemo(() => new Map((buildings.data || []).map((b) => [b.id, b])), [buildings.data])
  const partyMap = useMemo(() => new Map((parties.data || []).map((p) => [p.id, p])), [parties.data])
  const allBuildingIds = useMemo(() => (jobs.data || []).map((j) => j.building_id), [jobs.data])
  const unitsIndex = useUnitsIndex(allBuildingIds)

  const filtered = useMemo(() => {
    let list = jobs.data || []
    if (priority) list = list.filter((j) => j.priority === priority)
    if (assignee) list = list.filter((j) => j.assignee_party_id === assignee)
    if (q.trim()) {
      const needle = q.trim().toLowerCase()
      list = list.filter(
        (j) =>
          j.title.toLowerCase().includes(needle) ||
          (j.description || '').toLowerCase().includes(needle) ||
          String(j.number).includes(needle),
      )
    }
    return list
  }, [jobs.data, priority, assignee, q])

  const loading = jobs.loading || buildings.loading
  const anyFilterActive = buildingId || unitId || status || priority || assignee || q

  function resetFilters() {
    setBuildingId('')
    setUnitId('')
    setStatus('')
    setPriority('')
    setAssignee('')
    setQ('')
  }

  return (
    <div>
      <header className="mb-5 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Jobs</h1>
          <p className="mt-0.5 text-sm text-ink-muted">Maintenance work across every building.</p>
        </div>
        <div className="flex items-center gap-2">
          <div className="flex rounded-sm border border-line bg-surface-raised p-0.5">
            <button
              type="button"
              onClick={() => setView('board')}
              className={`rounded-xs px-3 py-1 text-xs font-medium transition-colors ${view === 'board' ? 'bg-accent-tint text-accent-ink' : 'text-ink-muted hover:text-ink'}`}
            >
              Board
            </button>
            <button
              type="button"
              onClick={() => setView('list')}
              className={`rounded-xs px-3 py-1 text-xs font-medium transition-colors ${view === 'list' ? 'bg-accent-tint text-accent-ink' : 'text-ink-muted hover:text-ink'}`}
            >
              List
            </button>
          </div>
          <Link to="/jobs/new">
            <Button variant="primary">
              <PlusIcon width={16} height={16} />
              Raise a job
            </Button>
          </Link>
        </div>
      </header>

      <Card className="mb-5 flex flex-wrap items-end gap-3 p-3">
        <div className="min-w-[160px] flex-1">
          <label className="mb-1 block text-2xs font-medium text-ink-faint">Search</label>
          <div className="relative">
            <SearchIcon width={15} height={15} className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-ink-faint" />
            <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Title, description, job #" className="pl-8" />
          </div>
        </div>
        <div className="w-44">
          <label className="mb-1 block text-2xs font-medium text-ink-faint">Building</label>
          <Select
            value={buildingId}
            onChange={(e) => {
              setBuildingId(e.target.value)
              setUnitId('')
            }}
          >
            <option value="">All buildings</option>
            {(buildings.data || []).map((b) => (
              <option key={b.id} value={b.id}>
                {b.name}
              </option>
            ))}
          </Select>
        </div>
        <div className="w-40">
          <label className="mb-1 block text-2xs font-medium text-ink-faint">Unit</label>
          <Select value={unitId} onChange={(e) => setUnitId(e.target.value)} disabled={!buildingId}>
            <option value="">All units</option>
            {(units.data || []).map((u) => (
              <option key={u.id} value={u.id}>
                {u.label}
              </option>
            ))}
          </Select>
        </div>
        {view === 'list' && (
          <div className="w-40">
            <label className="mb-1 block text-2xs font-medium text-ink-faint">Status</label>
            <Select value={status} onChange={(e) => setStatus(e.target.value)}>
              <option value="">All statuses</option>
              {JOB_STATUSES.map((s) => (
                <option key={s} value={s}>
                  {label(s)}
                </option>
              ))}
            </Select>
          </div>
        )}
        <div className="w-36">
          <label className="mb-1 block text-2xs font-medium text-ink-faint">Priority</label>
          <Select value={priority} onChange={(e) => setPriority(e.target.value)}>
            <option value="">All priorities</option>
            {JOB_PRIORITIES.map((p) => (
              <option key={p} value={p}>
                {label(p)}
              </option>
            ))}
          </Select>
        </div>
        <div className="w-44">
          <label className="mb-1 block text-2xs font-medium text-ink-faint">Assignee</label>
          <Select value={assignee} onChange={(e) => setAssignee(e.target.value)}>
            <option value="">Anyone</option>
            {(parties.data || []).map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </Select>
        </div>
        {anyFilterActive && (
          <Button variant="ghost" size="sm" onClick={resetFilters}>
            Clear filters
          </Button>
        )}
      </Card>

      {jobs.error ? (
        <ErrorState message={jobs.error.message} onRetry={jobs.reload} />
      ) : loading ? (
        <BoardSkeleton />
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={<WrenchIcon width={28} height={28} />}
          title={anyFilterActive ? 'No jobs match these filters' : 'No jobs yet'}
          description={
            anyFilterActive
              ? 'Try widening the filters above.'
              : 'Raise the first job against a building and unit to get started.'
          }
          action={
            anyFilterActive ? (
              <Button variant="secondary" onClick={resetFilters}>
                Clear filters
              </Button>
            ) : (
              <Link to="/jobs/new">
                <Button variant="primary">Raise a job</Button>
              </Link>
            )
          }
        />
      ) : view === 'board' ? (
        <BoardView jobs={filtered} buildingMap={buildingMap} unitsIndex={unitsIndex} partyMap={partyMap} />
      ) : (
        <ListView jobs={filtered} buildingMap={buildingMap} unitsIndex={unitsIndex} partyMap={partyMap} />
      )}
    </div>
  )
}

function JobCard({ job, buildingMap, unitsIndex, partyMap }) {
  const building = buildingMap.get(job.building_id)
  const unit = unitsIndex.get(job.unit_id)
  const assignee = partyMap.get(job.assignee_party_id)
  return (
    <Link
      to={`/jobs/${job.id}`}
      className="block rounded-sm border border-line bg-surface-raised p-3 shadow-e1 transition-shadow hover:shadow-e2"
    >
      <div className="mb-1.5 flex items-center justify-between">
        <span className="tab-num text-2xs text-ink-faint">#{job.number}</span>
        <PriorityPill priority={job.priority} />
      </div>
      <p className="mb-1.5 text-sm font-medium leading-snug text-ink">{job.title}</p>
      <p className="mb-2 truncate text-xs text-ink-muted">
        {building?.name || '—'}
        {unit ? ` · ${unit.label}` : ''}
      </p>
      <div className="flex items-center justify-between">
        <CategoryTag category={job.category} />
        {assignee ? (
          <span className="flex h-5 w-5 items-center justify-center rounded-full bg-surface-sunk text-2xs font-semibold text-ink-muted" title={assignee.name}>
            {assignee.name[0]?.toUpperCase()}
          </span>
        ) : (
          <span className="text-2xs text-ink-faint">Unassigned</span>
        )}
      </div>
    </Link>
  )
}

function BoardView({ jobs, buildingMap, unitsIndex, partyMap }) {
  return (
    <div className="flex gap-3 overflow-x-auto pb-2">
      {BOARD_COLUMNS.map((col) => {
        const inColumn = jobs.filter((j) => j.status === col.status)
        return (
          <div key={col.status} className="w-64 shrink-0">
            <div className="mb-2 flex items-center justify-between px-1">
              <h3 className="text-xs font-semibold uppercase tracking-wide text-ink-muted">{col.label}</h3>
              <span className="tab-num text-2xs text-ink-faint">{inColumn.length}</span>
            </div>
            <div className="flex min-h-[60px] flex-col gap-2 rounded-md bg-surface-sunk/60 p-1.5">
              {inColumn.length === 0 ? (
                <p className="px-2 py-3 text-center text-2xs text-ink-faint">Empty</p>
              ) : (
                inColumn.map((j) => (
                  <JobCard key={j.id} job={j} buildingMap={buildingMap} unitsIndex={unitsIndex} partyMap={partyMap} />
                ))
              )}
            </div>
          </div>
        )
      })}
    </div>
  )
}

function ListView({ jobs, buildingMap, unitsIndex, partyMap }) {
  return (
    <Card className="overflow-hidden">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-line bg-surface-sunk text-left text-2xs uppercase tracking-wide text-ink-faint">
            <th className="px-3 py-2 font-medium">#</th>
            <th className="px-3 py-2 font-medium">Job</th>
            <th className="px-3 py-2 font-medium">Building / unit</th>
            <th className="px-3 py-2 font-medium">Status</th>
            <th className="px-3 py-2 font-medium">Priority</th>
            <th className="px-3 py-2 font-medium">Assignee</th>
          </tr>
        </thead>
        <tbody>
          {jobs.map((j) => {
            const building = buildingMap.get(j.building_id)
            const unit = unitsIndex.get(j.unit_id)
            const assignee = partyMap.get(j.assignee_party_id)
            return (
              <tr key={j.id} className="border-b border-line last:border-0 hover:bg-surface-sunk/60">
                <td className="px-3 py-2">
                  <Link to={`/jobs/${j.id}`} className="tab-num text-xs text-ink-muted hover:text-accent">
                    #{j.number}
                  </Link>
                </td>
                <td className="max-w-[280px] px-3 py-2">
                  <Link to={`/jobs/${j.id}`} className="font-medium text-ink hover:text-accent">
                    {j.title}
                  </Link>
                  <div className="mt-0.5">
                    <CategoryTag category={j.category} />
                  </div>
                </td>
                <td className="px-3 py-2 text-ink-muted">
                  {building?.name || '—'}
                  {unit ? ` · ${unit.label}` : ''}
                </td>
                <td className="px-3 py-2">
                  <StatusPill status={j.status} />
                </td>
                <td className="px-3 py-2">
                  <PriorityPill priority={j.priority} />
                </td>
                <td className="px-3 py-2 text-ink-muted">{assignee?.name || '—'}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </Card>
  )
}

function BoardSkeleton() {
  return (
    <div className="flex gap-3">
      {Array.from({ length: 4 }).map((_, i) => (
        <div key={i} className="w-64 shrink-0 space-y-2">
          <Skeleton className="h-4 w-24" />
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-20 w-full" />
        </div>
      ))}
    </div>
  )
}
