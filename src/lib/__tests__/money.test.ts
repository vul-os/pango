import { describe, expect, it } from 'vitest'
import { formatMoney, parseMoneyInput, sumMinor, formatMinutes } from '../money'

describe('parseMoneyInput', () => {
  it('parses a plain decimal amount into minor units', () => {
    expect(parseMoneyInput('1234.50')).toBe(123450n)
  })

  it('parses a negative amount — the shape a correction entry takes', () => {
    expect(parseMoneyInput('-99')).toBe(-9900n)
  })

  it('pads a single decimal place', () => {
    expect(parseMoneyInput('10.5')).toBe(1050n)
  })

  it('truncates rather than rounds beyond two decimal places', () => {
    // Mirrors backend/internal/domain/money.go ParseMoney: more precision
    // than the currency has is a mistake at the source, never rounded up.
    expect(parseMoneyInput('1.239')).toBe(123n)
  })

  it('treats a single comma with no dot as a decimal separator', () => {
    expect(parseMoneyInput('45,50')).toBe(4550n)
  })

  it('never produces the classic float drift (1234.55 * 100)', () => {
    // parseFloat('1234.55') * 100 === 123454.99999999999 in JS — the exact
    // failure ParseMoney (and this mirror of it) exists to avoid.
    expect(parseMoneyInput('1234.55')).toBe(123455n)
  })

  it('rejects thousands-comma-plus-dot ambiguity rather than guessing', () => {
    expect(parseMoneyInput('1,234.50')).toBeNull()
  })

  it('rejects blank, sign-only and garbage input', () => {
    expect(parseMoneyInput('')).toBeNull()
    expect(parseMoneyInput('-')).toBeNull()
    expect(parseMoneyInput('abc')).toBeNull()
    expect(parseMoneyInput(null)).toBeNull()
  })
})

describe('formatMoney', () => {
  it('formats minor units as a currency string without floating point', () => {
    const out = formatMoney(123450, 'USD')
    expect(out).toMatch(/1,234\.50/)
  })

  it('formats a negative amount with a visible sign', () => {
    const out = formatMoney(-4500, 'USD')
    expect(out).toMatch(/-/)
  })

  it('falls back to a grouped decimal for an unrecognised currency code rather than throwing', () => {
    expect(() => formatMoney(1000, 'DEMO')).not.toThrow()
    expect(formatMoney(1000, 'DEMO')).toContain('DEMO')
  })
})

describe('sumMinor', () => {
  it('sums amount_minor across entries, including negative corrections', () => {
    const entries = [{ amount_minor: 45000 }, { amount_minor: 28550 }, { amount_minor: -45000 }]
    expect(sumMinor(entries)).toBe(28550n)
  })

  it('returns 0 for an empty ledger', () => {
    expect(sumMinor([])).toBe(0n)
  })
})

describe('formatMinutes', () => {
  it('formats minutes under an hour as Xm', () => {
    expect(formatMinutes(45)).toBe('45m')
  })

  it('formats minutes over an hour as Xh Ym', () => {
    expect(formatMinutes(125)).toBe('2h 5m')
  })

  it('formats a negative correction with a leading sign', () => {
    expect(formatMinutes(-30)).toBe('-30m')
  })
})
