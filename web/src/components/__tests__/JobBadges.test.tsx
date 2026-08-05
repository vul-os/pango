import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { StatusPill, PriorityPill, ConditionPill, CategoryTag } from '../JobBadges'

describe('JobBadges', () => {
  it('StatusPill renders a human label for a snake_case status', () => {
    render(<StatusPill status="in_progress" />)
    expect(screen.getByText('In Progress')).toBeInTheDocument()
  })

  it('PriorityPill renders emergency distinctly from low', () => {
    const { rerender } = render(<PriorityPill priority="emergency" />)
    expect(screen.getByText('Emergency')).toBeInTheDocument()
    rerender(<PriorityPill priority="low" />)
    expect(screen.getByText('Low')).toBeInTheDocument()
  })

  it('ConditionPill renders "N/A" for the na condition, not the raw code', () => {
    render(<ConditionPill condition="na" />)
    expect(screen.getByText('N/A')).toBeInTheDocument()
    expect(screen.queryByText('na')).not.toBeInTheDocument()
  })

  it('CategoryTag renders nothing for an empty category rather than an empty badge', () => {
    const { container } = render(<CategoryTag category="" />)
    expect(container).toBeEmptyDOMElement()
  })

  it('CategoryTag prefixes the category with #', () => {
    render(<CategoryTag category="plumbing" />)
    expect(screen.getByText('#plumbing')).toBeInTheDocument()
  })
})
