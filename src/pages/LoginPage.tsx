import { useState, type FormEvent } from 'react'
import { Link, Navigate, useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '../lib/auth-context'
import { ApiError } from '../lib/api'
import AuthLayout from './AuthLayout'
import { Input, Label } from '../components/ui/Field'
import Button from '../components/ui/Button'
import { InlineError } from '../components/ui/States'

export default function LoginPage() {
  const { status, login } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  if (status === 'authenticated') {
    return <Navigate to="/jobs" replace />
  }

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      await login(email.trim(), password)
      const dest = location.state?.from?.pathname || '/jobs'
      navigate(dest, { replace: true })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not sign in.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthLayout
      title="Sign in to Pango"
      subtitle="Your building's own maintenance node."
      footer={
        <>
          First time on this node?{' '}
          <Link to="/register" className="font-medium text-accent hover:text-accent-hover">
            Set it up
          </Link>
        </>
      }
    >
      <form onSubmit={onSubmit} className="flex flex-col gap-3.5" noValidate>
        <InlineError message={error} />
        <div>
          <Label htmlFor="email">Email</Label>
          <Input
            id="email"
            type="email"
            autoComplete="username"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="you@example.com"
          />
        </div>
        <div>
          <Label htmlFor="password">Password</Label>
          <Input
            id="password"
            type="password"
            autoComplete="current-password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="••••••••"
          />
        </div>
        <Button type="submit" variant="primary" size="lg" disabled={busy} className="mt-1 w-full">
          {busy ? 'Signing in…' : 'Sign in'}
        </Button>
      </form>
    </AuthLayout>
  )
}
