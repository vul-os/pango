import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { reportsApi, buildingsApi } from '../lib/api.js'
import { useAsync } from '../lib/useAsync.js'
import { formatMoney, formatMinutes, DEFAULT_CURRENCY } from '../lib/money.js'
import { formatDate } from '../lib/format.js'
import Card, { CardHeader } from '../components/ui/Card.jsx'
import { Select } from '../components/ui/Field.jsx'
import { ErrorState, LoadingState, Skeleton, EmptyState } from '../components/ui/States.jsx'

export default function ReportsPage() {
  const [unitBuildingId, setUnitBuildingId] = useState('')

  const buildingsQ = useAsync(() => buildingsApi.list(), [])
  const statusQ = useAsync(() => reportsApi.status(), [])
  const timelineQ = useAsync(() => reportsApi.timeline(), [])
  const byBuildingQ = useAsync(() => reportsApi.buildings(), [])
  const byUnitQ = useAsync(() => reportsApi.units(unitBuildingId), [unitBuildingId])

  return (
    <div>
      <header className="mb-5">
        <h1 className="text-2xl font-semibold tracking-tight">Reports</h1>
        <p className="mt-0.5 text-sm text-ink-muted">Spend and hours, computed fresh from the ledger every time — never a stored total.</p>
      </header>

      <StatusTiles statusQ={statusQ} />

      <div className="my-6">
        <TimelineChart timelineQ={timelineQ} />
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <BuildingReport byBuildingQ={byBuildingQ} />
        <UnitReport
          byUnitQ={byUnitQ}
          buildings={buildingsQ.data || []}
          buildingId={unitBuildingId}
          onBuildingChange={setUnitBuildingId}
        />
      </div>
    </div>
  )
}

function StatusTiles({ statusQ }) {
  if (statusQ.error) return <ErrorState message={statusQ.error.message} onRetry={statusQ.reload} />
  const s = statusQ.data
  const tiles = [
    { label: 'Open jobs', value: s?.open, tone: 'text-accent' },
    { label: 'Closed jobs', value: s?.closed, tone: 'text-ink' },
    { label: 'Total jobs', value: s?.total, tone: 'text-ink' },
  ]
  return (
    <div className="grid grid-cols-3 gap-3">
      {tiles.map((t) => (
        <Card key={t.label} className="p-4">
          <p className="text-2xs font-medium uppercase tracking-wide text-ink-faint">{t.label}</p>
          {statusQ.loading ? (
            <Skeleton className="mt-2 h-8 w-16" />
          ) : (
            <p className={`tab-num mt-1 text-3xl font-semibold ${t.tone}`}>{t.value ?? 0}</p>
          )}
        </Card>
      ))}
    </div>
  )
}

// Data-series colours (--series-a blue / --series-b teal) are validated with
// the dataviz palette checker against both light and dark chart surfaces —
// see src/index.css. Values are direct-labelled via <title> tooltips rather
// than relying on colour alone (a WARN the validator raised on contrast).
function TimelineChart({ timelineQ }) {
  const days = timelineQ.data || []
  const max = Math.max(1, ...days.map((d) => Math.max(d.created, d.closed)))

  return (
    <Card className="p-4">
      <CardHeader
        title="Created vs. closed"
        subtitle="Both series come from the job's own timestamps, not a counter."
        actions={
          <div className="flex items-center gap-3 text-2xs text-ink-muted">
            <span className="flex items-center gap-1.5">
              <span className="h-2 w-2 rounded-full" style={{ background: 'var(--series-a)' }} />
              Created
            </span>
            <span className="flex items-center gap-1.5">
              <span className="h-2 w-2 rounded-full" style={{ background: 'var(--series-b)' }} />
              Closed
            </span>
          </div>
        }
      />
      <div className="pt-4">
        {timelineQ.error ? (
          <ErrorState message={timelineQ.error.message} onRetry={timelineQ.reload} />
        ) : timelineQ.loading ? (
          <Skeleton className="h-40 w-full" />
        ) : days.length === 0 ? (
          <EmptyState title="No job history yet" description="This fills in as jobs are created and closed." />
        ) : (
          <div className="flex h-40 items-end gap-2 overflow-x-auto">
            {days.map((d) => (
              <div key={d.day} className="flex min-w-[28px] flex-1 flex-col items-center gap-1">
                <div className="flex h-32 w-full items-end justify-center gap-0.5">
                  <div
                    title={`${formatDate(d.day)} — ${d.created} created`}
                    className="w-2.5 rounded-t-xs"
                    style={{ height: `${(d.created / max) * 100}%`, background: 'var(--series-a)', minHeight: d.created ? 3 : 0 }}
                  />
                  <div
                    title={`${formatDate(d.day)} — ${d.closed} closed`}
                    className="w-2.5 rounded-t-xs"
                    style={{ height: `${(d.closed / max) * 100}%`, background: 'var(--series-b)', minHeight: d.closed ? 3 : 0 }}
                  />
                </div>
                <span className="text-2xs text-ink-faint">{new Date(d.day).getDate()}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </Card>
  )
}

function MagnitudeBar({ value, max }) {
  const pct = max > 0 ? Math.max(2, (Number(value) / max) * 100) : 0
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-surface-sunk">
      <div className="h-full rounded-full" style={{ width: `${pct}%`, background: 'var(--series-a)' }} />
    </div>
  )
}

function BuildingReport({ byBuildingQ }) {
  const rows = byBuildingQ.data || []
  const maxCost = Math.max(1, ...rows.map((r) => Number(r.cost_minor)))
  return (
    <Card className="p-4">
      <CardHeader title="Per building" subtitle="Cost and hours across every job the building has ever raised." />
      <div className="pt-3">
        {byBuildingQ.error ? (
          <ErrorState message={byBuildingQ.error.message} onRetry={byBuildingQ.reload} />
        ) : byBuildingQ.loading ? (
          <Skeleton className="h-40 w-full" />
        ) : rows.length === 0 ? (
          <EmptyState title="Nothing to report yet" />
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-line text-left text-2xs uppercase tracking-wide text-ink-faint">
                <th className="py-2 pr-2 font-medium">Building</th>
                <th className="px-2 py-2 font-medium">Open</th>
                <th className="px-2 py-2 font-medium">Cost</th>
                <th className="py-2 pl-2 font-medium">Hours</th>
              </tr>
            </thead>
            <tbody>
              {[...rows]
                .sort((a, b) => Number(b.cost_minor) - Number(a.cost_minor))
                .map((r) => (
                  <tr key={r.building_id} className="border-b border-line last:border-0">
                    <td className="max-w-[140px] truncate py-2 pr-2">
                      <Link to={`/buildings/${r.building_id}`} className="font-medium text-ink hover:text-accent">
                        {r.name}
                      </Link>
                      <span className="ml-1.5 text-2xs text-ink-faint">({r.jobs})</span>
                    </td>
                    <td className="tab-num px-2 py-2 text-ink-muted">{r.open_jobs}</td>
                    <td className="px-2 py-2">
                      <div className="tab-num mb-1 font-medium text-ink">{formatMoney(r.cost_minor, DEFAULT_CURRENCY)}</div>
                      <MagnitudeBar value={r.cost_minor} max={maxCost} />
                    </td>
                    <td className="tab-num py-2 pl-2 text-ink-muted">{formatMinutes(r.minutes)}</td>
                  </tr>
                ))}
            </tbody>
          </table>
        )}
      </div>
    </Card>
  )
}

function UnitReport({ byUnitQ, buildings, buildingId, onBuildingChange }) {
  // Memoised so the `sorted` memo below actually holds: a bare
  // `byUnitQ.data || []` is a fresh array on every render whenever data is
  // null, which invalidated the sort each time.
  const rows = useMemo(() => byUnitQ.data || [], [byUnitQ.data])
  const maxCost = Math.max(1, ...rows.map((r) => Number(r.cost_minor)))
  const sorted = useMemo(() => [...rows].sort((a, b) => Number(b.cost_minor) - Number(a.cost_minor)), [rows])

  return (
    <Card className="p-4">
      <CardHeader
        title="Per unit"
        subtitle="The number the legacy system fragmented across misspelled labels — now one row per door."
        actions={
          <Select value={buildingId} onChange={(e) => onBuildingChange(e.target.value)} className="w-40">
            <option value="">All buildings</option>
            {buildings.map((b) => (
              <option key={b.id} value={b.id}>
                {b.name}
              </option>
            ))}
          </Select>
        }
      />
      <div className="pt-3">
        {byUnitQ.error ? (
          <ErrorState message={byUnitQ.error.message} onRetry={byUnitQ.reload} />
        ) : byUnitQ.loading ? (
          <Skeleton className="h-40 w-full" />
        ) : sorted.length === 0 ? (
          <EmptyState title="No units with recorded spend yet" />
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-line text-left text-2xs uppercase tracking-wide text-ink-faint">
                <th className="py-2 pr-2 font-medium">Unit</th>
                <th className="px-2 py-2 font-medium">Open</th>
                <th className="px-2 py-2 font-medium">Cost</th>
                <th className="py-2 pl-2 font-medium">Hours</th>
              </tr>
            </thead>
            <tbody>
              {sorted.map((r) => (
                <tr key={r.unit_id} className="border-b border-line last:border-0">
                  <td className="py-2 pr-2">
                    <span className="font-medium text-ink">{r.label}</span>
                    <span className="ml-1.5 text-2xs text-ink-faint">({r.jobs})</span>
                  </td>
                  <td className="tab-num px-2 py-2 text-ink-muted">{r.open_jobs}</td>
                  <td className="px-2 py-2">
                    <div className="tab-num mb-1 font-medium text-ink">{formatMoney(r.cost_minor, DEFAULT_CURRENCY)}</div>
                    <MagnitudeBar value={r.cost_minor} max={maxCost} />
                  </td>
                  <td className="tab-num py-2 pl-2 text-ink-muted">{formatMinutes(r.minutes)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </Card>
  )
}
