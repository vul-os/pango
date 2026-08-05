import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { setupUser } from '../../test-support/user'
import { MemoryRouter } from 'react-router-dom'
import LoginPage from '../LoginPage'
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
const mockedLogin = vi.mocked(authApi.login)

function renderLogin() {
  return render(
    <MemoryRouter initialEntries={['/login']}>
      <AuthProvider>
        <LoginPage />
      </AuthProvider>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  mockedMe.mockReset().mockRejectedValue(new ApiError(401, 'authentication required'))
  mockedLogin.mockReset()
})

describe('LoginPage', () => {
  it('renders the sign-in form once the anonymous session check resolves', async () => {
    renderLogin()
    expect(await screen.findByRole('button', { name: /sign in/i })).toBeInTheDocument()
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument()
  })

  it('links to the first-run registration flow', async () => {
    renderLogin()
    expect(await screen.findByRole('link', { name: /set it up/i })).toHaveAttribute('href', '/register')
  })

  it('shows the server error message rather than a generic failure on bad credentials', async () => {
    const user = setupUser()
    mockedLogin.mockRejectedValue(new ApiError(401, 'invalid email or password'))
    renderLogin()

    await user.type(await screen.findByLabelText(/email/i), 'wrong@pango.local')
    await user.type(screen.getByLabelText(/password/i), 'wrongpass')
    await user.click(screen.getByRole('button', { name: /sign in/i }))

    expect(await screen.findByText('invalid email or password')).toBeInTheDocument()
  })

  it('calls the login API with trimmed credentials on submit', async () => {
    const user = setupUser()
    mockedLogin.mockResolvedValue({ token: 't', user: { id: 'u1', name: 'Demo', email: 'demo@pango.local' } })
    renderLogin()

    await user.type(await screen.findByLabelText(/email/i), '  demo@pango.local  ')
    await user.type(screen.getByLabelText(/password/i), 'demopassword')
    await user.click(screen.getByRole('button', { name: /sign in/i }))

    await waitFor(() => expect(mockedLogin).toHaveBeenCalledWith({ email: 'demo@pango.local', password: 'demopassword' }))
  })
})
