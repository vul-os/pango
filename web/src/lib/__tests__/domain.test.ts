import { describe, expect, it } from 'vitest'
import { deteriorated, nextStatuses } from '../domain'
import { rankByProximity, haversineKm } from '../geo'

describe('deteriorated', () => {
  it('flags ok -> damage as worse', () => {
    expect(deteriorated('ok', 'damage')).toEqual({ worse: true, comparable: true })
  })

  it('does not flag ok -> ok as worse', () => {
    expect(deteriorated('ok', 'ok')).toEqual({ worse: false, comparable: true })
  })

  it('does not flag an improvement as worse', () => {
    expect(deteriorated('damage', 'ok').worse).toBe(false)
  })

  it('treats "na" as unranked — not comparable either direction', () => {
    expect(deteriorated('ok', 'na').comparable).toBe(false)
    expect(deteriorated('na', 'damage').comparable).toBe(false)
  })
})

describe('nextStatuses', () => {
  it('does not offer leaving a closed job except via reopen', () => {
    expect(nextStatuses('closed')).toEqual(['in_progress'])
  })

  it('offers the usual triage path from reported', () => {
    expect(nextStatuses('reported')).toContain('triaged')
    expect(nextStatuses('reported')).toContain('cancelled')
  })
})

describe('rankByProximity', () => {
  const origin = { lat: -26.1076, lon: 28.0567 } // Riverside Court, Johannesburg
  const near = { id: 'a', lat: -26.11, lon: 28.06 }
  const far = { id: 'b', lat: -33.9249, lon: 18.4241 } // Cape Town
  const unsurveyed = { id: 'c', lat: null, lon: null }

  it('sorts nearest first', () => {
    const ranked = rankByProximity([far, near, unsurveyed], origin)
    expect(ranked.map((b) => b.id)).toEqual(['a', 'b', 'c'])
  })

  it('sorts unsurveyed buildings last regardless of order given', () => {
    const ranked = rankByProximity([unsurveyed, near], origin)
    // Optional chaining, not a direct index: noUncheckedIndexedAccess types
    // this access as possibly-undefined; if it ever were, `.toBe('c')` still
    // fails the assertion correctly (undefined !== 'c').
    expect(ranked[ranked.length - 1]?.id).toBe('c')
  })

  it('is a no-op without an origin', () => {
    const list = [far, near]
    expect(rankByProximity(list, null)).toBe(list)
  })
})

describe('haversineKm', () => {
  it('returns ~0 for the same point', () => {
    expect(haversineKm(-26.1, 28.0, -26.1, 28.0)).toBeCloseTo(0, 5)
  })

  it('returns a plausible distance between Johannesburg and Cape Town', () => {
    const km = haversineKm(-26.1076, 28.0567, -33.9249, 18.4241)
    expect(km).toBeGreaterThan(1200)
    expect(km).toBeLessThan(1400)
  })
})
