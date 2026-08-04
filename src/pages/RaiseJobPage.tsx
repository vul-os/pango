import { useMemo, useState, type ChangeEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { buildingsApi, jobsApi, type Building, type JobPriority } from '../lib/api'
import { useAsync, type UseAsyncResult } from '../lib/useAsync'
import { useGeolocation, rankByProximity, haversineKm } from '../lib/geo'
import { JOB_PRIORITIES, label } from '../lib/domain'
import Button from '../components/ui/Button'
import { Input, Select, Textarea } from '../components/ui/Field'
import { ErrorState, LoadingState, InlineError, EmptyState } from '../components/ui/States'
import { MapPinIcon, SearchIcon, ChevronLeftIcon, CheckIcon } from '../components/icons'
import PhotoPicker from '../components/PhotoPicker'

const CATEGORY_SUGGESTIONS = ['plumbing', 'electrical', 'general', 'compliance', 'damp', 'security', 'appliance', 'structural']

const STEPS = ['Building', 'Unit', 'Describe', 'Priority', 'Photos']

export default function RaiseJobPage() {
  const navigate = useNavigate()
  const [step, setStep] = useState(0)
  const [buildingId, setBuildingId] = useState('')
  const [unitLabel, setUnitLabel] = useState('')
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [category, setCategory] = useState('')
  const [priority, setPriority] = useState<JobPriority>('normal')
  const [photos, setPhotos] = useState<File[]>([])
  const [submitError, setSubmitError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const buildingsQ = useAsync(() => buildingsApi.list(), [])
  const building = (buildingsQ.data || []).find((b) => b.id === buildingId)

  const canNext = [
    Boolean(buildingId),
    true, // unit is optional
    title.trim().length > 0,
    Boolean(priority),
    true,
  ]

  async function submit() {
    setSubmitting(true)
    setSubmitError('')
    try {
      const job = await jobsApi.create({
        building_id: buildingId,
        unit_id: '',
        unit_label: unitLabel.trim(),
        title: title.trim(),
        description: description.trim(),
        priority,
        category: category.trim(),
        reporter_party_id: '',
      })
      navigate(`/jobs/${job.id}`, { replace: true })
    } catch (err) {
      setSubmitError((err instanceof Error && err.message) || 'Could not raise the job.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="mx-auto max-w-xl">
      <button
        type="button"
        onClick={() => navigate('/jobs')}
        className="mb-3 flex items-center gap-1 text-xs font-medium text-ink-muted hover:text-ink"
      >
        <ChevronLeftIcon width={14} height={14} />
        Cancel
      </button>

      <h1 className="mb-1 text-2xl font-semibold tracking-tight">Raise a job</h1>
      <p className="mb-5 text-sm text-ink-muted">Five short steps — building, unit, what&rsquo;s wrong, priority, photos.</p>

      <ol className="mb-6 flex items-center gap-1.5" aria-label="Progress">
        {STEPS.map((s, i) => (
          <li key={s} className="flex flex-1 items-center gap-1.5">
            <div
              className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-2xs font-semibold ${
                i < step ? 'bg-accent text-white' : i === step ? 'border-2 border-accent text-accent' : 'border border-line text-ink-faint'
              }`}
            >
              {i < step ? <CheckIcon width={12} height={12} /> : i + 1}
            </div>
            <span className={`hidden text-2xs sm:inline ${i === step ? 'font-medium text-ink' : 'text-ink-faint'}`}>{s}</span>
            {i < STEPS.length - 1 && <div className="h-px flex-1 bg-line" />}
          </li>
        ))}
      </ol>

      <div className="rounded-md border border-line bg-surface-raised p-4">
        {step === 0 && (
          <BuildingStep
            buildingsQ={buildingsQ}
            selected={buildingId}
            onSelect={setBuildingId}
          />
        )}
        {step === 1 && <UnitStep building={building} unitLabel={unitLabel} onChange={setUnitLabel} />}
        {step === 2 && (
          <DescribeStep
            title={title}
            onTitle={setTitle}
            description={description}
            onDescription={setDescription}
            category={category}
            onCategory={setCategory}
          />
        )}
        {step === 3 && <PriorityStep priority={priority} onChange={setPriority} />}
        {step === 4 && (
          <div>
            <h2 className="mb-1 text-sm font-semibold text-ink">Photos</h2>
            <p className="mb-3 text-xs text-ink-muted">Optional — attach evidence of the problem.</p>
            <PhotoPicker files={photos} onChange={setPhotos} />
            <div className="mt-4 rounded-sm border border-line bg-surface-sunk p-3 text-xs text-ink-muted">
              <p className="mb-1 font-medium text-ink">Summary</p>
              <p>
                {building?.name || '—'}
                {unitLabel ? ` · ${unitLabel}` : ' · No unit specified'}
              </p>
              <p className="mt-0.5">{title || '(no title yet)'}</p>
              <p className="mt-0.5">Priority: {label(priority)}</p>
            </div>
          </div>
        )}

        <InlineError message={submitError} />

        <div className="mt-5 flex items-center justify-between border-t border-line pt-4">
          <Button variant="ghost" onClick={() => setStep((s) => Math.max(0, s - 1))} disabled={step === 0}>
            Back
          </Button>
          {step < STEPS.length - 1 ? (
            <Button variant="primary" onClick={() => setStep((s) => s + 1)} disabled={!canNext[step]}>
              Continue
            </Button>
          ) : (
            <Button variant="primary" onClick={submit} disabled={submitting || !title.trim() || !buildingId}>
              {submitting ? 'Raising job…' : 'Raise job'}
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}

interface BuildingStepProps {
  buildingsQ: UseAsyncResult<Building[]>
  selected: string
  onSelect: (id: string) => void
}

function BuildingStep({ buildingsQ, selected, onSelect }: BuildingStepProps) {
  const [q, setQ] = useState('')
  const geo = useGeolocation()

  const ranked = useMemo(() => {
    const list = buildingsQ.data || []
    const filtered = q.trim()
      ? list.filter((b) => b.name.toLowerCase().includes(q.toLowerCase()) || b.address.toLowerCase().includes(q.toLowerCase()))
      : list
    return geo.position ? rankByProximity(filtered, geo.position) : filtered
  }, [buildingsQ.data, q, geo.position])

  if (buildingsQ.loading) return <LoadingState label="Loading buildings…" />
  if (buildingsQ.error) return <ErrorState message={buildingsQ.error.message} onRetry={buildingsQ.reload} />
  if ((buildingsQ.data || []).length === 0) {
    return <EmptyState title="No buildings yet" description="Add a building first, from the Buildings page." />
  }

  return (
    <div>
      <h2 className="mb-3 text-sm font-semibold text-ink">Which building?</h2>
      <div className="mb-3 flex gap-2">
        <div className="relative flex-1">
          <SearchIcon width={15} height={15} className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-ink-faint" />
          <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search buildings" className="pl-8" />
        </div>
        <Button
          variant="secondary"
          onClick={geo.request}
          disabled={geo.status === 'locating'}
          title="Sort by distance from where you are"
        >
          <MapPinIcon width={15} height={15} />
          {geo.status === 'locating' ? 'Locating…' : geo.status === 'done' ? 'Nearest first' : 'Near me'}
        </Button>
      </div>
      {geo.status === 'error' && <p className="mb-2 text-2xs text-serious">Could not get your location: {geo.error}</p>}
      {geo.status === 'unsupported' && <p className="mb-2 text-2xs text-ink-faint">Geolocation isn&rsquo;t available in this browser.</p>}

      <ul className="flex max-h-80 flex-col gap-1.5 overflow-y-auto">
        {ranked.map((b) => {
          const distance =
            geo.position && b.lat != null && b.lon != null ? haversineKm(geo.position.lat, geo.position.lon, b.lat, b.lon) : null
          return (
            <li key={b.id}>
              <button
                type="button"
                onClick={() => onSelect(b.id)}
                className={`flex w-full items-center justify-between rounded-sm border px-3 py-2.5 text-left transition-colors ${
                  selected === b.id ? 'border-accent bg-accent-tint' : 'border-line hover:bg-surface-sunk'
                }`}
              >
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-ink">{b.name}</p>
                  <p className="truncate text-xs text-ink-muted">{b.address}</p>
                </div>
                {distance != null && <span className="tab-num shrink-0 pl-3 text-xs text-ink-faint">{distance.toFixed(1)} km</span>}
              </button>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

interface UnitStepProps {
  building: Building | undefined
  unitLabel: string
  onChange: (label: string) => void
}

function UnitStep({ building, unitLabel, onChange }: UnitStepProps) {
  const unitsQ = useAsync(() => (building ? buildingsApi.units(building.id) : Promise.resolve([])), [building?.id])
  const existing = unitsQ.data || []

  return (
    <div>
      <h2 className="mb-1 text-sm font-semibold text-ink">Which unit?</h2>
      <p className="mb-3 text-xs text-ink-muted">
        Type it as you normally would — &ldquo;Flat 3A&rdquo; and &ldquo;3A&rdquo; are recognised as the same door. Leave blank for a
        common-area job.
      </p>
      <Input
        list="existing-units"
        value={unitLabel}
        onChange={(e) => onChange(e.target.value)}
        placeholder="e.g. Flat 3A"
        autoFocus
      />
      <datalist id="existing-units">
        {existing.map((u) => (
          <option key={u.id} value={u.label} />
        ))}
      </datalist>
      {existing.length > 0 && (
        <div className="mt-3">
          <p className="mb-1.5 text-2xs font-medium text-ink-faint">Existing units in {building!.name}</p>
          <div className="flex flex-wrap gap-1.5">
            {existing.slice(0, 12).map((u) => (
              <button
                key={u.id}
                type="button"
                onClick={() => onChange(u.label)}
                className="rounded-xs border border-line bg-surface-sunk px-2 py-1 text-xs text-ink-muted hover:border-line-strong hover:text-ink"
              >
                {u.label}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

interface DescribeStepProps {
  title: string
  onTitle: (v: string) => void
  description: string
  onDescription: (v: string) => void
  category: string
  onCategory: (v: string) => void
}

function DescribeStep({ title, onTitle, description, onDescription, category, onCategory }: DescribeStepProps) {
  return (
    <div className="flex flex-col gap-3">
      <div>
        <label className="mb-1 block text-xs font-medium text-ink-muted" htmlFor="job-title">
          What&rsquo;s wrong? <span className="text-critical">*</span>
        </label>
        <Input id="job-title" value={title} onChange={(e) => onTitle(e.target.value)} placeholder="Kitchen mixer leaking under sink" autoFocus />
      </div>
      <div>
        <label className="mb-1 block text-xs font-medium text-ink-muted" htmlFor="job-desc">
          More detail
        </label>
        <Textarea
          id="job-desc"
          value={description}
          onChange={(e) => onDescription(e.target.value)}
          placeholder="Anything a contractor would want to know before arriving."
        />
      </div>
      <div>
        <label className="mb-1 block text-xs font-medium text-ink-muted" htmlFor="job-category">
          Category
        </label>
        <Input id="job-category" list="category-suggestions" value={category} onChange={(e) => onCategory(e.target.value)} placeholder="plumbing" />
        <datalist id="category-suggestions">
          {CATEGORY_SUGGESTIONS.map((c) => (
            <option key={c} value={c} />
          ))}
        </datalist>
      </div>
    </div>
  )
}

const PRIORITY_DESCRIPTIONS = {
  low: 'No urgency — fits around other work.',
  normal: 'Standard job, scheduled in the usual order.',
  high: 'Needs attention soon — affects daily use.',
  emergency: 'Immediate — safety, security or major damage risk.',
}

interface PriorityStepProps {
  priority: JobPriority
  onChange: (p: JobPriority) => void
}

function PriorityStep({ priority, onChange }: PriorityStepProps) {
  return (
    <div>
      <h2 className="mb-3 text-sm font-semibold text-ink">How urgent is it?</h2>
      <div className="grid grid-cols-2 gap-2">
        {JOB_PRIORITIES.map((p) => (
          <button
            key={p}
            type="button"
            onClick={() => onChange(p)}
            className={`rounded-sm border px-3 py-3 text-left transition-colors ${
              priority === p ? 'border-accent bg-accent-tint' : 'border-line hover:bg-surface-sunk'
            }`}
          >
            <p className="text-sm font-medium capitalize text-ink">{p}</p>
            <p className="mt-0.5 text-2xs text-ink-muted">{PRIORITY_DESCRIPTIONS[p]}</p>
          </button>
        ))}
      </div>
    </div>
  )
}
