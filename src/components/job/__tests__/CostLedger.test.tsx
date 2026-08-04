import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { setupUser } from '../../../test-support/user'
import CostLedger from '../CostLedger'

vi.mock('../../../lib/api', () => ({
  jobsApi: { addCost: vi.fn() },
}))
import { jobsApi, type JobCost } from '../../../lib/api'

// The real jobsApi.addCost is a typed fetch wrapper, not a Vitest mock — this
// is the same function vi.mock() replaced above, just seen through a type
// that knows about .mockReset()/.mockResolvedValue()/.mock.calls.
const mockedAddCost = vi.mocked(jobsApi.addCost)

const baseCosts: JobCost[] = [
  { id: 'c1', kind: 'callout', description: 'Call-out fee', amount_minor: 45000, currency: 'ZAR', created_at: '2026-01-01T00:00:00Z' },
]

beforeEach(() => {
  mockedAddCost.mockReset()
})

describe('CostLedger', () => {
  it('renders the running total and existing entries, with no edit affordance anywhere', () => {
    render(<CostLedger jobId="j1" costs={baseCosts} parties={[]} onAdded={() => {}} />)
    expect(screen.getByText('Call-out fee')).toBeInTheDocument()
    // Append-only ledger: docs/ARCHITECTURE.md §6 — there must be no edit
    // control, anywhere, for an existing entry.
    expect(screen.queryByText(/^edit$/i)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /edit/i })).not.toBeInTheDocument()
  })

  it('shows an empty state with no entries', () => {
    render(<CostLedger jobId="j1" costs={[]} parties={[]} onAdded={() => {}} />)
    expect(screen.getByText(/no costs recorded/i)).toBeInTheDocument()
  })

  it('submits a new cost as integer minor units, never a float amount', async () => {
    const user = setupUser()
    mockedAddCost.mockResolvedValue({
      id: 'c2',
      kind: 'material',
      description: 'Mixer cartridge',
      amount_minor: 28550,
      currency: 'ZAR',
      created_at: '2026-01-02T00:00:00Z',
    })
    const onAdded = vi.fn()
    render(<CostLedger jobId="j1" costs={[]} parties={[]} onAdded={onAdded} />)

    await user.click(screen.getByRole('button', { name: /add cost/i }))
    await user.type(screen.getByPlaceholderText('Description'), 'Mixer cartridge')
    await user.type(screen.getByPlaceholderText(/450\.00/), '285.50')
    await user.click(screen.getByRole('button', { name: /^add entry$/i }))

    expect(mockedAddCost).toHaveBeenCalledTimes(1)
    const [, body] = mockedAddCost.mock.calls[0]
    expect(body.amount_minor).toBe(28550)
    expect(Number.isInteger(body.amount_minor)).toBe(true)
    expect(onAdded).toHaveBeenCalled()
  })

  it('accepts a negative amount as a correction rather than offering to edit the original', async () => {
    const user = setupUser()
    mockedAddCost.mockResolvedValue({
      id: 'c3',
      kind: 'callout',
      description: 'Call-out waived',
      amount_minor: -45000,
      currency: 'ZAR',
      created_at: '2026-01-03T00:00:00Z',
    })
    render(<CostLedger jobId="j1" costs={baseCosts} parties={[]} onAdded={() => {}} />)

    await user.click(screen.getByRole('button', { name: /add cost/i }))
    await user.type(screen.getByPlaceholderText('Description'), 'Call-out waived')
    await user.type(screen.getByPlaceholderText(/450\.00/), '-450.00')
    await user.click(screen.getByRole('button', { name: /^add entry$/i }))

    const [, body] = mockedAddCost.mock.calls[0]
    expect(body.amount_minor).toBe(-45000)
  })

  it('rejects a zero amount client-side without calling the API', async () => {
    const user = setupUser()
    render(<CostLedger jobId="j1" costs={[]} parties={[]} onAdded={() => {}} />)

    await user.click(screen.getByRole('button', { name: /add cost/i }))
    await user.type(screen.getByPlaceholderText(/450\.00/), '0')
    await user.click(screen.getByRole('button', { name: /^add entry$/i }))

    expect(mockedAddCost).not.toHaveBeenCalled()
    expect(screen.getByRole('alert')).toHaveTextContent(/cannot be zero|valid amount/i)
  })
})
