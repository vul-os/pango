import type { ReactElement } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useAuth } from '../../lib/auth-context'
import { useTheme } from '../../lib/theme'
import {
  WrenchIcon,
  BuildingIcon,
  ClipboardIcon,
  ChartIcon,
  GearIcon,
  PlusIcon,
  LogoutIcon,
  SunIcon,
  MoonIcon,
  type IconProps,
} from '../icons'

const NAV: { to: string; label: string; icon: (p: IconProps) => ReactElement }[] = [
  { to: '/jobs', label: 'Jobs', icon: WrenchIcon },
  { to: '/buildings', label: 'Buildings', icon: BuildingIcon },
  { to: '/inspections', label: 'Inspections', icon: ClipboardIcon },
  { to: '/reports', label: 'Reports', icon: ChartIcon },
  { to: '/settings', label: 'Settings', icon: GearIcon },
]

function navClass({ isActive }: { isActive: boolean }) {
  return (
    'flex items-center gap-2.5 rounded-sm px-3 py-2 text-sm font-medium transition-colors ' +
    (isActive
      ? 'bg-accent-tint text-accent-ink'
      : 'text-ink-muted hover:bg-surface-sunk hover:text-ink')
  )
}

export default function AppShell() {
  const { user, org, logout } = useAuth()
  const { resolved, cycle } = useTheme()
  const navigate = useNavigate()

  return (
    <div className="flex min-h-screen bg-bg text-ink" data-testid="app-shell">
      <aside className="flex w-56 shrink-0 flex-col border-r border-line bg-surface px-3 py-4">
        <div className="mb-6 flex items-center gap-2 px-1">
          <img src="/logo-mark.svg" alt="" width={26} height={26} className="rounded-[6px]" />
          <div className="min-w-0">
            <p className="truncate text-sm font-semibold leading-tight">Pango</p>
            <p className="truncate text-2xs leading-tight text-ink-faint">{org?.name || ' '}</p>
          </div>
        </div>

        <button
          type="button"
          onClick={() => navigate('/jobs/new')}
          className="mb-5 flex h-9 items-center justify-center gap-1.5 rounded-sm bg-accent text-sm font-medium text-white shadow-e1 transition-colors hover:bg-accent-hover"
        >
          <PlusIcon width={16} height={16} />
          Raise a job
        </button>

        <nav className="flex flex-1 flex-col gap-0.5" aria-label="Primary">
          {NAV.map(({ to, label, icon: Icon }) => (
            <NavLink key={to} to={to} className={navClass}>
              <Icon width={17} height={17} />
              {label}
            </NavLink>
          ))}
        </nav>

        <div className="mt-4 border-t border-line pt-3">
          <button
            type="button"
            onClick={cycle}
            className="mb-1 flex w-full items-center gap-2.5 rounded-sm px-3 py-2 text-sm text-ink-muted hover:bg-surface-sunk hover:text-ink"
          >
            {resolved === 'dark' ? <MoonIcon width={17} height={17} /> : <SunIcon width={17} height={17} />}
            {resolved === 'dark' ? 'Dark' : 'Light'} theme
          </button>
          <div className="flex items-center gap-2 px-3 py-1.5">
            <div className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-surface-sunk text-2xs font-semibold text-ink-muted">
              {(user?.name || user?.email || '?')[0]?.toUpperCase()}
            </div>
            <div className="min-w-0 flex-1">
              <p className="truncate text-xs font-medium leading-tight">{user?.name || user?.email}</p>
            </div>
            <button
              type="button"
              onClick={logout}
              aria-label="Log out"
              title="Log out"
              className="shrink-0 rounded-xs p-1 text-ink-faint hover:bg-surface-sunk hover:text-critical"
            >
              <LogoutIcon width={16} height={16} />
            </button>
          </div>
        </div>
      </aside>

      <main className="min-w-0 flex-1 overflow-y-auto">
        <div className="mx-auto max-w-[1200px] px-6 py-6">
          <Outlet />
        </div>
      </main>
    </div>
  )
}
