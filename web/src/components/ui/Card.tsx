import type { ElementType, HTMLAttributes, ReactNode } from 'react'

interface CardProps extends HTMLAttributes<HTMLElement> {
  children?: ReactNode
  className?: string
  as?: ElementType
}

export default function Card({ children, className = '', as: Comp = 'div', ...props }: CardProps) {
  return (
    <Comp
      className={`rounded-md border border-line bg-surface-raised shadow-e1 ${className}`}
      {...props}
    >
      {children}
    </Comp>
  )
}

interface CardHeaderProps {
  title?: ReactNode
  subtitle?: ReactNode
  actions?: ReactNode
}

export function CardHeader({ title, subtitle, actions }: CardHeaderProps) {
  return (
    <div className="flex items-start justify-between gap-4 border-b border-line px-4 py-3">
      <div className="min-w-0">
        <h2 className="truncate text-sm font-semibold text-ink">{title}</h2>
        {subtitle && <p className="mt-0.5 text-xs text-ink-muted">{subtitle}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  )
}
