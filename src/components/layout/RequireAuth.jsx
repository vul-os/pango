import { Navigate, useLocation } from 'react-router-dom'
import { useAuth } from '../../lib/auth-context.js'
import { LoadingState } from '../ui/States.jsx'

export default function RequireAuth({ children }) {
  const { status } = useAuth()
  const location = useLocation()

  if (status === 'loading') {
    return (
      <div className="flex min-h-screen items-center justify-center bg-bg">
        <LoadingState label="Checking your session…" />
      </div>
    )
  }
  if (status === 'anonymous') {
    return <Navigate to="/login" state={{ from: location }} replace />
  }
  return children
}
