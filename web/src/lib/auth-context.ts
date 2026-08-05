import { createContext, useContext } from 'react'
import type { AuthContextValue } from './api'

/**
 * The auth context and its accessor hook live apart from `<AuthProvider>`
 * (src/lib/auth.jsx) on purpose: react-refresh can only hot-swap a module
 * whose exports are all components, so shipping `useAuth` from the same file
 * as the provider silently disables fast refresh for the whole auth subtree.
 */
export const AuthContext = createContext<AuthContextValue | null>(null)

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}
