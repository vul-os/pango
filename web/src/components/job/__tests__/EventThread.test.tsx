import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { setupUser } from '../../../test-support/user'
import EventThread from '../EventThread'

vi.mock('../../../lib/api', () => ({
  jobsApi: { addEvent: vi.fn() },
}))
import { jobsApi, type JobEvent, type Party } from '../../../lib/api'

const mockedAddEvent = vi.mocked(jobsApi.addEvent)

const parties: Party[] = [{ id: 'p1', name: 'Thabo Nkosi', kind: 'staff', email: '', phone: '', pubkey: '' }]

const events: JobEvent[] = [
  { id: 'e1', kind: 'note', body: 'Internal-only triage note', actor_party_id: 'p1', visibility: 'internal', created_at: '2026-01-01T00:00:00Z' },
  { id: 'e2', kind: 'note', body: 'Contractor booked for Thursday', actor_party_id: 'p1', visibility: 'public', created_at: '2026-01-02T00:00:00Z' },
]

beforeEach(() => {
  mockedAddEvent.mockReset()
})

describe('EventThread', () => {
  it('shows both internal and tenant-visible events under "All"', () => {
    render(<EventThread jobId="j1" events={events} parties={parties} onPosted={() => {}} />)
    expect(screen.getByText('Internal-only triage note')).toBeInTheDocument()
    expect(screen.getByText('Contractor booked for Thursday')).toBeInTheDocument()
  })

  it('the tenant-visible filter hides internal-only events — this is the tenant\'s view', async () => {
    const user = setupUser()
    render(<EventThread jobId="j1" events={events} parties={parties} onPosted={() => {}} />)

    await user.click(screen.getByRole('button', { name: /tenant-visible \(1\)/i }))

    expect(screen.queryByText('Internal-only triage note')).not.toBeInTheDocument()
    expect(screen.getByText('Contractor booked for Thursday')).toBeInTheDocument()
  })

  it('defaults a new post to internal, and posting requires an explicit switch to make it tenant-visible', async () => {
    const user = setupUser()
    mockedAddEvent.mockResolvedValue({
      id: 'e3',
      kind: 'note',
      body: 'New note',
      actor_party_id: '',
      visibility: 'internal',
      created_at: '2026-01-03T00:00:00Z',
    })
    render(<EventThread jobId="j1" events={[]} parties={parties} onPosted={() => {}} />)

    await user.type(screen.getByPlaceholderText(/add an update/i), 'New note')
    await user.click(screen.getByRole('button', { name: /post update/i }))

    expect(mockedAddEvent).toHaveBeenCalledWith('j1', expect.objectContaining({ visibility: 'internal', body: 'New note' }))
  })

  it('posts as tenant-visible when the toggle is switched, and warns before doing so', async () => {
    const user = setupUser()
    mockedAddEvent.mockResolvedValue({
      id: 'e4',
      kind: 'note',
      body: 'Visible to tenant',
      actor_party_id: '',
      visibility: 'public',
      created_at: '2026-01-04T00:00:00Z',
    })
    render(<EventThread jobId="j1" events={[]} parties={parties} onPosted={() => {}} />)

    await user.click(screen.getByRole('button', { name: /^tenant-visible$/i }))
    expect(screen.getByText(/visible to the tenant on this job/i)).toBeInTheDocument()

    await user.type(screen.getByPlaceholderText(/add an update/i), 'Visible to tenant')
    await user.click(screen.getByRole('button', { name: /post update/i }))

    expect(mockedAddEvent).toHaveBeenCalledWith('j1', expect.objectContaining({ visibility: 'public' }))
  })

  it('refuses to post an empty update', async () => {
    const user = setupUser()
    render(<EventThread jobId="j1" events={[]} parties={parties} onPosted={() => {}} />)
    await user.click(screen.getByRole('button', { name: /post update/i }))
    expect(mockedAddEvent).not.toHaveBeenCalled()
    expect(screen.getByRole('alert')).toBeInTheDocument()
  })
})
