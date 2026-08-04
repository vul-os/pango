// Money is int64 minor units everywhere the API returns it (docs/ARCHITECTURE.md
// §3/§6). This module is the one place that turns minor units into a string —
// nowhere else in src/ should divide an amount by 100 by hand.
//
// The backend defaults an unset currency to "ZAR" (backend/internal/repo/
// cost.go), so that is the fallback used here too when a caller has no
// currency to hand — most importantly the per-building/per-unit/per-job report
// aggregates, which sum across cost entries but do not carry a currency field
// back (backend/internal/report/report.go). A single-currency org (the normal
// case) reports correctly either way; a multi-currency org would need the
// backend to expose the currency split, which is outside this frontend's
// remit — see the API gaps noted in the build report.
export const DEFAULT_CURRENCY = 'ZAR'

const formatterCache = new Map<string, Intl.NumberFormat | null>()

function formatterFor(currency: string | null | undefined): Intl.NumberFormat | null {
  const key = currency || DEFAULT_CURRENCY
  if (!formatterCache.has(key)) {
    try {
      formatterCache.set(
        key,
        new Intl.NumberFormat(undefined, {
          style: 'currency',
          currency: key,
          currencyDisplay: 'narrowSymbol',
        }),
      )
    } catch {
      // Not a recognised ISO 4217 code (a demo/typo currency) — fall back to
      // a plain grouped decimal with the code suffixed, rather than throwing
      // and blanking the screen.
      formatterCache.set(key, null)
    }
  }
  return formatterCache.get(key) ?? null
}

/** Format int64 minor units as a localized currency string. Never floats. */
export function formatMoney(minor: number | bigint, currency: string | null | undefined = DEFAULT_CURRENCY): string {
  const n = typeof minor === 'bigint' ? minor : BigInt(Math.trunc(Number(minor) || 0))
  const whole = n / 100n
  const fraction = (n < 0n ? -n : n) % 100n
  const fmt = formatterFor(currency)
  if (fmt) {
    // Intl.NumberFormat wants a JS number; minor units are always well within
    // safe-integer range for property maintenance amounts, so the precision
    // loss floats would otherwise cause never materialises here — but we
    // still build the number from the exact major/minor split, not division.
    return fmt.format(Number(n) / 100)
  }
  const sign = n < 0n ? '-' : ''
  return `${sign}${whole < 0n ? -whole : whole}.${fraction.toString().padStart(2, '0')} ${currency || DEFAULT_CURRENCY}`
}

/**
 * Sum an array of entries carrying `amount_minor` (bigint-safe).
 *
 * `T` is intentionally unconstrained — a named interface like `JobCost` has
 * no index signature of its own, so requiring `T extends Record<string,
 * unknown>` would reject every real caller. The dynamic-by-name field access
 * is cast at the one line that actually does it instead.
 */
export function sumMinor<T>(entries: readonly T[] | null | undefined, field = 'amount_minor'): bigint {
  let total = 0n
  for (const e of entries || []) {
    total += BigInt(((e as Record<string, unknown>)[field] as number | bigint | string | undefined) ?? 0)
  }
  return total
}

/**
 * Parse a decimal amount typed by a person ("1234.50", "-99", "45,50") into
 * integer minor units — mirroring backend/internal/domain/money.go's
 * ParseMoney digit-by-digit approach (never `parseFloat(x) * 100`, which
 * drifts by a cent on inputs like 1234.55). Returns null, not 0, for
 * unparseable input so a blank/invalid amount cannot silently post as free.
 */
export function parseMoneyInput(raw: string | number | null | undefined): bigint | null {
  if (raw == null) return null
  let s = String(raw).trim().replace(/\s/g, '')
  if (s === '') return null

  let neg = false
  if (s[0] === '-') {
    neg = true
    s = s.slice(1)
  } else if (s[0] === '+') {
    s = s.slice(1)
  }
  if (s === '') return null

  // A single comma with no dot is a decimal separator (EU style); otherwise
  // commas are rejected rather than silently treated as thousands grouping,
  // which is how a mistyped amount would become 100x too large.
  if (s.includes(',')) {
    if (s.includes('.') || (s.match(/,/g) || []).length > 1) return null
    s = s.replace(',', '.')
  }

  const parts = s.split('.')
  if (parts.length > 2) return null
  const whole = parts[0] || '0'
  let frac = parts[1] || ''
  if (!/^\d+$/.test(whole)) return null
  if (frac !== '' && !/^\d+$/.test(frac)) return null
  if (frac.length > 2) frac = frac.slice(0, 2)
  frac = frac.padEnd(2, '0')

  const total = BigInt(whole) * 100n + BigInt(frac)
  return neg ? -total : total
}

/** Format a minutes count as "Xh Ym". */
export function formatMinutes(minutes: number | string | null | undefined): string {
  const m = Number(minutes) || 0
  const neg = m < 0
  const abs = Math.abs(m)
  const h = Math.floor(abs / 60)
  const mm = abs % 60
  const out = h > 0 ? `${h}h ${mm}m` : `${mm}m`
  return neg ? `-${out}` : out
}
