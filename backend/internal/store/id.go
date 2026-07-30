package store

// Identifiers must be mintable offline. A database sequence or an
// auto-increment column would need coordination the moment two nodes both
// accept writes while partitioned — exactly the case Pango is built for
// (ARCHITECTURE §2.1) — so ids are ULID-shaped: sortable by creation time and
// collision-safe without anyone asking anyone's permission.
//
// Job NUMBERS are a separate concern: they are per-building sequences allocated
// by the building's owning organisation (§5), because humans have to say them
// out loud.

import (
	"crypto/rand"
	"encoding/binary"
	"time"
)

// Crockford base32 alphabet (ULID-style): sortable, unambiguous, URL-safe.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewID returns a 26-char ULID-like identifier: a 48-bit millisecond timestamp
// plus 80 bits of randomness, Crockford base32 encoded.
func NewID() string {
	ms := uint64(time.Now().UnixMilli())

	var buf [16]byte
	binary.BigEndian.PutUint64(buf[0:8], ms<<16) // top 48 bits are the timestamp
	_, _ = rand.Read(buf[6:16])                  // low 80 bits random

	var out [26]byte
	out[0] = crockford[(buf[0]&224)>>5]
	out[1] = crockford[buf[0]&31]
	out[2] = crockford[(buf[1]&248)>>3]
	out[3] = crockford[((buf[1]&7)<<2)|((buf[2]&192)>>6)]
	out[4] = crockford[(buf[2]&62)>>1]
	out[5] = crockford[((buf[2]&1)<<4)|((buf[3]&240)>>4)]
	out[6] = crockford[((buf[3]&15)<<1)|((buf[4]&128)>>7)]
	out[7] = crockford[(buf[4]&124)>>2]
	out[8] = crockford[((buf[4]&3)<<3)|((buf[5]&224)>>5)]
	out[9] = crockford[buf[5]&31]
	out[10] = crockford[(buf[6]&248)>>3]
	out[11] = crockford[((buf[6]&7)<<2)|((buf[7]&192)>>6)]
	out[12] = crockford[(buf[7]&62)>>1]
	out[13] = crockford[((buf[7]&1)<<4)|((buf[8]&240)>>4)]
	out[14] = crockford[((buf[8]&15)<<1)|((buf[9]&128)>>7)]
	out[15] = crockford[(buf[9]&124)>>2]
	out[16] = crockford[((buf[9]&3)<<3)|((buf[10]&224)>>5)]
	out[17] = crockford[buf[10]&31]
	out[18] = crockford[(buf[11]&248)>>3]
	out[19] = crockford[((buf[11]&7)<<2)|((buf[12]&192)>>6)]
	out[20] = crockford[(buf[12]&62)>>1]
	out[21] = crockford[((buf[12]&1)<<4)|((buf[13]&240)>>4)]
	out[22] = crockford[((buf[13]&15)<<1)|((buf[14]&128)>>7)]
	out[23] = crockford[(buf[14]&124)>>2]
	out[24] = crockford[((buf[14]&3)<<3)|((buf[15]&224)>>5)]
	out[25] = crockford[buf[15]&31]
	return string(out[:])
}
