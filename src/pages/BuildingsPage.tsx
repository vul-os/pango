import { useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { buildingsApi, type Building } from '../lib/api'
import { useAsync } from '../lib/useAsync'
import { UNIT_SCHEMES } from '../lib/domain'
import Button from '../components/ui/Button'
import Modal from '../components/ui/Modal'
import { Input, Select, FormRow } from '../components/ui/Field'
import { EmptyState, ErrorState, LoadingState, InlineError, Skeleton } from '../components/ui/States'
import { BuildingIcon, PlusIcon } from '../components/icons'

export default function BuildingsPage() {
  const buildingsQ = useAsync(() => buildingsApi.list(), [])
  const [open, setOpen] = useState(false)

  return (
    <div>
      <header className="mb-5 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Buildings</h1>
          <p className="mt-0.5 text-sm text-ink-muted">Every property this organisation manages.</p>
        </div>
        <Button variant="primary" onClick={() => setOpen(true)}>
          <PlusIcon width={16} height={16} />
          Add building
        </Button>
      </header>

      {buildingsQ.error ? (
        <ErrorState message={buildingsQ.error.message} onRetry={buildingsQ.reload} />
      ) : buildingsQ.loading ? (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-28 w-full" />
          ))}
        </div>
      ) : (buildingsQ.data || []).length === 0 ? (
        <EmptyState
          icon={<BuildingIcon width={28} height={28} />}
          title="No buildings yet"
          description="Add the first building this organisation manages — units are created automatically the first time a job or inspection names one."
          action={
            <Button variant="primary" onClick={() => setOpen(true)}>
              Add building
            </Button>
          }
        />
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {buildingsQ.data!.map((b) => (
            <Link
              key={b.id}
              to={`/buildings/${b.id}`}
              className="rounded-md border border-line bg-surface-raised p-4 shadow-e1 transition-shadow hover:shadow-e2"
            >
              <div className="mb-2 flex h-9 w-9 items-center justify-center rounded-sm bg-accent-tint text-accent-ink">
                <BuildingIcon width={18} height={18} />
              </div>
              <p className="text-sm font-semibold text-ink">{b.name}</p>
              <p className="mt-0.5 line-clamp-2 text-xs text-ink-muted">{b.address || 'No address on file'}</p>
              {b.lat != null && b.lon != null ? (
                <p className="tab-num mt-2 text-2xs text-ink-faint">
                  {b.lat.toFixed(4)}, {b.lon.toFixed(4)}
                </p>
              ) : (
                <p className="mt-2 text-2xs text-ink-faint">Not surveyed</p>
              )}
            </Link>
          ))}
        </div>
      )}

      <CreateBuildingModal
        open={open}
        onClose={() => setOpen(false)}
        onCreated={(b) => {
          buildingsQ.setData((d) => [...(d || []), b])
          setOpen(false)
        }}
      />
    </div>
  )
}

interface CreateBuildingModalProps {
  open: boolean
  onClose: () => void
  onCreated: (building: Building) => void
}

function CreateBuildingModal({ open, onClose, onCreated }: CreateBuildingModalProps) {
  const [name, setName] = useState('')
  const [address, setAddress] = useState('')
  const [unitScheme, setUnitScheme] = useState('')
  const [lat, setLat] = useState('')
  const [lon, setLon] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!name.trim()) {
      setError('A building needs a name.')
      return
    }
    setBusy(true)
    setError('')
    try {
      const b = await buildingsApi.create({
        name: name.trim(),
        address: address.trim(),
        unit_scheme: unitScheme,
        lat: lat.trim() ? Number(lat) : null,
        lon: lon.trim() ? Number(lon) : null,
      })
      onCreated(b)
      setName('')
      setAddress('')
      setUnitScheme('')
      setLat('')
      setLon('')
    } catch (err) {
      setError((err instanceof Error && err.message) || 'Could not create the building.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open={open} onClose={onClose} title="Add a building">
      <form onSubmit={submit} className="flex flex-col gap-3.5">
        <InlineError message={error} />
        <FormRow label="Name" htmlFor="b-name" required>
          <Input id="b-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Riverside Court" autoFocus />
        </FormRow>
        <FormRow label="Address" htmlFor="b-address">
          <Input id="b-address" value={address} onChange={(e) => setAddress(e.target.value)} placeholder="14 Riverside Road" />
        </FormRow>
        <FormRow label="Unit numbering" htmlFor="b-scheme" hint="drives how typed labels collapse to one unit">
          <Select id="b-scheme" value={unitScheme} onChange={(e) => setUnitScheme(e.target.value)}>
            {UNIT_SCHEMES.map((s) => (
              <option key={s.value} value={s.value}>
                {s.label}
              </option>
            ))}
          </Select>
        </FormRow>
        <div className="grid grid-cols-2 gap-3">
          <FormRow label="Latitude" htmlFor="b-lat" hint="optional">
            <Input id="b-lat" value={lat} onChange={(e) => setLat(e.target.value)} placeholder="-26.1076" inputMode="decimal" />
          </FormRow>
          <FormRow label="Longitude" htmlFor="b-lon" hint="optional">
            <Input id="b-lon" value={lon} onChange={(e) => setLon(e.target.value)} placeholder="28.0567" inputMode="decimal" />
          </FormRow>
        </div>
        <div className="mt-1 flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            {busy ? 'Adding…' : 'Add building'}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
