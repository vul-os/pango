import { useState, type FormEvent } from 'react'
import { jobsApi, type CostKind, type JobCost, type Party } from '../../lib/api'
import { formatMoney, parseMoneyInput, sumMinor, DEFAULT_CURRENCY } from '../../lib/money'
import { formatDate } from '../../lib/format'
import { COST_KINDS, label } from '../../lib/domain'
import { Input, Select } from '../ui/Field'
import Button from '../ui/Button'
import { InlineError, EmptyState } from '../ui/States'

const CURRENCIES = ['ZAR', 'USD', 'GBP', 'EUR']

interface CostLedgerProps {
  jobId: string
  costs: JobCost[]
  parties: Party[]
  onAdded: (cost: JobCost) => void
}

export default function CostLedger({ jobId, costs, parties, onAdded }: CostLedgerProps) {
  const [open, setOpen] = useState(false)
  const [kind, setKind] = useState<CostKind>('material')
  const [description, setDescription] = useState('')
  const [amount, setAmount] = useState('')
  const [currency, setCurrency] = useState(DEFAULT_CURRENCY)
  const [partyId, setPartyId] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const total = sumMinor(costs)
  const currencyInUse = costs[0]?.currency || DEFAULT_CURRENCY

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const minor = parseMoneyInput(amount)
    if (minor === null) {
      setError('Enter a valid amount, e.g. 450.00 — use a leading "-" for a correction.')
      return
    }
    if (minor === 0n) {
      setError('A cost entry cannot be zero — that is what the ledger rejects corrections-as-comments with.')
      return
    }
    setBusy(true)
    setError('')
    try {
      const created = await jobsApi.addCost(jobId, {
        kind,
        description: description.trim(),
        amount_minor: Number(minor),
        currency,
        party_id: partyId,
      })
      onAdded(created)
      setDescription('')
      setAmount('')
      setOpen(false)
    } catch (err) {
      setError((err instanceof Error && err.message) || 'Could not add the cost entry.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div>
      <div className="mb-3 flex items-center justify-between">
        <div>
          <h3 className="text-sm font-semibold text-ink">Costs</h3>
          <p className="tab-num text-xs text-ink-muted">{formatMoney(total, currencyInUse)} total</p>
        </div>
        <Button variant="secondary" size="sm" onClick={() => setOpen((v) => !v)}>
          {open ? 'Cancel' : 'Add cost'}
        </Button>
      </div>

      {open && (
        // submit catches its own errors into `error`, rendered via
        // InlineError below — this wrap only discards the resolved
        // Promise<void>.
        <form onSubmit={(e) => { void submit(e) }} className="mb-3 rounded-md border border-line bg-surface-sunk/60 p-3">
          <InlineError message={error} />
          <div className="mb-2 grid grid-cols-2 gap-2">
            <Select value={kind} onChange={(e) => setKind(e.target.value as CostKind)}>
              {COST_KINDS.map((k) => (
                <option key={k} value={k}>
                  {label(k)}
                </option>
              ))}
            </Select>
            <Select value={partyId} onChange={(e) => setPartyId(e.target.value)}>
              <option value="">No party</option>
              {parties.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </Select>
          </div>
          <Input
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Description"
            className="mb-2 bg-surface-raised"
          />
          <div className="mb-2 flex gap-2">
            <Input
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              placeholder="450.00 or -45.00 for a correction"
              inputMode="decimal"
              className="flex-1 bg-surface-raised"
            />
            <Select value={currency} onChange={(e) => setCurrency(e.target.value)} className="w-24 bg-surface-raised">
              {CURRENCIES.map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </Select>
          </div>
          <p className="mb-2 text-2xs text-ink-faint">
            This ledger is append-only — there is no edit. A mistake is corrected with a new negative entry, not by
            changing this one.
          </p>
          <Button type="submit" variant="primary" size="sm" disabled={busy}>
            {busy ? 'Adding…' : 'Add entry'}
          </Button>
        </form>
      )}

      {costs.length === 0 ? (
        <EmptyState title="No costs recorded" description="Every entry stays forever — corrections are new negative entries." />
      ) : (
        <ul className="divide-y divide-line rounded-md border border-line">
          {costs.map((c) => (
            <li key={c.id} className="flex items-center justify-between gap-3 px-3 py-2">
              <div className="min-w-0">
                <p className="truncate text-sm text-ink">{c.description || label(c.kind)}</p>
                <p className="text-2xs text-ink-faint">
                  {label(c.kind)} · {formatDate(c.created_at)}
                </p>
              </div>
              <span className={`tab-num shrink-0 text-sm font-medium ${c.amount_minor < 0 ? 'text-critical' : 'text-ink'}`}>
                {formatMoney(c.amount_minor, c.currency)}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
