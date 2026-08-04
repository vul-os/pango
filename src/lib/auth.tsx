import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { authApi, ApiError, type AuthOrg, type AuthStatus, type AuthUser, type MeResponse, type RegisterFields } from './api'
import { AuthContext } from './auth-context'

interface AuthProviderProps {
  children?: ReactNode
}

export function AuthProvider({ children }: AuthProviderProps) {
  const [status, setStatus] = useState<AuthStatus>('loading')
  const [user, setUser] = useState<AuthUser | null>(null)
  const [org, setOrg] = useState<AuthOrg | null>(null)

  // `data` is a /auth/me payload, or null for "no session".
  const applySession = useCallback((data: MeResponse | null) => {
    if (data) {
      setUser(data.user)
      setOrg(data.organisation ?? null)
      setStatus('authenticated')
    } else {
      setUser(null)
      setOrg(null)
      setStatus('anonymous')
    }
  }, [])

  const refresh = useCallback(
    () => authApi.me().then(applySession, () => applySession(null)),
    [applySession],
  )

  // Resolve the session once on mount. The state lands in the promise's
  // callbacks rather than in the effect body — an effect that calls setState
  // synchronously drives a second render pass before the browser paints, and
  // React's own guidance is to subscribe here and set state when the external
  // system answers. `cancelled` keeps a provider that unmounted (or logged in)
  // mid-flight from having this initial probe overwrite the newer state.
  useEffect(() => {
    let cancelled = false
    authApi.me().then(
      (data) => {
        if (!cancelled) applySession(data)
      },
      () => {
        if (!cancelled) applySession(null)
      },
    )
    return () => {
      cancelled = true
    }
  }, [applySession])

  const login = useCallback(async (email: string, password: string) => {
    const data = await authApi.login({ email, password })
    setUser(data.user)
    setStatus('authenticated')
    // /auth/login does not return the organisation, only the user + token;
    // fetch it so the shell has an org name to show immediately.
    await refresh()
    return data
  }, [refresh])

  const register = useCallback(async ({ organisation, email, password, name }: RegisterFields) => {
    try {
      const data = await authApi.register({ organisation, email, password, name })
      setUser(data.user)
      setStatus('authenticated')
      await refresh()
      return data
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        // "registration is closed on this node" — a first-run-only state,
        // not a generic failure. Let the caller render it distinctly.
        throw Object.assign(err, { registrationClosed: true })
      }
      throw err
    }
  }, [refresh])

  const logout = useCallback(async () => {
    try {
      await authApi.logout()
    } finally {
      applySession(null)
    }
  }, [applySession])

  const value = useMemo(
    () => ({ status, user, org, login, register, logout, refresh }),
    [status, user, org, login, register, logout, refresh],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
