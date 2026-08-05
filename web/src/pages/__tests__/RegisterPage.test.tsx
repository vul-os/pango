import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { setupUser } from '../../test-support/user'
import { MemoryRouter } from 'react-router-dom'
import RegisterPage from '../RegisterPage'
import { AuthProvider } from '../../lib/auth'

vi.mock('../../lib/api', () => ({
  authApi: { me: vi.fn(), login: vi.fn(), register: vi.fn(), logout: vi.fn() },
  ApiError: class ApiError extends Error {
    status: number
    constructor(status: number, message: string) {
      super(message)
      this.status = status
    }
  },
}))
import { authApi, ApiError } from '../../lib/api'

const mockedMe = vi.mocked(authApi.me)
const mockedRegister = vi.mocked(authApi.register)

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
  mockedMe.mockReset().mockRejectedValue(new ApiError(401, 'authentication required'))
  mockedRegister.mockReset()
})

describe('RegisterPage', () => {
  it('renders the first-run setup form', async () => {
    renderRegister()
    expect(await screen.findByRole('button', { name: /create organisation/i })).toBeInTheDocument()
  })

  it('shows a dedicated "registration closed" state on a 403, not a generic error', async () => {
    const user = setupUser()
    mockedRegister.mockRejectedValue(new ApiError(403, 'registration is closed on this node'))
    renderRegister()

    await user.type(await screen.findByLabelText(/organisation/i), 'Meridian Property Management')
    await user.type(screen.getByLabelText(/your name/i), 'Jordan Naidoo')
    await user.type(screen.getByLabelText(/^email$/i), 'jordan@pango.local')
    await user.type(screen.getByLabelText(/^password$/i), 'a-strong-password')
    await user.click(screen.getByRole('button', { name: /create organisation/i }))

    expect(await screen.findByText(/registration is closed/i)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /go to sign in/i })).toHaveAttribute('href', '/login')
    // It must not be rendered as a plain inline form error.
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('surfaces a non-403 failure as an ordinary inline error and keeps the form', async () => {
    const user = setupUser()
    mockedRegister.mockRejectedValue(new ApiError(400, 'organisation name is required'))
    renderRegister()

    await user.type(await screen.findByLabelText(/organisation/i), 'X')
    await user.type(screen.getByLabelText(/your name/i), 'Jordan')
    await user.type(screen.getByLabelText(/^email$/i), 'jordan@pango.local')
    await user.type(screen.getByLabelText(/^password$/i), 'a-strong-password')
    await user.click(screen.getByRole('button', { name: /create organisation/i }))

    expect(await screen.findByText('organisation name is required')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /create organisation/i })).toBeInTheDocument()
  })
})
