// Empty and error states. Every list surface in the app renders one of
// these instead of a blank screen when it has nothing, or failed, to show.
import type { ReactNode } from 'react'
import Button from './Button'

interface EmptyStateProps {
  icon?: ReactNode
  title?: ReactNode
  description?: ReactNode
  action?: ReactNode
}

export function EmptyState({ icon, title, description, action }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 rounded-md border border-dashed border-line px-6 py-14 text-center">
      {icon && <div className="mb-1 text-ink-faint" aria-hidden="true">{icon}</div>}
      <p className="text-sm font-medium text-ink">{title}</p>
      {description && <p className="max-w-sm text-xs text-ink-muted">{description}</p>}
      {action && <div className="mt-3">{action}</div>}
    </div>
  )
}

interface ErrorStateProps {
  title?: ReactNode
  message?: ReactNode
  onRetry?: () => void
}

export function ErrorState({ title = 'Something went wrong', message, onRetry }: ErrorStateProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 rounded-md border border-critical/30 bg-critical-bg px-6 py-14 text-center">
      <p className="text-sm font-medium text-critical">{title}</p>
      {message && <p className="max-w-sm text-xs text-critical/80">{message}</p>}
      {onRetry && (
        <div className="mt-3">
          <Button variant="secondary" size="sm" onClick={onRetry}>
            Try again
          </Button>
        </div>
      )}
    </div>
  )
}

interface InlineErrorProps {
  message?: ReactNode
}

export function InlineError({ message }: InlineErrorProps) {
  if (!message) return null
  return (
    <div role="alert" className="rounded-sm border border-critical/30 bg-critical-bg px-3 py-2 text-xs text-critical">
      {message}
    </div>
  )
}

interface SpinnerProps {
  className?: string
}

export function Spinner({ className = '' }: SpinnerProps) {
  return (
    <svg
      className={`animate-spin text-ink-faint ${className}`}
      width="18"
      height="18"
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
    >
      <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" opacity="0.25" />
      <path d="M22 12a10 10 0 0 0-10-10" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
    </svg>
  )
}

interface LoadingStateProps {
  label?: ReactNode
}

export function LoadingState({ label = 'Loading…' }: LoadingStateProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-14 text-ink-faint">
      <Spinner />
      <p className="text-xs">{label}</p>
    </div>
  )
}

interface SkeletonProps {
  className?: string
}

export function Skeleton({ className = '' }: SkeletonProps) {
  return <div className={`skeleton rounded-sm bg-surface-sunk ${className}`} />
}
