// Field primitives: Label + Input/Select/Textarea, sharing one visual
// language so a form never mixes two input heights or radii.
import {
  forwardRef,
  type InputHTMLAttributes,
  type LabelHTMLAttributes,
  type ReactNode,
  type SelectHTMLAttributes,
  type TextareaHTMLAttributes,
} from 'react'

interface LabelProps extends LabelHTMLAttributes<HTMLLabelElement> {
  children?: ReactNode
  hint?: ReactNode
  required?: boolean
}

export function Label({ children, htmlFor, hint, required }: LabelProps) {
  return (
    <label htmlFor={htmlFor} className="mb-1.5 flex items-baseline justify-between text-xs font-medium text-ink-muted">
      <span>
        {children}
        {required && <span className="ml-0.5 text-critical">*</span>}
      </span>
      {hint && <span className="text-2xs font-normal text-ink-faint">{hint}</span>}
    </label>
  )
}

const inputBase =
  'w-full rounded-sm border border-line bg-surface-raised px-3 text-sm text-ink placeholder:text-ink-faint ' +
  'transition-colors duration-150 outline-none ' +
  'focus:border-accent focus:shadow-focus ' +
  'disabled:opacity-50 disabled:cursor-not-allowed'

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(function Input(
  { className = '', ...props },
  ref,
) {
  return <input ref={ref} className={`${inputBase} h-9 ${className}`} {...props} />
})

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement>>(
  function Textarea({ className = '', ...props }, ref) {
    return <textarea ref={ref} className={`${inputBase} min-h-20 py-2 ${className}`} {...props} />
  },
)

export const Select = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement>>(function Select(
  { className = '', children, ...props },
  ref,
) {
  return (
    <select ref={ref} className={`${inputBase} h-9 pr-8 ${className}`} {...props}>
      {children}
    </select>
  )
})

interface FieldErrorProps {
  children?: ReactNode
}

export function FieldError({ children }: FieldErrorProps) {
  if (!children) return null
  return <p className="mt-1.5 text-xs text-critical">{children}</p>
}

interface FormRowProps {
  label?: ReactNode
  htmlFor?: string
  hint?: ReactNode
  required?: boolean
  error?: ReactNode
  children?: ReactNode
}

export function FormRow({ label: text, htmlFor, hint, required, error, children }: FormRowProps) {
  return (
    <div>
      <Label htmlFor={htmlFor} hint={hint} required={required}>
        {text}
      </Label>
      {children}
      <FieldError>{error}</FieldError>
    </div>
  )
}
