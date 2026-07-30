import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { setupUser } from '../../test-support/user.js'
import { MemoryRouter } from 'react-router-dom'
import RegisterPage from '../RegisterPage.jsx'
import { AuthProvider } from '../../lib/auth.jsx'

vi.mock('../../lib/api.js', () => ({
  authApi: { me: vi.fn(), login: vi.fn(), register: vi.fn(), logout: vi.fn() },
  ApiError: class ApiError extends Error {
    constructor(status, message) {
      super(message)
      this.status = status
    }
  },
}))
import { authApi, ApiError } from '../../lib/api.js'

function renderRegister() {
  return render(
    <MemoryRouter initialEntries={['/register']}>
      <AuthProvider>
        <RegisterPage />
      </AuthProvider>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  authApi.me.mockReset().mockRejectedValue(new ApiError(401, 'authentication required'))
  authApi.register.mockReset()
})

describe('RegisterPage', () => {
  it('renders the first-run setup form', async () => {
    renderRegister()
    expect(await screen.findByRole('button', { name: /create organisation/i })).toBeInTheDocument()
  })

  it('shows a dedicated "registration closed" state on a 403, not a generic error', async () => {
    const user = setupUser()
    authApi.register.mockRejectedValue(new ApiError(403, 'registration is closed on this node'))
    renderRegister()

    await user.type(await screen.findByLabelText(/organisation/i), 'Meridian Property Management')
    await user.type(screen.getByLabelText(/your name/i), 'Jordan Naidoo')
    await user.type(screen.getByLabelText(/^email$/i), 'jordan@propfix.local')
    await user.type(screen.getByLabelText(/^password$/i), 'a-strong-password')
    await user.click(screen.getByRole('button', { name: /create organisation/i }))

    expect(await screen.findByText(/registration is closed/i)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /go to sign in/i })).toHaveAttribute('href', '/login')
    // It must not be rendered as a plain inline form error.
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('surfaces a non-403 failure as an ordinary inline error and keeps the form', async () => {
    const user = setupUser()
    authApi.register.mockRejectedValue(new ApiError(400, 'organisation name is required'))
    renderRegister()

    await user.type(await screen.findByLabelText(/organisation/i), 'X')
    await user.type(screen.getByLabelText(/your name/i), 'Jordan')
    await user.type(screen.getByLabelText(/^email$/i), 'jordan@propfix.local')
    await user.type(screen.getByLabelText(/^password$/i), 'a-strong-password')
    await user.click(screen.getByRole('button', { name: /create organisation/i }))

    expect(await screen.findByText('organisation name is required')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /create organisation/i })).toBeInTheDocument()
  })
})
