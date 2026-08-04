import { useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import {
  buildingsApi,
  inspectionsApi,
  templatesApi,
  ApiError,
  type Comparison,
  type ComparisonOutcome,
  type Finding,
  type FindingCondition,
  type Inspection,
  type InspectionKind,
  type InspectionStatus,
  type Template,
  type TemplateItem,
} from '../lib/api'
import { useAsync, type UseAsyncResult } from '../lib/useAsync'
import { INSPECTION_STATUSES, FINDING_CONDITIONS, label } from '../lib/domain'
import { formatDate, formatDateTime } from '../lib/format'
import { ConditionPill } from '../components/JobBadges'
import Card, { CardHeader } from '../components/ui/Card'
import Pill, { type Tone } from '../components/ui/Pill'
import Button from '../components/ui/Button'
import { Select, Input } from '../components/ui/Field'
import { ErrorState, LoadingState, InlineError, EmptyState } from '../components/ui/States'
import { ChevronLeftIcon } from '../components/icons'

const KIND_TONE: Record<InspectionKind, Tone> = { ingoing: 'info', outgoing: 'warning', routine: 'neutral', snag: 'serious' }
const STATUS_TONE: Record<InspectionStatus, Tone> = { scheduled: 'neutral', in_progress: 'info', complete: 'good' }

// Mirrors inspect.Outcome (backend/internal/inspect/compare.go) — the five
// deliberate states. "not_captured_*" is never collapsed into "deteriorated"
// or "unchanged": a missing baseline is not evidence either way.
const OUTCOME: Record<ComparisonOutcome, { tone: Tone; text: string }> = {
  unchanged: { tone: 'neutral', text: 'No change' },
  deteriorated: { tone: 'critical', text: 'Deteriorated' },
  improved: { tone: 'good', text: 'Improved' },
  not_captured_ingoing: { tone: 'warning', text: 'Not captured (ingoing)' },
  not_captured_outgoing: { tone: 'warning', text: 'Not captured (outgoing)' },
}

export default function InspectionDetailPage() {
  // This page only mounts under the "inspections/:id" route (see App.tsx),
  // so the param is always present.
  const { id } = useParams() as { id: string }
  const navigate = useNavigate()
  const inspQ = useAsync(() => inspectionsApi.get(id), [id])
  const inspection = inspQ.data?.inspection
  const findings = inspQ.data?.findings || []

  const buildingQ = useAsync(
    () => (inspection ? buildingsApi.get(inspection.building_id) : Promise.resolve(null)),
    [inspection?.building_id],
  )
  const unitsQ = useAsync(
    () => (inspection ? buildingsApi.units(inspection.building_id) : Promise.resolve([])),
    [inspection?.building_id],
  )
  const templateQ = useAsync(
    () => (inspection?.template_id ? templatesApi.get(inspection.template_id) : Promise.resolve(null)),
    [inspection?.template_id],
  )

  // The differentiator (§1): GET /api/inspections/{outgoing-id}/comparison
  // resolves the matching ingoing inspection server-side (repo.MatchingIngoing)
  // and returns the per-item delta — this page never pairs the two itself.
  // A 404 means no ingoing baseline exists yet for this unit; that is a real,
  // renderable state, not an error to swallow.
  const isOutgoing = inspection?.kind === 'outgoing'
  const comparisonQ = useAsync(
    () => (isOutgoing ? inspectionsApi.compare(id) : Promise.resolve(null)),
    [isOutgoing, id],
  )
  const noBaselineYet = comparisonQ.error instanceof ApiError && comparisonQ.error.status === 404

  const [statusBusy, setStatusBusy] = useState(false)
  const [statusError, setStatusError] = useState('')

  if (inspQ.loading) return <LoadingState label="Loading inspection…" />
  if (inspQ.error) return <ErrorState message={inspQ.error.message} onRetry={inspQ.reload} />
  if (!inspection) return null

  const unit = (unitsQ.data || []).find((u) => u.id === inspection.unit_id)

  async function changeStatus(next: string) {
    if (!next || next === inspection!.status) return
    setStatusBusy(true)
    setStatusError('')
    try {
      const updated = await inspectionsApi.setStatus(id, next as InspectionStatus)
      inspQ.setData((d) => ({ ...d!, inspection: updated }))
    } catch (err) {
      setStatusError((err instanceof Error && err.message) || 'Could not change status.')
    } finally {
      setStatusBusy(false)
    }
  }

  return (
    <div>
      <button
        type="button"
        onClick={() => navigate('/inspections')}
        className="mb-3 flex items-center gap-1 text-xs font-medium text-ink-muted hover:text-ink"
      >
        <ChevronLeftIcon width={14} height={14} />
        Back to inspections
      </button>

      <div className="mb-5 flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="mb-1 flex items-center gap-2">
            <Pill tone={KIND_TONE[inspection.kind]}>{label(inspection.kind)}</Pill>
            <Pill tone={STATUS_TONE[inspection.status]}>{label(inspection.status)}</Pill>
          </div>
          <h1 className="text-2xl font-semibold tracking-tight">
            {buildingQ.data?.name || 'Building'}
            {unit ? ` · ${unit.label}` : ''}
          </h1>
          <p className="mt-1 text-sm text-ink-muted">
            {inspection.scheduled_for ? `Scheduled ${formatDateTime(inspection.scheduled_for)}` : 'Not scheduled'}
          </p>
          {inspection.notes && <p className="mt-1 max-w-xl text-sm text-ink-muted">{inspection.notes}</p>}
        </div>
        <div className="w-52 shrink-0 rounded-md border border-line bg-surface-raised p-3">
          <InlineError message={statusError} />
          <label className="mb-1 block text-2xs font-medium text-ink-faint">Status</label>
          <Select value={inspection.status} onChange={(e) => changeStatus(e.target.value)} disabled={statusBusy}>
            {INSPECTION_STATUSES.map((s) => (
              <option key={s} value={s}>
                {label(s)}
              </option>
            ))}
          </Select>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-[1fr_320px]">
        <Card className="p-4">
          <CardHeader
            title="Checklist"
            subtitle={templateQ.data ? templateQ.data.name : 'No template — findings are recorded ad hoc'}
          />
          <div className="pt-3">
            <ChecklistRunner
              inspectionId={id}
              template={templateQ.data}
              findings={findings}
              onSaved={(f) => inspQ.setData((d) => ({ ...d!, findings: [...d!.findings, f] }))}
            />
          </div>
        </Card>

        <Card className="h-fit p-4">
          <CardHeader title="Findings recorded" subtitle={`${findings.length} entries`} />
          <div className="max-h-[480px] overflow-y-auto pt-3">
            {findings.length === 0 ? (
              <EmptyState title="Nothing recorded yet" description="Walk the checklist on the left." />
            ) : (
              <ul className="flex flex-col gap-2">
                {[...findings]
                  .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
                  .map((f) => (
                    <li key={f.id} className="rounded-sm border border-line px-2.5 py-2">
                      <div className="mb-1 flex items-center justify-between">
                        <span className="truncate text-xs font-medium text-ink">{f.label || 'Item'}</span>
                        <ConditionPill condition={f.condition} />
                      </div>
                      {f.comment && <p className="text-xs text-ink-muted">{f.comment}</p>}
                      <p className="tab-num mt-1 text-2xs text-ink-faint">{formatDate(f.created_at)}</p>
                    </li>
                  ))}
              </ul>
            )}
          </div>
        </Card>
      </div>

      {isOutgoing && (
        <div className="mt-6">
          <ComparisonCard comparisonQ={comparisonQ} noBaselineYet={noBaselineYet} />
        </div>
      )}
    </div>
  )
}

function latestByItem(findings: Finding[]): Map<string, Finding> {
  const map = new Map<string, Finding>()
  for (const f of findings) {
    const key = f.item_id || f.label
    const existing = map.get(key)
    if (!existing || new Date(f.created_at) > new Date(existing.created_at)) map.set(key, f)
  }
  return map
}

interface Draft {
  condition: FindingCondition
  comment: string
}

interface ChecklistRunnerProps {
  inspectionId: string
  template: Template | null
  findings: Finding[]
  onSaved: (finding: Finding) => void
}

function ChecklistRunner({ inspectionId, template, findings, onSaved }: ChecklistRunnerProps) {
  const latest = useMemo(() => latestByItem(findings), [findings])
  const [drafts, setDrafts] = useState<Record<string, Draft>>({})
  const [savingId, setSavingId] = useState<string | null>(null)
  const [error, setError] = useState('')

  const items = template?.items || []

  function draftFor(item: TemplateItem): Draft {
    return drafts[item.id] ?? { condition: latest.get(item.id)?.condition || 'ok', comment: '' }
  }

  function setDraft(item: TemplateItem, patch: Partial<Draft>) {
    setDrafts((d) => ({ ...d, [item.id]: { ...draftFor(item), ...patch } }))
  }

  async function save(item: TemplateItem) {
    const draft = draftFor(item)
    setSavingId(item.id)
    setError('')
    try {
      const f = await inspectionsApi.addFinding(inspectionId, {
        item_id: item.id,
        label: item.label,
        condition: draft.condition,
        comment: draft.comment,
        photo_refs: '',
      })
      onSaved(f)
      setDrafts((d) => ({ ...d, [item.id]: { condition: draft.condition, comment: '' } }))
    } catch (err) {
      setError((err instanceof Error && err.message) || 'Could not save that finding.')
    } finally {
      setSavingId(null)
    }
  }

  if (!template) {
    return (
      <EmptyState
        title="No checklist attached"
        description="This inspection has no template. Findings can still be recorded from Settings → Templates on future inspections."
      />
    )
  }

  const bySection = new Map<string, TemplateItem[]>()
  for (const item of [...items].sort((a, b) => a.sort - b.sort)) {
    const list = bySection.get(item.section) || []
    list.push(item)
    bySection.set(item.section, list)
  }

  return (
    <div>
      <InlineError message={error} />
      <div className="flex flex-col gap-5">
        {[...bySection.entries()].map(([section, sectionItems]) => (
          <div key={section}>
            <h4 className="mb-2 text-2xs font-semibold uppercase tracking-wide text-ink-faint">{section}</h4>
            <div className="flex flex-col gap-2">
              {sectionItems.map((item) => {
                const done = latest.get(item.id)
                const draft = draftFor(item)
                return (
                  <div key={item.id} className="rounded-sm border border-line p-2.5">
                    <div className="mb-1.5 flex items-center justify-between gap-2">
                      <span className="text-sm text-ink">{item.label}</span>
                      {done && <ConditionPill condition={done.condition} />}
                    </div>
                    <div className="flex flex-wrap items-center gap-2">
                      <Select
                        value={draft.condition}
                        onChange={(e) => setDraft(item, { condition: e.target.value as FindingCondition })}
                        className="w-32"
                      >
                        {FINDING_CONDITIONS.map((c) => (
                          <option key={c} value={c}>
                            {c === 'na' ? 'N/A' : label(c)}
                          </option>
                        ))}
                      </Select>
                      <Input
                        value={draft.comment}
                        onChange={(e) => setDraft(item, { comment: e.target.value })}
                        placeholder="Comment"
                        className="flex-1"
                      />
                      <Button
                        variant="secondary"
                        size="sm"
                        disabled={savingId === item.id}
                        onClick={() => save(item)}
                      >
                        {savingId === item.id ? 'Saving…' : done ? 'Update' : 'Record'}
                      </Button>
                    </div>
                    {done?.comment && <p className="mt-1.5 text-2xs text-ink-faint">Last: {done.comment}</p>}
                  </div>
                )
              })}
            </div>
          </div>
        ))}
      </div>
      <p className="mt-4 text-2xs text-ink-faint">
        Findings are append-only — recording an item again adds a new entry rather than changing the old one, so the
        history stays intact.
      </p>
    </div>
  )
}

interface ComparisonCardProps {
  comparisonQ: UseAsyncResult<Comparison | null>
  noBaselineYet: boolean
}

function ComparisonCard({ comparisonQ, noBaselineYet }: ComparisonCardProps) {
  const cmp = comparisonQ.data

  return (
    <Card className="p-4">
      <CardHeader
        title="Ingoing / outgoing comparison"
        subtitle="Item-by-item — this is the evidence a deposit deduction rests on."
        actions={
          cmp && (
            <div className="flex items-center gap-2 text-2xs text-ink-muted">
              {Object.entries(cmp.counts || {}).map(([outcome, n]) => (
                <Pill key={outcome} tone={OUTCOME[outcome as ComparisonOutcome]?.tone || 'neutral'}>
                  {n} {OUTCOME[outcome as ComparisonOutcome]?.text || outcome}
                </Pill>
              ))}
            </div>
          )
        }
      />
      <div className="overflow-x-auto pt-3">
        {comparisonQ.loading ? (
          <LoadingState label="Loading the comparison…" />
        ) : noBaselineYet ? (
          <EmptyState
            title="No ingoing baseline yet"
            description="There is no ingoing inspection recorded for this unit before this one — schedule and complete one to compare against."
          />
        ) : comparisonQ.error ? (
          <ErrorState message={comparisonQ.error.message} onRetry={comparisonQ.reload} />
        ) : !cmp || cmp.items.length === 0 ? (
          <EmptyState title="Nothing to compare yet" description="Record findings on both inspections first." />
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-line text-left text-2xs uppercase tracking-wide text-ink-faint">
                <th className="py-2 pr-3 font-medium">Item</th>
                <th className="px-3 py-2 font-medium">Ingoing</th>
                <th className="px-3 py-2 font-medium">Outgoing</th>
                <th className="py-2 pl-3 font-medium">Change</th>
              </tr>
            </thead>
            <tbody>
              {cmp.items.map((r, i) => {
                const outcome = OUTCOME[r.outcome] || { tone: 'neutral', text: r.outcome }
                return (
                  <tr key={i} className="border-b border-line last:border-0">
                    <td className="py-2 pr-3 text-ink">
                      {r.label}
                      {r.section && <span className="ml-1.5 text-2xs text-ink-faint">({r.section})</span>}
                    </td>
                    <td className="px-3 py-2">
                      {/* Backend invariant (compare.go): *_captured implies the matching *_condition is set. */}
                      {r.ingoing_captured ? <ConditionPill condition={r.ingoing_condition!} /> : <span className="text-ink-faint">Not recorded</span>}
                    </td>
                    <td className="px-3 py-2">
                      {r.outgoing_captured ? <ConditionPill condition={r.outgoing_condition!} /> : <span className="text-ink-faint">Not recorded</span>}
                    </td>
                    <td className="py-2 pl-3">
                      <Pill tone={outcome.tone}>{outcome.text}</Pill>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </div>
    </Card>
  )
}
