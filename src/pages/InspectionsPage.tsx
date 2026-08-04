import { useMemo, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import {
  buildingsApi,
  inspectionsApi,
  partiesApi,
  templatesApi,
  type Building,
  type Inspection,
  type InspectionKind,
  type InspectionStatus,
} from '../lib/api'
import { useAsync } from '../lib/useAsync'
import { useUnitsIndex } from '../lib/useUnitsIndex'
import { INSPECTION_KINDS, INSPECTION_STATUSES, label } from '../lib/domain'
import { formatDate } from '../lib/format'
import Pill, { type Tone } from '../components/ui/Pill'
import Card from '../components/ui/Card'
import Button from '../components/ui/Button'
import Modal from '../components/ui/Modal'
import { Select, Input, FormRow } from '../components/ui/Field'
import { EmptyState, ErrorState, LoadingState, InlineError, Skeleton } from '../components/ui/States'
import { ClipboardIcon, PlusIcon } from '../components/icons'

const KIND_TONE: Record<InspectionKind, Tone> = { ingoing: 'info', outgoing: 'warning', routine: 'neutral', snag: 'serious' }
const STATUS_TONE: Record<InspectionStatus, Tone> = { scheduled: 'neutral', in_progress: 'info', complete: 'good' }

export default function InspectionsPage() {
  const [buildingId, setBuildingId] = useState('')
  const [kind, setKind] = useState('')
  const [status, setStatus] = useState('')
  const [open, setOpen] = useState(false)

  const buildingsQ = useAsync(() => buildingsApi.list(), [])
  const inspectionsQ = useAsync(
    () => inspectionsApi.list({ building_id: buildingId || undefined, kind: kind || undefined, status: status || undefined }),
    [buildingId, kind, status],
  )
  const buildingMap = useMemo(() => new Map((buildingsQ.data || []).map((b) => [b.id, b])), [buildingsQ.data])
  const unitsIndex = useUnitsIndex((inspectionsQ.data || []).map((i) => i.building_id))

  const anyFilter = buildingId || kind || status

  return (
    <div>
      <header className="mb-5 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Inspections</h1>
          <p className="mt-0.5 text-sm text-ink-muted">Templated condition checks, and the ingoing/outgoing pair that proves deterioration.</p>
        </div>
        <Button variant="primary" onClick={() => setOpen(true)}>
          <PlusIcon width={16} height={16} />
          Schedule inspection
        </Button>
      </header>

      <Card className="mb-5 flex flex-wrap items-end gap-3 p-3">
        <div className="w-48">
          <label className="mb-1 block text-2xs font-medium text-ink-faint">Building</label>
          <Select value={buildingId} onChange={(e) => setBuildingId(e.target.value)}>
            <option value="">All buildings</option>
            {(buildingsQ.data || []).map((b) => (
              <option key={b.id} value={b.id}>
                {b.name}
              </option>
            ))}
          </Select>
        </div>
        <div className="w-40">
          <label className="mb-1 block text-2xs font-medium text-ink-faint">Kind</label>
          <Select value={kind} onChange={(e) => setKind(e.target.value)}>
            <option value="">All kinds</option>
            {INSPECTION_KINDS.map((k) => (
              <option key={k} value={k}>
                {label(k)}
              </option>
            ))}
          </Select>
        </div>
        <div className="w-40">
          <label className="mb-1 block text-2xs font-medium text-ink-faint">Status</label>
          <Select value={status} onChange={(e) => setStatus(e.target.value)}>
            <option value="">All statuses</option>
            {INSPECTION_STATUSES.map((s) => (
              <option key={s} value={s}>
                {label(s)}
              </option>
            ))}
          </Select>
        </div>
        {anyFilter && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              setBuildingId('')
              setKind('')
              setStatus('')
            }}
          >
            Clear filters
          </Button>
        )}
      </Card>

      {inspectionsQ.error ? (
        <ErrorState message={inspectionsQ.error.message} onRetry={inspectionsQ.reload} />
      ) : inspectionsQ.loading ? (
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-16 w-full" />
          ))}
        </div>
      ) : (inspectionsQ.data || []).length === 0 ? (
        <EmptyState
          icon={<ClipboardIcon width={28} height={28} />}
          title={anyFilter ? 'No inspections match these filters' : 'No inspections scheduled'}
          description="Schedule an ingoing inspection when a tenancy starts, and an outgoing one when it ends — Pango compares them automatically."
          action={
            <Button variant="primary" onClick={() => setOpen(true)}>
              Schedule inspection
            </Button>
          }
        />
      ) : (
        <ul className="flex flex-col gap-2">
          {inspectionsQ.data!.map((insp) => {
            const b = buildingMap.get(insp.building_id)
            const u = unitsIndex.get(insp.unit_id)
            return (
              <li key={insp.id}>
                <Link
                  to={`/inspections/${insp.id}`}
                  className="flex items-center justify-between gap-3 rounded-md border border-line bg-surface-raised px-4 py-3 shadow-e1 transition-shadow hover:shadow-e2"
                >
                  <div className="flex items-center gap-3">
                    <Pill tone={KIND_TONE[insp.kind]}>{label(insp.kind)}</Pill>
                    <div>
                      <p className="text-sm font-medium text-ink">
                        {b?.name || 'Unknown building'}
                        {u ? ` · ${u.label}` : ''}
                      </p>
                      <p className="text-xs text-ink-muted">
                        {insp.scheduled_for ? `Scheduled ${formatDate(insp.scheduled_for)}` : 'Not scheduled'}
                      </p>
                    </div>
                  </div>
                  <Pill tone={STATUS_TONE[insp.status]}>{label(insp.status)}</Pill>
                </Link>
              </li>
            )
          })}
        </ul>
      )}

      <ScheduleModal
        open={open}
        onClose={() => setOpen(false)}
        buildings={buildingsQ.data || []}
        onCreated={(insp) => {
          inspectionsQ.setData((d) => [insp, ...(d || [])])
          setOpen(false)
        }}
      />
    </div>
  )
}

interface ScheduleModalProps {
  open: boolean
  onClose: () => void
  buildings: Building[]
  onCreated: (inspection: Inspection) => void
}

function ScheduleModal({ open, onClose, buildings, onCreated }: ScheduleModalProps) {
  const [buildingId, setBuildingId] = useState('')
  const [unitLabel, setUnitLabel] = useState('')
  const [templateId, setTemplateId] = useState('')
  const [kind, setKind] = useState<InspectionKind>('routine')
  const [scheduledFor, setScheduledFor] = useState('')
  const [inspectorId, setInspectorId] = useState('')
  const [notes, setNotes] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const templatesQ = useAsync(() => templatesApi.list(), [])
  const partiesQ = useAsync(() => partiesApi.list(), [])
  const needsUnit = kind === 'ingoing' || kind === 'outgoing'

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!buildingId) {
      setError('Pick a building.')
      return
    }
    if (needsUnit && !unitLabel.trim()) {
      setError(`An ${kind} inspection must name a unit — it exists to be paired against its counterpart.`)
      return
    }
    setBusy(true)
    setError('')
    try {
      const insp = await inspectionsApi.create({
        building_id: buildingId,
        unit_id: '',
        unit_label: unitLabel.trim(),
        template_id: templateId,
        kind,
        scheduled_for: scheduledFor ? new Date(scheduledFor).toISOString() : '',
        inspector_party_id: inspectorId,
        notes: notes.trim(),
      })
      onCreated(insp)
      setBuildingId('')
      setUnitLabel('')
      setNotes('')
    } catch (err) {
      setError((err instanceof Error && err.message) || 'Could not schedule the inspection.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open={open} onClose={onClose} title="Schedule an inspection">
      <form onSubmit={submit} className="flex flex-col gap-3.5">
        <InlineError message={error} />
        <FormRow label="Kind" htmlFor="i-kind" required>
          <Select id="i-kind" value={kind} onChange={(e) => setKind(e.target.value as InspectionKind)}>
            {INSPECTION_KINDS.map((k) => (
              <option key={k} value={k}>
                {label(k)}
              </option>
            ))}
          </Select>
        </FormRow>
        <FormRow label="Building" htmlFor="i-building" required>
          <Select id="i-building" value={buildingId} onChange={(e) => setBuildingId(e.target.value)}>
            <option value="">Select a building</option>
            {buildings.map((b) => (
              <option key={b.id} value={b.id}>
                {b.name}
              </option>
            ))}
          </Select>
        </FormRow>
        <FormRow label="Unit" htmlFor="i-unit" required={needsUnit} hint={needsUnit ? 'required for ingoing/outgoing' : 'optional'}>
          <Input id="i-unit" value={unitLabel} onChange={(e) => setUnitLabel(e.target.value)} placeholder="Flat 3A" />
        </FormRow>
        <FormRow label="Template" htmlFor="i-template" hint="optional checklist">
          <Select id="i-template" value={templateId} onChange={(e) => setTemplateId(e.target.value)}>
            <option value="">No template</option>
            {(templatesQ.data || []).map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
          </Select>
        </FormRow>
        <FormRow label="Inspector" htmlFor="i-inspector">
          <Select id="i-inspector" value={inspectorId} onChange={(e) => setInspectorId(e.target.value)}>
            <option value="">Unassigned</option>
            {(partiesQ.data || []).map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </Select>
        </FormRow>
        <FormRow label="Scheduled for" htmlFor="i-when">
          <Input id="i-when" type="datetime-local" value={scheduledFor} onChange={(e) => setScheduledFor(e.target.value)} />
        </FormRow>
        <FormRow label="Notes" htmlFor="i-notes">
          <Input id="i-notes" value={notes} onChange={(e) => setNotes(e.target.value)} placeholder="Context for whoever walks it" />
        </FormRow>
        <div className="mt-1 flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            {busy ? 'Scheduling…' : 'Schedule'}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
