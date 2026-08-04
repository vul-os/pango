// Mirrors the enums in backend/internal/domain/domain.go. Kept in one place
// so a status pill and a filter dropdown never drift apart.

import type { Tone } from '../components/ui/Pill'

export const JOB_STATUSES = [
  'reported',
  'triaged',
  'assigned',
  'in_progress',
  'on_hold',
  'resolved',
  'closed',
  'cancelled',
] as const

export type JobStatus = (typeof JOB_STATUSES)[number]

export const OPEN_STATUSES = JOB_STATUSES.filter((s) => s !== 'closed' && s !== 'cancelled')

// Kanban-style columns for the board view — a deliberately smaller set than
// the full status graph (on_hold/resolved fold into neighbours visually via
// a badge rather than their own column, so the board stays scannable).
export const BOARD_COLUMNS: { status: JobStatus; label: string }[] = [
  { status: 'reported', label: 'Reported' },
  { status: 'triaged', label: 'Triaged' },
  { status: 'assigned', label: 'Assigned' },
  { status: 'in_progress', label: 'In progress' },
  { status: 'resolved', label: 'Resolved' },
  { status: 'closed', label: 'Closed' },
]

// Mirrors jobTransitions in backend/internal/domain/domain.go — used only to
// hint which next statuses make sense in the UI. The server re-validates
// every transition independently; this is a UX nicety, not the authority.
export const JOB_TRANSITIONS: Record<JobStatus, JobStatus[]> = {
  reported: ['triaged', 'assigned', 'in_progress', 'on_hold', 'cancelled'],
  triaged: ['assigned', 'in_progress', 'on_hold', 'cancelled'],
  assigned: ['in_progress', 'on_hold', 'triaged', 'resolved', 'cancelled'],
  in_progress: ['on_hold', 'resolved', 'assigned', 'cancelled'],
  on_hold: ['in_progress', 'assigned', 'triaged', 'cancelled'],
  resolved: ['closed', 'in_progress'],
  closed: ['in_progress'],
  cancelled: ['reported'],
}

export function nextStatuses(status: JobStatus): JobStatus[] {
  return JOB_TRANSITIONS[status] || []
}

export const JOB_PRIORITIES = ['low', 'normal', 'high', 'emergency'] as const
export type JobPriority = (typeof JOB_PRIORITIES)[number]

export const COST_KINDS = ['labour', 'material', 'callout', 'contractor', 'other'] as const
export type CostKind = (typeof COST_KINDS)[number]

export const PARTY_KINDS = ['staff', 'contractor', 'tenant'] as const
export type PartyKind = (typeof PARTY_KINDS)[number]

export const INSPECTION_KINDS = ['ingoing', 'outgoing', 'routine', 'snag'] as const
export type InspectionKind = (typeof INSPECTION_KINDS)[number]

export const INSPECTION_STATUSES = ['scheduled', 'in_progress', 'complete'] as const
export type InspectionStatus = (typeof INSPECTION_STATUSES)[number]

export const FINDING_CONDITIONS = ['ok', 'wear', 'damage', 'missing', 'na'] as const
export type FindingCondition = (typeof FINDING_CONDITIONS)[number]

export const UNIT_SCHEMES: { value: string; label: string }[] = [
  { value: '', label: 'Default — "Flat 3A" and "3A" are the same unit' },
  { value: 'mixed-use', label: 'Mixed use — "Shop 1" and "Flat 1" stay separate' },
  { value: 'verbatim', label: 'Verbatim — only case/whitespace normalised' },
]

export const EVENT_VISIBILITIES = ['internal', 'public'] as const
export type EventVisibility = (typeof EVENT_VISIBILITIES)[number]

// status → { label, tone } for pills. tone keys map to the CSS variables in
// src/index.css (good/warning/serious/critical/neutral/info).
export const STATUS_TONE: Record<JobStatus, Tone> = {
  reported: 'info',
  triaged: 'neutral',
  assigned: 'neutral',
  in_progress: 'info',
  on_hold: 'warning',
  resolved: 'good',
  closed: 'neutral-muted',
  cancelled: 'neutral-muted',
}

export const PRIORITY_TONE: Record<JobPriority, Tone> = {
  low: 'neutral',
  normal: 'info',
  high: 'warning',
  emergency: 'critical',
}

export const CONDITION_TONE: Record<FindingCondition, Tone> = {
  ok: 'good',
  wear: 'warning',
  damage: 'serious',
  missing: 'critical',
  na: 'neutral-muted',
}

// Mirrors domain.Deteriorated (backend/internal/domain/domain.go) — used to
// flag a worse condition between an ingoing and outgoing inspection finding
// on the same template item. `na` is deliberately unranked: "not applicable"
// is not a point on the scale.
const CONDITION_RANK: Partial<Record<FindingCondition, number>> = { ok: 0, wear: 1, damage: 2, missing: 3 }

export function deteriorated(
  from: string | undefined,
  to: string | undefined,
): { worse: boolean; comparable: boolean } {
  const a = CONDITION_RANK[from as FindingCondition]
  const b = CONDITION_RANK[to as FindingCondition]
  if (a === undefined || b === undefined) return { worse: false, comparable: false }
  return { worse: b > a, comparable: true }
}

export function label(s: string | null | undefined): string {
  if (!s) return '—'
  return s
    .split('_')
    .map((w) => w[0].toUpperCase() + w.slice(1))
    .join(' ')
}
