// Thin fetch wrapper over Pango's HTTP API (backend/internal/api).
//
// Two things matter here because the backend enforces them:
//  - the session cookie is HttpOnly, so every call needs `credentials:
//    "include"` — there is no token in JS to attach by hand.
//  - `decode()` on the server rejects unknown JSON fields (backend/internal/
//    api/json.go), so request builders below send exactly the fields the
//    matching Go `xReq` struct declares, nothing more.

import type {
  CostKind,
  EventVisibility,
  FindingCondition,
  InspectionKind,
  InspectionStatus,
  JobPriority,
  JobStatus,
  PartyKind,
} from './domain'

// Re-exported (not just imported) so callers that already reach into this
// module for entity shapes (Job, Building, ...) can pull the matching enum
// types from the same place, rather than needing a second import from
// './domain' just for e.g. JobStatus.
export type { CostKind, EventVisibility, FindingCondition, InspectionKind, InspectionStatus, JobPriority, JobStatus, PartyKind }

export class ApiError extends Error {
  status: number
  // Set by src/lib/auth.jsx on a 403 from /auth/register — a first-run-only
  // state the caller renders distinctly from a generic failure.
  registrationClosed?: boolean

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

interface RequestOpts {
  method?: string
  body?: unknown
  signal?: AbortSignal
}

async function request<T>(path: string, { method = 'GET', body, signal }: RequestOpts = {}): Promise<T> {
  let res: Response
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
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      // Non-JSON body (should not happen against this API) — fall through
      // with data null so callers still get a status-based error.
    }
  }

  if (!res.ok) {
    // Written as an explicit if/assign rather than a chained `&&`/`||`
    // expression — the chain's own inferred type (a union of every
    // short-circuited operand, unknown included) is what a plain `||`
    // fallback can't narrow back down to `string`.
    let message = `Request failed (${res.status})`
    if (data && typeof data === 'object' && 'error' in data && typeof (data as { error: unknown }).error === 'string') {
      message = (data as { error: string }).error
    }
    throw new ApiError(res.status, message)
  }
  return data as T
}

export const api = {
  get: <T>(path: string, opts?: RequestOpts) => request<T>(path, { ...opts, method: 'GET' }),
  post: <T>(path: string, body?: unknown, opts?: RequestOpts) =>
    request<T>(path, { ...opts, method: 'POST', body: body ?? {} }),
  patch: <T>(path: string, body?: unknown, opts?: RequestOpts) =>
    request<T>(path, { ...opts, method: 'PATCH', body: body ?? {} }),
  del: <T>(path: string, opts?: RequestOpts) => request<T>(path, { ...opts, method: 'DELETE' }),
}

// Accepts any plain object of filter/query values — typed as `unknown` per
// value rather than a closed union so callers can pass a precise interface
// (e.g. JobsFilter) without that interface needing an index signature of its
// own just to satisfy this one internal helper.
function qs(params?: Record<string, unknown>): string {
  const usp = new URLSearchParams()
  for (const [k, v] of Object.entries(params || {})) {
    if (v !== undefined && v !== null && v !== '') usp.set(k, String(v))
  }
  const s = usp.toString()
  return s ? `?${s}` : ''
}

// ── shared entity shapes ────────────────────────────────────────────────

export interface AuthUser {
  id: string
  name: string
  email: string
}

export interface AuthOrg {
  id: string
  name: string
}

export interface Building {
  id: string
  name: string
  address: string
  lat: number | null
  lon: number | null
  unit_scheme?: string
}

export interface Unit {
  id: string
  label: string
  key: string
  building_id?: string
}

export interface Job {
  id: string
  number: number
  title: string
  description: string
  status: JobStatus
  priority: JobPriority
  category: string
  building_id: string
  unit_id: string
  assignee_party_id: string
  reporter_party_id?: string
  opened_at: string
  // Go zero-value string, never null — empty means "still open".
  closed_at: string
}

export interface JobTotals {
  cost_minor: number
  minutes: number
}

export interface JobWithTotals {
  job: Job
  // Absent when the report query returns no row for this job (handleGetJob
  // only sets it `if len(totals) == 1`) — never assume it is there.
  totals?: JobTotals
}

export interface JobEvent {
  id: string
  kind: string
  body: string
  actor_party_id: string
  visibility: EventVisibility
  created_at: string
}

export interface JobCost {
  id: string
  kind: CostKind
  description: string
  amount_minor: number
  currency: string
  party_id?: string
  created_at: string
}

export interface JobTimeEntry {
  id: string
  minutes: number
  note: string
  party_id?: string
  created_at: string
}

export interface Party {
  id: string
  kind: PartyKind
  name: string
  email: string
  phone: string
  pubkey: string
}

export interface Peer {
  id: string
  name: string
  url: string
  pubkey: string
  enabled: boolean
}

export interface TemplateItem {
  id: string
  section: string
  label: string
  sort: number
}

export interface Template {
  id: string
  name: string
  kind: string
  items: TemplateItem[]
}

export interface Inspection {
  id: string
  building_id: string
  unit_id: string
  template_id: string
  kind: InspectionKind
  status: InspectionStatus
  // Go zero-value string, never null — empty means "not scheduled".
  scheduled_for: string
  inspector_party_id: string
  notes: string
  created_at?: string
}

export interface InspectionWithFindings {
  inspection: Inspection
  findings: Finding[]
}

export interface Finding {
  id: string
  item_id: string
  label: string
  condition: FindingCondition
  comment: string
  photo_refs: string
  created_at: string
}

// Mirrors inspect.Outcome (backend/internal/inspect/compare.go).
export type ComparisonOutcome =
  | 'unchanged'
  | 'deteriorated'
  | 'improved'
  | 'not_captured_ingoing'
  | 'not_captured_outgoing'

export interface ComparisonRow {
  label: string
  section: string
  ingoing_captured: boolean
  // `omitempty` on the Go side — absent (not just falsy) when not captured.
  ingoing_condition?: FindingCondition
  outgoing_captured: boolean
  outgoing_condition?: FindingCondition
  outcome: ComparisonOutcome
}

export interface Comparison {
  items: ComparisonRow[]
  // Only outcomes that actually occurred get a key (`counts[outcome]++`, never
  // pre-seeded), so every entry is optional rather than defaulted to 0.
  counts: Partial<Record<ComparisonOutcome, number>>
}

export interface StatusReport {
  open: number
  closed: number
  total: number
}

export interface TimelinePoint {
  day: string
  created: number
  closed: number
}

export interface BuildingReportRow {
  building_id: string
  name: string
  jobs: number
  open_jobs: number
  cost_minor: number
  minutes: number
}

export interface UnitReportRow {
  unit_id: string
  label: string
  jobs: number
  open_jobs: number
  cost_minor: number
  minutes: number
}

// Mirrors report.JobTotals (backend/internal/report/report.go). Not
// currently rendered by any page, but the shape is real — see reportsApi.jobs.
export interface JobReportRow {
  job_id: string
  building_id: string
  unit_id: string
  number: number
  title: string
  status: string
  cost_minor: number
  minutes: number
  entries: number
}

// ── auth ─────────────────────────────────────────────────────────────────
export interface RegisterFields {
  organisation: string
  email: string
  password: string
  name: string
}

export interface LoginFields {
  email: string
  password: string
}

export interface AuthResponse {
  token: string
  user: AuthUser
}

export interface MeResponse {
  user: AuthUser
  organisation?: AuthOrg | null
}

export const authApi = {
  register: (body: RegisterFields) => api.post<AuthResponse>('/auth/register', body),
  login: (body: LoginFields) => api.post<AuthResponse>('/auth/login', body),
  logout: () => api.post<void>('/auth/logout'),
  me: () => api.get<MeResponse>('/auth/me'),
}

export type AuthStatus = 'loading' | 'anonymous' | 'authenticated'

export interface AuthContextValue {
  status: AuthStatus
  user: AuthUser | null
  org: AuthOrg | null
  login: (email: string, password: string) => Promise<AuthResponse>
  register: (fields: RegisterFields) => Promise<AuthResponse>
  logout: () => Promise<void>
  refresh: () => Promise<void>
}

// ── buildings & units ───────────────────────────────────────────────────
export interface CreateBuildingBody {
  name: string
  address: string
  unit_scheme: string
  lat: number | null
  lon: number | null
}

export interface JobsFilter {
  building_id?: string
  unit_id?: string
  status?: string
  // Lets a JobsFilter variable (not just a fresh literal) pass straight into
  // qs()'s Record<string, unknown> parameter — named interfaces don't get an
  // index signature for free.
  [key: string]: string | undefined
}

export const buildingsApi = {
  list: () => api.get<Building[]>('/buildings'),
  create: (body: CreateBuildingBody) => api.post<Building>('/buildings', body),
  get: (id: string) => api.get<Building>(`/buildings/${id}`),
  update: (id: string, body: Partial<CreateBuildingBody>) => api.patch<Building>(`/buildings/${id}`, body),
  remove: (id: string) => api.del<void>(`/buildings/${id}`),
  units: (id: string) => api.get<Unit[]>(`/buildings/${id}/units`),
  ensureUnit: (id: string, label: string) => api.post<Unit>(`/buildings/${id}/units`, { label }),
}

// ── jobs ─────────────────────────────────────────────────────────────────
export interface CreateJobBody {
  building_id: string
  unit_id: string
  unit_label: string
  title: string
  description: string
  priority: JobPriority
  category: string
  reporter_party_id: string
}

export interface SetJobStatusBody {
  status: JobStatus
  note: string
  actor_party_id: string
}

export interface AddJobEventBody {
  kind: string
  body: string
  actor_party_id: string
  visibility: EventVisibility
}

export interface AddJobCostBody {
  kind: CostKind
  description: string
  amount_minor: number
  currency: string
  party_id: string
}

export interface AddJobTimeBody {
  minutes: number
  note: string
  party_id: string
}

export const jobsApi = {
  list: (filter?: JobsFilter) => api.get<Job[]>(`/jobs${qs(filter)}`),
  create: (body: CreateJobBody) => api.post<Job>('/jobs', body),
  get: (id: string) => api.get<JobWithTotals>(`/jobs/${id}`),
  setStatus: (id: string, body: SetJobStatusBody) => api.post<Job>(`/jobs/${id}/status`, body),
  assign: (id: string, partyId: string) => api.post<Job>(`/jobs/${id}/assign`, { party_id: partyId }),
  events: (id: string, publicOnly?: boolean) =>
    api.get<JobEvent[]>(`/jobs/${id}/events${qs({ public: publicOnly ? '1' : undefined })}`),
  addEvent: (id: string, body: AddJobEventBody) => api.post<JobEvent>(`/jobs/${id}/events`, body),
  costs: (id: string) => api.get<JobCost[]>(`/jobs/${id}/costs`),
  addCost: (id: string, body: AddJobCostBody) => api.post<JobCost>(`/jobs/${id}/costs`, body),
  time: (id: string) => api.get<JobTimeEntry[]>(`/jobs/${id}/time`),
  addTime: (id: string, body: AddJobTimeBody) => api.post<JobTimeEntry>(`/jobs/${id}/time`, body),
}

// ── parties & peers ─────────────────────────────────────────────────────
export interface CreatePartyBody {
  kind: PartyKind
  name: string
  email: string
  phone: string
  pubkey: string
}

export interface SavePeerBody {
  id: string
  name: string
  url: string
  pubkey: string
  enabled: boolean
}

export const partiesApi = {
  list: (kind?: PartyKind) => api.get<Party[]>(`/parties${qs({ kind })}`),
  create: (body: CreatePartyBody) => api.post<Party>('/parties', body),
}

export const peersApi = {
  list: () => api.get<Peer[]>('/peers'),
  save: (body: SavePeerBody) => api.post<Peer>('/peers', body),
  remove: (id: string) => api.del<void>(`/peers/${id}`),
}

// ── inspections & templates ─────────────────────────────────────────────
export interface CreateTemplateBody {
  name: string
  kind: string
  items: { section: string; label: string; sort: number }[]
}

export interface InspectionsFilter {
  building_id?: string
  kind?: string
  status?: string
  [key: string]: string | undefined
}

export interface CreateInspectionBody {
  building_id: string
  unit_id: string
  unit_label: string
  template_id: string
  kind: InspectionKind
  scheduled_for: string
  inspector_party_id: string
  notes: string
}

export interface AddFindingBody {
  item_id: string
  label: string
  condition: FindingCondition
  comment: string
  photo_refs: string
}

export const templatesApi = {
  list: () => api.get<Template[]>('/templates'),
  create: (body: CreateTemplateBody) => api.post<Template>('/templates', body),
  get: (id: string) => api.get<Template>(`/templates/${id}`),
}

export const inspectionsApi = {
  list: (filter?: InspectionsFilter) => api.get<Inspection[]>(`/inspections${qs(filter)}`),
  create: (body: CreateInspectionBody) => api.post<Inspection>('/inspections', body),
  get: (id: string) => api.get<InspectionWithFindings>(`/inspections/${id}`),
  setStatus: (id: string, status: InspectionStatus) => api.post<Inspection>(`/inspections/${id}/status`, { status }),
  findings: (id: string) => api.get<Finding[]>(`/inspections/${id}/findings`),
  addFinding: (id: string, body: AddFindingBody) => api.post<Finding>(`/inspections/${id}/findings`, body),
  // Keyed on the OUTGOING inspection id — the server resolves the matching
  // ingoing inspection itself (backend/internal/repo/inspection.go's
  // MatchingIngoing), the same way org scoping is never a client parameter.
  // 404 means no ingoing baseline exists for this unit yet; 409 means the
  // id named isn't an outgoing inspection.
  compare: (outgoingId: string) => api.get<Comparison>(`/inspections/${outgoingId}/comparison`),
}

// ── reports ──────────────────────────────────────────────────────────────
export const reportsApi = {
  buildings: () => api.get<BuildingReportRow[]>('/reports/buildings'),
  units: (buildingId?: string) => api.get<UnitReportRow[]>(`/reports/units${qs({ building_id: buildingId })}`),
  jobs: (jobId?: string) => api.get<JobReportRow[]>(`/reports/jobs${qs({ job_id: jobId })}`),
  status: () => api.get<StatusReport>('/reports/status'),
  timeline: () => api.get<TimelinePoint[]>('/reports/timeline'),
}

export interface HealthReport {
  status: string
  version: string
  demo: boolean
  node: string
}

export const healthApi = {
  check: () => api.get<HealthReport>('/health'),
}
