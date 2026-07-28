package wrap

// Typed views over the six WRAP object kinds (02-objects.md §3.3–3.11) and
// the shared Place / Window / Compensation value types they embed.
//
// Each kind's ToObject builds an unsigned *Object (call Sign before
// Envelope); each kind's parser reads an already-decoded *Object back into
// the typed shape. Field keys are documented inline against the spec table
// they come from — they are scoped per kind, not shared, so key 7 means
// "title" on a WorkOrder and "performer" on an Assignment.

import "fmt"

func asMap(v any) (map[any]any, bool) {
	switch x := v.(type) {
	case map[any]any:
		return x, true
	case M:
		out := make(map[any]any, len(x))
		for k, vv := range x {
			out[k] = vv
		}
		return out, true
	}
	return nil, false
}

// ── Place (§3.9) ────────────────────────────────────────────────────────────

type Place struct {
	Role   string // 1: e.g. "site", "pickup", "dropoff"
	Lat    *float64
	Lon    *float64
	Label  string // 4
	Detail string // 5: access notes, unit number, gate code
	Geo    string // 6: optional GeoJSON
}

func (p Place) toM() M {
	m := M{}
	if p.Role != "" {
		m[1] = p.Role
	}
	if p.Lat != nil {
		m[2] = *p.Lat
	}
	if p.Lon != nil {
		m[3] = *p.Lon
	}
	if p.Label != "" {
		m[4] = p.Label
	}
	if p.Detail != "" {
		m[5] = p.Detail
	}
	if p.Geo != "" {
		m[6] = p.Geo
	}
	return m
}

func placeFromMap(m map[any]any) Place {
	var p Place
	if s, ok := getString(m, 1); ok {
		p.Role = s
	}
	if f, ok := mapGet(m, uint64(2)); ok {
		if v, ok := f.(float64); ok {
			p.Lat = &v
		}
	}
	if f, ok := mapGet(m, uint64(3)); ok {
		if v, ok := f.(float64); ok {
			p.Lon = &v
		}
	}
	if s, ok := getString(m, 4); ok {
		p.Label = s
	}
	if s, ok := getString(m, 5); ok {
		p.Detail = s
	}
	if s, ok := getString(m, 6); ok {
		p.Geo = s
	}
	return p
}

// ── Window (§3.10) ──────────────────────────────────────────────────────────

const (
	WindowImmediate uint64 = 0
	WindowScheduled uint64 = 1 // the field that makes trades/v0 expressible
)

type Window struct {
	Earliest uint64
	Latest   uint64
	Duration uint64
	Kind     uint64
}

func (w Window) toM() M {
	m := M{}
	if w.Earliest != 0 {
		m[1] = w.Earliest
	}
	if w.Latest != 0 {
		m[2] = w.Latest
	}
	if w.Duration != 0 {
		m[3] = w.Duration
	}
	m[4] = w.Kind
	return m
}

func windowFromMap(m map[any]any) Window {
	var w Window
	if u, ok := getUint(m, 1); ok {
		w.Earliest = u
	}
	if u, ok := getUint(m, 2); ok {
		w.Latest = u
	}
	if u, ok := getUint(m, 3); ok {
		w.Duration = u
	}
	if u, ok := getUint(m, 4); ok {
		w.Kind = u
	}
	return w
}

// ── Compensation (§3.11) — terms only; WRAP never moves money ──────────────

type Compensation struct {
	Currency string
	Amount   int64
	Rate     int64
	Unit     string
	Max      int64
	Note     string
}

func (c Compensation) toM() M {
	m := M{}
	if c.Currency != "" {
		m[1] = c.Currency
	}
	if c.Amount != 0 {
		m[2] = c.Amount
	}
	if c.Rate != 0 {
		m[3] = c.Rate
	}
	if c.Unit != "" {
		m[4] = c.Unit
	}
	if c.Max != 0 {
		m[5] = c.Max
	}
	if c.Note != "" {
		m[6] = c.Note
	}
	return m
}

func compensationFromMap(m map[any]any) Compensation {
	var c Compensation
	if s, ok := getString(m, 1); ok {
		c.Currency = s
	}
	if n, ok := getInt(m, 2); ok {
		c.Amount = n
	}
	if n, ok := getInt(m, 3); ok {
		c.Rate = n
	}
	if s, ok := getString(m, 4); ok {
		c.Unit = s
	}
	if n, ok := getInt(m, 5); ok {
		c.Max = n
	}
	if s, ok := getString(m, 6); ok {
		c.Note = s
	}
	return c
}

func getInt(m map[any]any, key uint64) (int64, bool) {
	v, ok := mapGet(m, key)
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case uint64:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}

// ── WorkOrder (kind 0x01, §3.3) ─────────────────────────────────────────────

type WorkOrder struct {
	Profile string            // 6, MUST
	Title   string            // 7, MUST
	Detail  string            // 8
	Places  []Place           // 9
	Window  *Window           // 10
	Comp    *Compensation     // 11
	Needs   []string          // 12
	Expires uint64            // 13, MUST — unix seconds
	Refs    map[string]string // 14 — opaque external identifiers
	Body    M                 // 15 — profile-specific fields, keys 32+
}

func (w WorkOrder) ToObject(issuer [32]byte, ts string) *Object {
	f := M{6: w.Profile, 7: w.Title, 13: w.Expires}
	if w.Detail != "" {
		f[8] = w.Detail
	}
	if len(w.Places) > 0 {
		arr := make([]any, len(w.Places))
		for i, p := range w.Places {
			arr[i] = p.toM()
		}
		f[9] = arr
	}
	if w.Window != nil {
		f[10] = w.Window.toM()
	}
	if w.Comp != nil {
		f[11] = w.Comp.toM()
	}
	if len(w.Needs) > 0 {
		arr := make([]any, len(w.Needs))
		for i, n := range w.Needs {
			arr[i] = n
		}
		f[12] = arr
	}
	if len(w.Refs) > 0 {
		rm := RM{}
		for k, v := range w.Refs {
			rm[k] = v
		}
		f[14] = rm
	}
	if len(w.Body) > 0 {
		f[15] = w.Body
	}
	return &Object{Kind: KindWorkOrder, Author: issuer, TS: ts, Fields: f}
}

// WorkOrderFrom reads a decoded WorkOrder object back into the typed shape.
func WorkOrderFrom(o *Object) (WorkOrder, error) {
	if o.Kind != KindWorkOrder {
		return WorkOrder{}, fmt.Errorf("wrap: WorkOrderFrom: object is kind %s, not WorkOrder", o.Kind)
	}
	m := fieldsAsMap(o.Fields)
	w := WorkOrder{}
	if s, ok := getString(m, 6); ok {
		w.Profile = s
	} else {
		return WorkOrder{}, fmt.Errorf("wrap: WorkOrderFrom: missing profile (key 6)")
	}
	if s, ok := getString(m, 7); ok {
		w.Title = s
	} else {
		return WorkOrder{}, fmt.Errorf("wrap: WorkOrderFrom: missing title (key 7)")
	}
	if s, ok := getString(m, 8); ok {
		w.Detail = s
	}
	if arr, ok := getArray(m, 9); ok {
		for _, e := range arr {
			if pm, ok := asMap(e); ok {
				w.Places = append(w.Places, placeFromMap(pm))
			}
		}
	}
	if wm, ok := getMapField(m, 10); ok {
		win := windowFromMap(wm)
		w.Window = &win
	}
	if cm, ok := getMapField(m, 11); ok {
		comp := compensationFromMap(cm)
		w.Comp = &comp
	}
	if arr, ok := getArray(m, 12); ok {
		for _, e := range arr {
			if s, ok := e.(string); ok {
				w.Needs = append(w.Needs, s)
			}
		}
	}
	if u, ok := getUint(m, 13); ok {
		w.Expires = u
	} else {
		return WorkOrder{}, fmt.Errorf("wrap: WorkOrderFrom: missing expires (key 13)")
	}
	if rv, present := m[uint64(14)]; present {
		if rm, ok := rv.(RM); ok {
			w.Refs = map[string]string{}
			for k, v := range rm {
				if s, ok := v.(string); ok {
					w.Refs[k] = s
				}
			}
		} else if gm, ok := rv.(map[any]any); ok {
			w.Refs = map[string]string{}
			for k, v := range gm {
				ks, kok := k.(string)
				vs, vok := v.(string)
				if kok && vok {
					w.Refs[ks] = vs
				}
			}
		}
	}
	if bm, ok := getMapField(m, 15); ok {
		w.Body = M{}
		for k, v := range bm {
			if ku, ok := k.(uint64); ok {
				w.Body[ku] = v
			}
		}
	}
	return w, nil
}

// fieldsAsMap normalises Object.Fields (M) into the map[any]any shape the
// getX helpers understand, so the same parsers work whether the Object came
// from Decode (values may be map[any]any/[]any from the wire) or was built
// directly in Go (values may be M/[]Place-derived M, etc — both handled by
// asMap and a plain []any assertion at the point of use).
func fieldsAsMap(f M) map[any]any {
	out := make(map[any]any, len(f))
	for k, v := range f {
		out[k] = v
	}
	return out
}

// ── Offer (kind 0x02, §3.4) ─────────────────────────────────────────────────

type Offer struct {
	Order  []byte // 6, MUST — WorkOrder.id
	Pool   [32]byte
	Mode   uint64 // 8: 0 direct, 1 open bid, 2 sealed bid
	Closes uint64 // 9
}

const (
	OfferModeDirect  uint64 = 0
	OfferModeOpenBid uint64 = 1
	OfferModeSealed  uint64 = 2
)

func (o Offer) ToObject(issuer [32]byte, ts string) *Object {
	f := M{6: o.Order, 7: append([]byte(nil), o.Pool[:]...), 8: o.Mode}
	if o.Closes != 0 {
		f[9] = o.Closes
	}
	return &Object{Kind: KindOffer, Author: issuer, TS: ts, Fields: f}
}

// ── Bid (kind 0x03, §3.5) ────────────────────────────────────────────────────

type Bid struct {
	Order     []byte // 6, MUST
	Offer     []byte // 7, MUST
	Quote     *Compensation
	ETA       uint64
	Note      string
	Withdrawn bool
}

func (b Bid) ToObject(performer [32]byte, ts string) *Object {
	f := M{6: b.Order, 7: b.Offer}
	if b.Quote != nil {
		f[8] = b.Quote.toM()
	}
	if b.ETA != 0 {
		f[9] = b.ETA
	}
	if b.Note != "" {
		f[10] = b.Note
	}
	if b.Withdrawn {
		f[11] = true
	}
	return &Object{Kind: KindBid, Author: performer, TS: ts, Fields: f}
}

// ── Assignment (kind 0x04, §3.6) — issuer-only, enforced in propfix.go ─────

type Assignment struct {
	Order     []byte // 6, MUST
	Performer [32]byte
	Terms     *Compensation
	Revoked   bool
}

func (a Assignment) ToObject(issuer [32]byte, ts string) *Object {
	f := M{6: a.Order, 7: append([]byte(nil), a.Performer[:]...)}
	if a.Terms != nil {
		f[8] = a.Terms.toM()
	}
	if a.Revoked {
		f[9] = true
	}
	return &Object{Kind: KindAssignment, Author: issuer, TS: ts, Fields: f}
}

// AssignmentFrom reads a decoded Assignment object back into the typed shape.
// It does NOT check the authorship rule — see VerifyAssignmentAuthor, which
// needs the WorkOrder this refers to.
func AssignmentFrom(o *Object) (Assignment, error) {
	if o.Kind != KindAssignment {
		return Assignment{}, fmt.Errorf("wrap: AssignmentFrom: object is kind %s, not Assignment", o.Kind)
	}
	m := fieldsAsMap(o.Fields)
	a := Assignment{}
	order, ok := getBytes(m, 6)
	if !ok {
		return Assignment{}, fmt.Errorf("wrap: AssignmentFrom: missing order (key 6)")
	}
	a.Order = order
	performer, ok := getBytes(m, 7)
	if !ok || len(performer) != 32 {
		return Assignment{}, fmt.Errorf("wrap: AssignmentFrom: missing or malformed performer (key 7)")
	}
	copy(a.Performer[:], performer)
	if tm, ok := getMapField(m, 8); ok {
		terms := compensationFromMap(tm)
		a.Terms = &terms
	}
	if v, ok := mapGet(m, uint64(9)); ok {
		if b, ok := v.(bool); ok {
			a.Revoked = b
		}
	}
	return a, nil
}

// ── Progress (kind 0x05, §3.7) ──────────────────────────────────────────────

type Progress struct {
	Order []byte // 6, MUST
	State string // 7, MUST
	At    *Place // 8
	Note  string // 9
	Body  M      // 10
}

func (p Progress) ToObject(author [32]byte, ts string) *Object {
	f := M{6: p.Order, 7: p.State}
	if p.At != nil {
		f[8] = p.At.toM()
	}
	if p.Note != "" {
		f[9] = p.Note
	}
	if len(p.Body) > 0 {
		f[10] = p.Body
	}
	return &Object{Kind: KindProgress, Author: author, TS: ts, Fields: f}
}

// ── Attestation (kind 0x06, §3.8) ───────────────────────────────────────────

const (
	OutcomeCompleted uint64 = 0
	OutcomeFailed    uint64 = 1
	OutcomeCancelled uint64 = 2
	OutcomeDisputed  uint64 = 3
)

type Attestation struct {
	Order   []byte // 6, MUST
	Subject [32]byte
	Outcome uint64 // 8, MUST
	Rating  uint64 // 9: 1-5
	Proof   M      // 10
	Note    string // 11
}

func (a Attestation) ToObject(attestor [32]byte, ts string) *Object {
	f := M{6: a.Order, 7: append([]byte(nil), a.Subject[:]...), 8: a.Outcome}
	if a.Rating != 0 {
		f[9] = a.Rating
	}
	if len(a.Proof) > 0 {
		f[10] = a.Proof
	}
	if a.Note != "" {
		f[11] = a.Note
	}
	return &Object{Kind: KindAttestation, Author: attestor, TS: ts, Fields: f}
}
