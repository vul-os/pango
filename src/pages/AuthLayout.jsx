export default function AuthLayout({ title, subtitle, children, footer }) {
  return (
    <div className="flex min-h-screen items-center justify-center bg-bg px-4 py-10">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex flex-col items-center text-center">
          <img src="/logo-mark.svg" alt="Pango" width={44} height={44} className="mb-3 rounded-[10px]" />
          <h1 className="text-xl font-semibold text-ink">{title}</h1>
          {subtitle && <p className="mt-1.5 text-sm text-ink-muted">{subtitle}</p>}
        </div>
        <div className="rounded-md border border-line bg-surface-raised p-5 shadow-e1">{children}</div>
        {footer && <div className="mt-4 text-center text-xs text-ink-muted">{footer}</div>}
      </div>
    </div>
  )
}
