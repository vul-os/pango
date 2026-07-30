// Thin fetch wrapper over Pango's HTTP API (backend/internal/api).
//
// Two things matter here because the backend enforces them:
//  - the session cookie is HttpOnly, so every call needs `credentials:
//    "include"` — there is no token in JS to attach by hand.
//  - `decode()` on the server rejects unknown JSON fields (backend/internal/
//    api/json.go), so request builders below send exactly the fields the
//    matching Go `xReq` struct declares, nothing more.

export class ApiError extends Error {
  constructor(status, message) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

async function request(path, { method = 'GET', body, signal } = {}) {
  let res
  try {
    res = await fetch(`/api${path}`, {
      method,
      credentials: 'include',
      headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
      body: body !== undefined ? JSON.stringify(body) : undefined,
      signal,
    })
  } catch {
    throw new ApiError(0, 'Could not reach the Pango server. Check the node is running and reachable.')
  }

  const text = await res.text()
  let data = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      // Non-JSON body (should not happen against this API) — fall through
      // with data null so callers still get a status-based error.
    }
  }

  if (!res.ok) {
    const message = (data && data.error) || `Request failed (${res.status})`
    throw new ApiError(res.status, message)
  }
  return data
}

export const api = {
  get: (path, opts) => request(path, { ...opts, method: 'GET' }),
  post: (path, body, opts) => request(path, { ...opts, method: 'POST', body: body ?? {} }),
  patch: (path, body, opts) => request(path, { ...opts, method: 'PATCH', body: body ?? {} }),
  del: (path, opts) => request(path, { ...opts, method: 'DELETE' }),
}

function qs(params) {
  const usp = new URLSearchParams()
  for (const [k, v] of Object.entries(params || {})) {
    if (v !== undefined && v !== null && v !== '') usp.set(k, v)
  }
  const s = usp.toString()
  return s ? `?${s}` : ''
}

// ── auth ─────────────────────────────────────────────────────────────────
export const authApi = {
  register: (body) => api.post('/auth/register', body), // { organisation, email, password, name }
  login: (body) => api.post('/auth/login', body), // { email, password }
  logout: () => api.post('/auth/logout'),
  me: () => api.get('/auth/me'),
}

// ── buildings & units ───────────────────────────────────────────────────
export const buildingsApi = {
  list: () => api.get('/buildings'),
  create: (body) => api.post('/buildings', body), // { name, address, lat, lon, unit_scheme }
  get: (id) => api.get(`/buildings/${id}`),
  update: (id, body) => api.patch(`/buildings/${id}`, body),
  remove: (id) => api.del(`/buildings/${id}`),
  units: (id) => api.get(`/buildings/${id}/units`),
  ensureUnit: (id, label) => api.post(`/buildings/${id}/units`, { label }),
}

// ── jobs ─────────────────────────────────────────────────────────────────
export const jobsApi = {
  list: (filter) => api.get(`/jobs${qs(filter)}`),
  create: (body) => api.post('/jobs', body),
  get: (id) => api.get(`/jobs/${id}`),
  setStatus: (id, body) => api.post(`/jobs/${id}/status`, body), // { status, note, actor_party_id }
  assign: (id, partyId) => api.post(`/jobs/${id}/assign`, { party_id: partyId }),
  events: (id, publicOnly) => api.get(`/jobs/${id}/events${qs({ public: publicOnly ? '1' : undefined })}`),
  addEvent: (id, body) => api.post(`/jobs/${id}/events`, body), // { kind, body, actor_party_id, visibility }
  costs: (id) => api.get(`/jobs/${id}/costs`),
  addCost: (id, body) => api.post(`/jobs/${id}/costs`, body), // { kind, description, amount_minor, currency, party_id }
  time: (id) => api.get(`/jobs/${id}/time`),
  addTime: (id, body) => api.post(`/jobs/${id}/time`, body), // { minutes, note, party_id }
}

// ── parties & peers ─────────────────────────────────────────────────────
export const partiesApi = {
  list: (kind) => api.get(`/parties${qs({ kind })}`),
  create: (body) => api.post('/parties', body), // { kind, name, email, phone, pubkey }
}

export const peersApi = {
  list: () => api.get('/peers'),
  save: (body) => api.post('/peers', body), // { id, name, url, pubkey, enabled }
  remove: (id) => api.del(`/peers/${id}`),
}

// ── inspections & templates ─────────────────────────────────────────────
export const templatesApi = {
  list: () => api.get('/templates'),
  create: (body) => api.post('/templates', body), // { name, kind, items: [{section,label,sort}] }
  get: (id) => api.get(`/templates/${id}`),
}

export const inspectionsApi = {
  list: (filter) => api.get(`/inspections${qs(filter)}`),
  create: (body) => api.post('/inspections', body),
  get: (id) => api.get(`/inspections/${id}`),
  setStatus: (id, status) => api.post(`/inspections/${id}/status`, { status }),
  findings: (id) => api.get(`/inspections/${id}/findings`),
  addFinding: (id, body) => api.post(`/inspections/${id}/findings`, body), // { item_id, label, condition, comment, photo_refs }
  // Keyed on the OUTGOING inspection id — the server resolves the matching
  // ingoing inspection itself (backend/internal/repo/inspection.go's
  // MatchingIngoing), the same way org scoping is never a client parameter.
  // 404 means no ingoing baseline exists for this unit yet; 409 means the
  // id named isn't an outgoing inspection.
  compare: (outgoingId) => api.get(`/inspections/${outgoingId}/comparison`),
}

// ── reports ──────────────────────────────────────────────────────────────
export const reportsApi = {
  buildings: () => api.get('/reports/buildings'),
  units: (buildingId) => api.get(`/reports/units${qs({ building_id: buildingId })}`),
  jobs: (jobId) => api.get(`/reports/jobs${qs({ job_id: jobId })}`),
  status: () => api.get('/reports/status'),
  timeline: () => api.get('/reports/timeline'),
}

export const healthApi = {
  check: () => api.get('/health'),
}
