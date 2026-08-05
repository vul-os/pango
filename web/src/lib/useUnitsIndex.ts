import { useEffect, useState } from 'react'
import { buildingsApi, type Unit } from './api'

/**
 * The API has no global "list units" endpoint — units are listed per
 * building (backend/internal/api/api.go: GET /buildings/{id}/units) and a
 * job only carries `unit_id`, not a label (backend/internal/api/jobs.go).
 * To show a unit's label anywhere a job list spans multiple buildings, this
 * fetches units for every building referenced and indexes them by id.
 */
// Shared empty result, so "no buildings" returns a stable identity that
// callers can safely use as a useMemo/useEffect dependency.
const EMPTY: Map<string, Unit> = new Map()

interface Loaded {
  key: string
  index: Map<string, Unit>
}

export function useUnitsIndex(buildingIds: (string | undefined)[] | undefined): Map<string, Unit> {
  const key = [...new Set(buildingIds || [])].sort().join(',')
  // Held together with the key it was fetched for, so a result belonging to a
  // previous set of buildings is never handed back as if it were current.
  const [loaded, setLoaded] = useState<Loaded>({ key: '', index: EMPTY })

  useEffect(() => {
    // No buildings to look up: nothing to fetch, and nothing to reset either —
    // the empty case is derived on the way out rather than written back into
    // state from inside the effect, which would cost an extra render pass and
    // leave one render reading the *old* index for the new key.
    if (!key) return undefined

    let cancelled = false
    Promise.all(key.split(',').map((id) => buildingsApi.units(id).catch(() => [])))
      .then((lists) => {
        if (cancelled) return
        const map = new Map<string, Unit>()
        for (const list of lists) {
          for (const u of list) map.set(u.id, u)
        }
        setLoaded({ key, index: map })
      })
      .catch(() => {
        if (!cancelled) setLoaded({ key, index: EMPTY })
      })
    return () => {
      cancelled = true
    }
  }, [key])

  return loaded.key === key ? loaded.index : EMPTY
}
