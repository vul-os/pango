import type { ReactNode } from 'react'

// Status/priority/condition pills. `tone` picks a fixed status colour — never
// a data-series colour — and always pairs colour with text (never colour
// alone), per the dataviz skill's status-palette rule.
const toneClasses = {
  good: 'bg-good-bg text-good',
  warning: 'bg-warning-bg text-warning',
  serious: 'bg-serious-bg text-serious',
  critical: 'bg-critical-bg text-critical',
  info: 'bg-accent-tint text-accent-ink',
  neutral: 'bg-surface-sunk text-ink-muted border border-line',
  'neutral-muted': 'bg-transparent text-ink-faint border border-line',
}

export type Tone = keyof typeof toneClasses

interface PillProps {
  tone?: Tone
  children?: ReactNode
  className?: string
  dot?: boolean
}

export default function Pill({ tone = 'neutral', children, className = '', dot = false }: PillProps) {
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-xs px-2 py-0.5 text-2xs font-medium uppercase tracking-wide ${toneClasses[tone] || toneClasses.neutral} ${className}`}
    >
      {dot && <span className="h-1.5 w-1.5 rounded-full bg-current" aria-hidden="true" />}
      {children}
    </span>
  )
}
