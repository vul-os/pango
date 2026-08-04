import Pill from './ui/Pill'
import { STATUS_TONE, PRIORITY_TONE, CONDITION_TONE, label, type JobStatus, type JobPriority, type FindingCondition } from '../lib/domain'

interface StatusPillProps {
  status: JobStatus
  className?: string
}

export function StatusPill({ status, className }: StatusPillProps) {
  return (
    <Pill tone={STATUS_TONE[status] || 'neutral'} className={className}>
      {label(status)}
    </Pill>
  )
}

interface PriorityPillProps {
  priority: JobPriority
  className?: string
}

export function PriorityPill({ priority, className }: PriorityPillProps) {
  return (
    <Pill tone={PRIORITY_TONE[priority] || 'neutral'} dot className={className}>
      {label(priority)}
    </Pill>
  )
}

interface ConditionPillProps {
  condition: FindingCondition
  className?: string
}

export function ConditionPill({ condition, className }: ConditionPillProps) {
  return (
    <Pill tone={CONDITION_TONE[condition] || 'neutral'} className={className}>
      {condition === 'na' ? 'N/A' : label(condition)}
    </Pill>
  )
}

interface CategoryTagProps {
  category?: string | null
}

export function CategoryTag({ category }: CategoryTagProps) {
  if (!category) return null
  return (
    <span className="inline-flex items-center rounded-xs border border-line bg-surface-sunk px-1.5 py-0.5 text-2xs font-medium text-ink-muted">
      #{category}
    </span>
  )
}
