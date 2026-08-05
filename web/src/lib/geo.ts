// Proximity ranking for the raise-a-job building picker.
//
// The API has no server-side "near me" endpoint (backend/internal/repo/
// building.go's ListBuildings takes no location) — so this is computed
// client-side from the browser's geolocation and each building's lat/lon,
// which the backend does return (§4.2). Buildings with no surveyed location
// (`lat`/`lon` both null — "not surveyed" is deliberately distinguishable
// from 0,0, see domain.Building.Validate) sort after every located one
// rather than to a wrong place on the list.

const EARTH_RADIUS_KM = 6371

export function haversineKm(lat1: number, lon1: number, lat2: number, lon2: number): number {
  const toRad = (d: number) => (d * Math.PI) / 180
  const dLat = toRad(lat2 - lat1)
  const dLon = toRad(lon2 - lon1)
  const a =
    Math.sin(dLat / 2) ** 2 +
    Math.cos(toRad(lat1)) * Math.cos(toRad(lat2)) * Math.sin(dLon / 2) ** 2
  return 2 * EARTH_RADIUS_KM * Math.asin(Math.min(1, Math.sqrt(a)))
}

export interface GeoOrigin {
  lat: number
  lon: number
}

interface WithLatLon {
  lat: number | null
  lon: number | null
}

/** Sort buildings by distance from {lat, lon}; unsurveyed buildings sort last. */
export function rankByProximity<T extends WithLatLon>(buildings: T[], origin: GeoOrigin | null | undefined): T[] {
  if (!origin) return buildings
  return [...buildings].sort((a, b) => {
    const da = a.lat != null && a.lon != null ? haversineKm(origin.lat, origin.lon, a.lat, a.lon) : Infinity
    const db = b.lat != null && b.lon != null ? haversineKm(origin.lat, origin.lon, b.lat, b.lon) : Infinity
    return da - db
  })
}

export type GeoStatus = 'idle' | 'locating' | 'done' | 'error' | 'unsupported'

/**
 * useGeolocation — requests the browser's position once, on demand.
 * Returns { position, status, error, request }. Never auto-prompts: a
 * maintenance app asking for location before the user asked for it is the
 * kind of thing that gets an install uninstalled.
 */
import { useCallback, useState } from 'react'

export function useGeolocation() {
  const [status, setStatus] = useState<GeoStatus>('idle')
  const [position, setPosition] = useState<GeoOrigin | null>(null)
  const [error, setError] = useState<string | null>(null)

  const request = useCallback(() => {
    if (!('geolocation' in navigator)) {
      setStatus('unsupported')
      return
    }
    setStatus('locating')
    setError(null)
    navigator.geolocation.getCurrentPosition(
      (pos) => {
        setPosition({ lat: pos.coords.latitude, lon: pos.coords.longitude })
        setStatus('done')
      },
      (err) => {
        setError(err.message || 'Location unavailable')
        setStatus('error')
      },
      { enableHighAccuracy: false, timeout: 8000, maximumAge: 60_000 },
    )
  }, [])

  return { position, status, error, request }
}
