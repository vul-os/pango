import { useState } from 'react'
import { Link, Navigate, useNavigate } from 'react-router-dom'
import { useAuth } from '../lib/auth-context.js'
import AuthLayout from './AuthLayout.jsx'
import { Input, Label } from '../components/ui/Field.jsx'
import Button from '../components/ui/Button.jsx'
import { InlineError } from '../components/ui/States.jsx'

export default function RegisterPage() {
  const { status, register } = useAuth()
  const navigate = useNavigate()
  const [form, setForm] = useState({ organisation: '', name: '', email: '', password: '' })
  const [error, setError] = useState('')
  const [closed, setClosed] = useState(false)
  const [busy, setBusy] = useState(false)

  if (status === 'authenticated') {
    return <Navigate to="/jobs" replace />
  }

  function set(field) {
    return (e) => setForm((f) => ({ ...f, [field]: e.target.value }))
  }

  async function onSubmit(e) {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      await register(form)
      navigate('/jobs', { replace: true })
    } catch (err) {
      if (err.registrationClosed) {
        setClosed(true)
      } else {
        setError(err.message || 'Could not create the organisation.')
      }
    } finally {
      setBusy(false)
    }
  }

  if (closed) {
    return (
      <AuthLayout
        title="Registration is closed"
        subtitle="This PropFix node already belongs to an organisation."
      >
        <p className="text-sm text-ink-muted">
          A PropFix node registers exactly one organisation, on the first account created.
          That has already happened here — ask whoever set this node up for an invitation,
          or sign in if the account is yours.
        </p>
        <Link to="/login" className="mt-4 block">
          <Button variant="primary" className="w-full">
            Go to sign in
          </Button>
        </Link>
      </AuthLayout>
    )
  }

  return (
    <AuthLayout
      title="Set up this node"
      subtitle="Runs once — the first account you create here owns this PropFix node."
      footer={
        <>
          Already set up?{' '}
          <Link to="/login" className="font-medium text-accent hover:text-accent-hover">
            Sign in instead
          </Link>
        </>
      }
    >
      <form onSubmit={onSubmit} className="flex flex-col gap-3.5" noValidate>
        <InlineError message={error} />
        <div>
          <Label htmlFor="organisation">Organisation</Label>
          <Input
            id="organisation"
            required
            value={form.organisation}
            onChange={set('organisation')}
            placeholder="Meridian Property Management"
          />
        </div>
        <div>
          <Label htmlFor="name">Your name</Label>
          <Input id="name" required value={form.name} onChange={set('name')} placeholder="Jordan Naidoo" />
        </div>
        <div>
          <Label htmlFor="email">Email</Label>
          <Input
            id="email"
            type="email"
            autoComplete="username"
            required
            value={form.email}
            onChange={set('email')}
            placeholder="you@example.com"
          />
        </div>
        <div>
          <Label htmlFor="password">Password</Label>
          <Input
            id="password"
            type="password"
            autoComplete="new-password"
            required
            minLength={8}
            value={form.password}
            onChange={set('password')}
            placeholder="At least 8 characters"
          />
        </div>
        <Button type="submit" variant="primary" size="lg" disabled={busy} className="mt-1 w-full">
          {busy ? 'Creating…' : 'Create organisation'}
        </Button>
      </form>
    </AuthLayout>
  )
}
