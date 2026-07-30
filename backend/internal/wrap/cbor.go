// Package wrap implements Pango's binding to WRAP (github.com/vul-os/wrap),
// the open work-coordination protocol: object encoding, content addressing,
// signing, and the `trades/v0` mapping onto a Pango job
// (ARCHITECTURE.md §8, docs/WRAP.md).
//
// This file is a small, self-contained deterministic CBOR (RFC 8949 §4.2.1)
// codec, hand-written rather than pulled from a general-purpose CBOR library.
// WRAP's correctness properties — the object `id` and its signature are both
// computed over these exact bytes (WRAP 04-signing.md §5.1) — depend on two
// implementations of the encoder always producing byte-identical output for
// the same object, so the encoder's rules need to be simple enough to state
// and verify rather than inherited from a library's general-purpose options.
package wrap

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
)

// M is a WRAP integer-keyed map: the shape of the common object header and of
// Place, Window, Compensation and profile `body` maps (WRAP 02-objects.md
// §3.2–3.11). A nil-valued entry is treated as absent and omitted by the
// encoder — WRAP's MAY fields are absent, never present-with-null.
type M map[uint64]any

// RM is a WRAP text-keyed map, used only for `refs` — opaque external
// identifiers such as {"order": "BB-4417"} (WRAP 02-objects.md §3.3).
type RM map[string]any

// ErrForbiddenKey is returned when a map contains key 0, which WRAP reserves
// as a deliberate trap for an encoder bug (03-wire-format.md §4.5): it is the
// value produced when a field name fails to resolve to a registered number.
var ErrForbiddenKey = errors.New("wrap: cbor: key 0 is forbidden")

// ErrNotCanonical is returned by Decode when the input, though valid CBOR, is
// not the unique deterministic encoding of the value it represents — for
// example a non-shortest-form integer or unsorted map keys
// (03-wire-format.md §4.1, 04-signing.md §5.4 step 1).
var ErrNotCanonical = errors.New("wrap: cbor: not canonical")

func encodeHead(major byte, n uint64) []byte {
	mt := major << 5
	switch {
	case n < 24:
		return []byte{mt | byte(n)}
	case n <= 0xff:
		return []byte{mt | 24, byte(n)}
	case n <= 0xffff:
		b := make([]byte, 3)
		b[0] = mt | 25
		binary.BigEndian.PutUint16(b[1:], uint16(n))
		return b
	case n <= 0xffffffff:
		b := make([]byte, 5)
		b[0] = mt | 26
		binary.BigEndian.PutUint32(b[1:], uint32(n))
		return b
	default:
		b := make([]byte, 9)
		b[0] = mt | 27
		binary.BigEndian.PutUint64(b[1:], n)
		return b
	}
}

func tstrBytes(s string) []byte {
	return append(encodeHead(3, uint64(len(s))), s...)
}

// encodeFloat writes v as the shortest of IEEE 754 half (2 byte), single
// (4 byte) or double (8 byte) precision that represents v *exactly* — RFC
// 8949's preferred serialization, which deterministic encoding requires
// (03-wire-format.md §4.1: "shortest-form integer encoding" is one instance
// of the general shortest-form rule; floats are the other). "Exactly" is
// load-bearing: this is lossless minimisation, never rounding — a value that
// does not survive narrowing unchanged is encoded at the next width up.
//
// This also makes DecodeCBOR's non-canonical check work for floats for free:
// it decodes to a float64, re-encodes via this function, and compares bytes.
// A wider-than-necessary encoding (e.g. 1.5 sent as an 8-byte double) fails
// that comparison and is rejected as ErrNotCanonical, exactly as a
// non-shortest-form integer is (04-signing.md §5.4 step 1).
func encodeFloat(buf *bytes.Buffer, v float64) {
	if bits, ok := float64ToFloat16Bits(v); ok {
		buf.WriteByte(0xf9)
		var b [2]byte
		binary.BigEndian.PutUint16(b[:], bits)
		buf.Write(b[:])
		return
	}
	if f32 := float32(v); float64(f32) == v {
		buf.WriteByte(0xfa)
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], math.Float32bits(f32))
		buf.Write(b[:])
		return
	}
	buf.WriteByte(0xfb)
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], math.Float64bits(v))
	buf.Write(b[:])
}

// float64ToFloat16Bits reports whether v is exactly representable as an IEEE
// 754 binary16 and, if so, its bit pattern. It is an exactness test, not a
// rounding conversion: any value whose low mantissa bits would be lost by
// narrowing returns ok=false so the caller falls through to a wider width.
//
// NaN is always reported as the canonical quiet half-precision NaN
// (0x7e00, no payload) per RFC 8949's requirement that NaNs be normalised to
// that single bit pattern regardless of the payload or sign the source value
// happened to carry.
func float64ToFloat16Bits(v float64) (uint16, bool) {
	switch {
	case v == 0:
		if math.Signbit(v) {
			return 0x8000, true
		}
		return 0, true
	case math.IsNaN(v):
		return 0x7e00, true
	case math.IsInf(v, 1):
		return 0x7c00, true
	case math.IsInf(v, -1):
		return 0xfc00, true
	}

	sign := uint16(0)
	av := v
	if v < 0 {
		sign = 0x8000
		av = -v
	}

	bits64 := math.Float64bits(av)
	exp64 := int((bits64 >> 52) & 0x7ff)
	mant64 := bits64 & ((uint64(1) << 52) - 1)
	if exp64 == 0 {
		// Double itself is subnormal (|v| < 2^-1022): far smaller than any
		// half can represent exactly except zero, already handled above.
		return 0, false
	}
	e := exp64 - 1023 // av == 1.mant64 * 2^e

	if e >= -14 && e <= 15 {
		// Half normal range. The half mantissa keeps the top 10 of the 52
		// double mantissa bits; the other 42 must be exactly zero.
		const dropped = 42
		if mant64&((uint64(1)<<dropped)-1) != 0 {
			return 0, false
		}
		halfMant := uint16(mant64 >> dropped)
		halfExp := uint16(e + 15)
		return sign | (halfExp << 10) | halfMant, true
	}
	if e >= -24 && e < -14 {
		// Half subnormal range: value = halfMant/1024 * 2^-14, halfMant in
		// [1,1023]. With the implicit leading 1 folded in (M = 2^52|mant64,
		// av = M * 2^(e-52)), halfMant = av * 2^24 = M * 2^(e-28) = M >>
		// (28-e), exact only if the shifted-out low bits are all zero.
		M := (uint64(1) << 52) | mant64
		shift := uint(28 - e)
		if M&((uint64(1)<<shift)-1) != 0 {
			return 0, false
		}
		halfMant := uint16(M >> shift)
		if halfMant == 0 || halfMant > 0x3ff {
			return 0, false
		}
		return sign | halfMant, true
	}
	return 0, false
}

// float16BitsToFloat64 widens a binary16 bit pattern to float64. Widening a
// half to a double is always exact (a double has strictly more range and
// precision than a half), so this needs no rounding logic.
func float16BitsToFloat64(bits uint16) float64 {
	sign := bits&0x8000 != 0
	exp := int((bits >> 10) & 0x1f)
	mant := float64(bits & 0x3ff)

	var f float64
	switch exp {
	case 0:
		f = math.Ldexp(mant, -24) // subnormal: mant/1024 * 2^-14
	case 0x1f:
		if mant == 0 {
			f = math.Inf(1)
		} else {
			f = math.NaN()
		}
	default:
		f = math.Ldexp(1+mant/1024, exp-15)
	}
	if sign {
		f = -f
	}
	return f
}

// kv is one already-key-encoded map entry, ready to be sorted bytewise and
// written — the single mechanism behind M, RM and the generic map decode
// produces (map[any]any), so there is exactly one place that implements
// RFC 8949 §4.2.1's "sort by the bytewise lexicographic order of the encoded
// key" rule.
type kv struct {
	keyBytes []byte
	value    any
}

func encodeMapEntries(buf *bytes.Buffer, entries []kv) error {
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].keyBytes, entries[j].keyBytes) < 0
	})
	buf.Write(encodeHead(5, uint64(len(entries))))
	for _, e := range entries {
		buf.Write(e.keyBytes)
		if err := encodeValue(buf, e.value); err != nil {
			return err
		}
	}
	return nil
}

func encodeValue(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		buf.WriteByte(0xf6) // null: only ever reachable inside an array — map
		// builders omit a nil-valued key rather than encode it as null.
	case uint64:
		buf.Write(encodeHead(0, x))
	case int:
		return encodeValue(buf, int64(x))
	case int64:
		if x >= 0 {
			buf.Write(encodeHead(0, uint64(x)))
		} else {
			buf.Write(encodeHead(1, uint64(-1-x)))
		}
	case bool:
		if x {
			buf.WriteByte(0xf5)
		} else {
			buf.WriteByte(0xf4)
		}
	case float64:
		encodeFloat(buf, x)
	case []byte:
		buf.Write(encodeHead(2, uint64(len(x))))
		buf.Write(x)
	case string:
		buf.Write(tstrBytes(x))
	case []any:
		buf.Write(encodeHead(4, uint64(len(x))))
		for _, e := range x {
			if err := encodeValue(buf, e); err != nil {
				return err
			}
		}
	case M:
		entries := make([]kv, 0, len(x))
		for k, val := range x {
			if val == nil {
				continue
			}
			if k == 0 {
				return ErrForbiddenKey
			}
			entries = append(entries, kv{encodeHead(0, k), val})
		}
		return encodeMapEntries(buf, entries)
	case RM:
		entries := make([]kv, 0, len(x))
		for k, val := range x {
			if val == nil {
				continue
			}
			entries = append(entries, kv{tstrBytes(k), val})
		}
		return encodeMapEntries(buf, entries)
	case map[any]any: // the shape Decode produces, for round-trip re-encoding
		entries := make([]kv, 0, len(x))
		for k, val := range x {
			if val == nil {
				continue
			}
			switch kk := k.(type) {
			case uint64:
				if kk == 0 {
					return ErrForbiddenKey
				}
				entries = append(entries, kv{encodeHead(0, kk), val})
			case string:
				entries = append(entries, kv{tstrBytes(kk), val})
			default:
				return fmt.Errorf("wrap: cbor: unsupported map key type %T", k)
			}
		}
		return encodeMapEntries(buf, entries)
	default:
		return fmt.Errorf("wrap: cbor: unsupported value type %T", v)
	}
	return nil
}

// Encode returns the deterministic CBOR encoding of v (RFC 8949 §4.2.1): map
// keys sorted by the bytewise order of their own encoding, shortest-form
// integers, definite-length arrays and maps only. v must be M, RM, or one of
// the scalar/array/map(any) types encodeValue understands.
func Encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := encodeValue(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decoder is a cursor over a CBOR byte string. It decodes leniently — any
// well-formed CBOR value, not only canonical encodings — because canonicality
// is verified once, generically, by Decode re-encoding the result and
// comparing bytes, rather than by every individual rule being re-implemented
// on the read path.
type decoder struct {
	b []byte
	i int
}

func (d *decoder) readN(n int) ([]byte, error) {
	if n < 0 || d.i+n > len(d.b) {
		return nil, io.ErrUnexpectedEOF
	}
	b := d.b[d.i : d.i+n]
	d.i += n
	return b, nil
}

func (d *decoder) readLen(addl byte) (uint64, error) {
	switch {
	case addl < 24:
		return uint64(addl), nil
	case addl == 24:
		b, err := d.readN(1)
		if err != nil {
			return 0, err
		}
		return uint64(b[0]), nil
	case addl == 25:
		b, err := d.readN(2)
		if err != nil {
			return 0, err
		}
		return uint64(binary.BigEndian.Uint16(b)), nil
	case addl == 26:
		b, err := d.readN(4)
		if err != nil {
			return 0, err
		}
		return uint64(binary.BigEndian.Uint32(b)), nil
	case addl == 27:
		b, err := d.readN(8)
		if err != nil {
			return 0, err
		}
		return binary.BigEndian.Uint64(b), nil
	default:
		return 0, fmt.Errorf("wrap: cbor: unsupported or indefinite length (additional info %d)", addl)
	}
}

func (d *decoder) decodeValue() (any, error) {
	head, err := d.readN(1)
	if err != nil {
		return nil, err
	}
	major := head[0] >> 5
	addl := head[0] & 0x1f

	switch major {
	case 0: // unsigned int
		return d.readLen(addl)
	case 1: // negative int
		n, err := d.readLen(addl)
		if err != nil {
			return nil, err
		}
		return -1 - int64(n), nil
	case 2: // byte string
		n, err := d.readLen(addl)
		if err != nil {
			return nil, err
		}
		b, err := d.readN(int(n))
		if err != nil {
			return nil, err
		}
		out := make([]byte, len(b))
		copy(out, b)
		return out, nil
	case 3: // text string
		n, err := d.readLen(addl)
		if err != nil {
			return nil, err
		}
		b, err := d.readN(int(n))
		if err != nil {
			return nil, err
		}
		return string(b), nil
	case 4: // array
		n, err := d.readLen(addl)
		if err != nil {
			return nil, err
		}
		out := make([]any, 0, n)
		for i := uint64(0); i < n; i++ {
			v, err := d.decodeValue()
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case 5: // map
		n, err := d.readLen(addl)
		if err != nil {
			return nil, err
		}
		out := make(map[any]any, n)
		for i := uint64(0); i < n; i++ {
			k, err := d.decodeValue()
			if err != nil {
				return nil, err
			}
			v, err := d.decodeValue()
			if err != nil {
				return nil, err
			}
			out[k] = v
		}
		return out, nil
	case 7: // simple values and floats
		switch addl {
		case 20:
			return false, nil
		case 21:
			return true, nil
		case 22:
			return nil, nil
		case 25:
			b, err := d.readN(2)
			if err != nil {
				return nil, err
			}
			return float16BitsToFloat64(binary.BigEndian.Uint16(b)), nil
		case 26:
			b, err := d.readN(4)
			if err != nil {
				return nil, err
			}
			return float64(math.Float32frombits(binary.BigEndian.Uint32(b))), nil
		case 27:
			b, err := d.readN(8)
			if err != nil {
				return nil, err
			}
			return math.Float64frombits(binary.BigEndian.Uint64(b)), nil
		default:
			return nil, fmt.Errorf("wrap: cbor: unsupported simple/float additional info %d", addl)
		}
	default:
		return nil, fmt.Errorf("wrap: cbor: unsupported major type %d", major)
	}
}

// DecodeCBOR parses b as CBOR and verifies it is the unique canonical
// encoding of the value it decodes to, per WRAP's signature-verification
// order (04-signing.md §5.4 step 1): a receiver MUST reject a non-canonical
// encoding rather than accept it and re-encode. It is verified here by doing
// exactly that check — decoding, then re-encoding the result and requiring a
// byte-for-byte match — rather than re-deriving every individual canonicality
// rule on the read path.
//
// The returned value is map[any]any for a CBOR map (keys are uint64 or
// string, matching whichever the encoder used), []any for an array, and
// uint64 / int64 / []byte / string / bool / float64 / nil for scalars.
//
// This is the byte-level primitive. Decode (object.go) is the object-level
// entry point most callers want: it parses a signed envelope, recomputes the
// id and verifies the signature on top of this.
func DecodeCBOR(b []byte) (any, error) {
	d := &decoder{b: b}
	v, err := d.decodeValue()
	if err != nil {
		return nil, err
	}
	if d.i != len(b) {
		return nil, errors.New("wrap: cbor: trailing bytes after top-level value")
	}
	re, err := Encode(v)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(re, b) {
		return nil, ErrNotCanonical
	}
	return v, nil
}

// ── typed accessors over a decoded map[any]any ─────────────────────────────

func mapGet(m map[any]any, key uint64) (any, bool) {
	v, ok := m[key]
	return v, ok
}

func getUint(m map[any]any, key uint64) (uint64, bool) {
	v, ok := mapGet(m, key)
	if !ok {
		return 0, false
	}
	n, ok := v.(uint64)
	return n, ok
}

func getBytes(m map[any]any, key uint64) ([]byte, bool) {
	v, ok := mapGet(m, key)
	if !ok {
		return nil, false
	}
	b, ok := v.([]byte)
	return b, ok
}

func getString(m map[any]any, key uint64) (string, bool) {
	v, ok := mapGet(m, key)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func getMapField(m map[any]any, key uint64) (map[any]any, bool) {
	v, ok := mapGet(m, key)
	if !ok {
		return nil, false
	}
	mm, ok := v.(map[any]any)
	return mm, ok
}

func getArray(m map[any]any, key uint64) ([]any, bool) {
	v, ok := mapGet(m, key)
	if !ok {
		return nil, false
	}
	a, ok := v.([]any)
	return a, ok
}
