import { forwardRef, type ButtonHTMLAttributes } from 'react'

const sizeClasses = {
  sm: 'h-7 px-2.5 text-xs gap-1.5',
  md: 'h-9 px-3.5 text-sm gap-2',
  lg: 'h-10 px-4 text-base gap-2',
}

const base =
  'inline-flex items-center justify-center font-medium rounded-sm whitespace-nowrap select-none ' +
  'transition-colors duration-150 ease-out ' +
  'disabled:opacity-50 disabled:cursor-not-allowed ' +
  'focus-visible:outline-none focus-visible:shadow-focus'

const variantClasses = {
  primary: 'bg-accent text-white shadow-e1 hover:bg-accent-hover active:bg-accent-press',
  secondary: 'bg-surface-raised text-ink border border-line hover:border-line-strong hover:bg-surface-sunk',
  ghost: 'bg-transparent text-ink-muted hover:bg-surface-sunk hover:text-ink',
  destructive: 'bg-critical-bg text-critical border border-transparent hover:bg-critical hover:text-white',
}

export type ButtonVariant = keyof typeof variantClasses
export type ButtonSize = keyof typeof sizeClasses

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  size?: ButtonSize
}

const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = 'secondary', size = 'md', className = '', type = 'button', ...props },
  ref,
) {
  return (
    <button
      ref={ref}
      type={type}
      className={`${base} ${sizeClasses[size]} ${variantClasses[variant]} ${className}`}
      {...props}
    />
  )
})

export default Button
