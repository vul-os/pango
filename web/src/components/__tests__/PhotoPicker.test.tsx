import { beforeAll, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { setupUser } from '../../test-support/user'
import PhotoPicker from '../PhotoPicker'

// jsdom has no createObjectURL/revokeObjectURL implementation.
beforeAll(() => {
  globalThis.URL.createObjectURL = vi.fn(() => 'blob:mock')
  globalThis.URL.revokeObjectURL = vi.fn()
})

describe('PhotoPicker', () => {
  it('is explicit that nothing is uploaded — there is no attachments API yet (docs/ARCHITECTURE.md §13)', () => {
    render(<PhotoPicker files={[]} onChange={() => {}} />)
    expect(screen.getByText(/isn.t wired up on the backend yet/i)).toBeInTheDocument()
  })

  it('stages a selected file and calls onChange with it, without pretending to upload', async () => {
    const user = setupUser()
    const onChange = vi.fn()
    const { container } = render(<PhotoPicker files={[]} onChange={onChange} />)

    const file = new File(['x'], 'leak.png', { type: 'image/png' })
    const input = container.querySelector('input[type="file"]') as HTMLInputElement
    await user.upload(input, file)

    expect(onChange).toHaveBeenCalledWith([file])
  })

  it('renders a remove control for each staged photo', () => {
    const file = new File(['x'], 'leak.png', { type: 'image/png' })
    render(<PhotoPicker files={[file]} onChange={() => {}} />)
    expect(screen.getByRole('button', { name: /remove photo/i })).toBeInTheDocument()
    expect(screen.getByText(/1 photo staged on this device only/i)).toBeInTheDocument()
  })
})
