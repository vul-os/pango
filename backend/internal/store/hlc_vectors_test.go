package store

// Runs conformance/hlc_vectors.json against this package's HLC.
//
// The point of a vector file rather than more Go assertions: PropFix's HLC and
// FlowStock's are near-identical copies that nothing forces to stay identical
// (docs/SYNC.md §11). Hand-written tests in two repositories drift silently.
// A shared file does not — either both implementations run it or the one that
// does not is visibly not running it.
//
// The file lives at the repository root, not in testdata/, because it is meant
// to be read and copied by a sibling product. Nothing outside tests reads it.

import (
	"encoding/json"
	"math/rand"
	"os"
	"sort"
	"testing"
)

const hlcVectorsPath = "../../../conformance/hlc_vectors.json"

type hlcVectors struct {
	VectorsVersion int               `json:"vectors_version"`
	StampFormat    string            `json:"stamp_format"`
	MaxWallMS      int64             `json:"max_wall_ms"`
	MaxCounter     uint32            `json:"max_counter"`
	Keys           map[string]string `json:"keys"`

	Parse []struct {
		ID       string `json:"id"`
		TS       string `json:"ts"`
		OK       bool   `json:"ok"`
		MS       int64  `json:"ms"`
		Counter  uint32 `json:"counter"`
		Tiebreak string `json:"tiebreak"`
	} `json:"parse"`

	Order []struct {
		ID      string `json:"id"`
		Lesser  string `json:"lesser"`
		Greater string `json:"greater"`
		Why     string `json:"why"`
	} `json:"order"`

	Sorted struct {
		ID        string   `json:"id"`
		Ascending []string `json:"ascending"`
	} `json:"sorted"`

	Tick []struct {
		ID        string   `json:"id"`
		Author    string   `json:"author"`
		Seed      string   `json:"seed"`
		NowMS     int64    `json:"now_ms"`
		ThenNowMS int64    `json:"then_now_ms"`
		Expect    []string `json:"expect"`
		Why       string   `json:"why"`
	} `json:"tick"`

	Observe []struct {
		ID         string   `json:"id"`
		Author     string   `json:"author"`
		NowMS      int64    `json:"now_ms"`
		Remote     []string `json:"remote"`
		ExpectTick string   `json:"expect_tick"`
		Why        string   `json:"why"`
	} `json:"observe"`
}

func loadHLCVectors(t *testing.T) *hlcVectors {
	t.Helper()
	b, err := os.ReadFile(hlcVectorsPath)
	if err != nil {
		// Not a skip. The file is checked into this repository, so it is
		// either there or somebody deleted the statement of the rule.
		t.Fatalf("reading %s: %v", hlcVectorsPath, err)
	}
	var v hlcVectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("parsing %s: %v", hlcVectorsPath, err)
	}
	return &v
}

// TestHLCConformanceVectors runs every group in the file. Each group asserts
// its own count as well as its content: a vector file that quietly shrank to
// nothing would otherwise pass.
func TestHLCConformanceVectors(t *testing.T) {
	v := loadHLCVectors(t)

	if v.VectorsVersion != 1 {
		t.Fatalf("vectors_version = %d, this runner implements 1", v.VectorsVersion)
	}
	if v.MaxWallMS != MaxWallMS {
		t.Errorf("max_wall_ms = %d, implementation has %d", v.MaxWallMS, int64(MaxWallMS))
	}
	if v.MaxCounter != MaxCounter {
		t.Errorf("max_counter = %d, implementation has %d", v.MaxCounter, uint32(MaxCounter))
	}
	if got, want := v.StampFormat, "{unix_ms:013d}-{counter:04x}-{tiebreak_hex}"; got != want {
		t.Errorf("stamp_format = %q, this runner implements %q", got, want)
	}

	// Coverage floors. These are the groups this runner claims to execute;
	// if the file loses one, the claim must fail rather than pass vacuously.
	if len(v.Parse) < 8 {
		t.Errorf("parse group has %d vectors, expected at least 8", len(v.Parse))
	}
	if len(v.Order) < 4 {
		t.Errorf("order group has %d vectors, expected at least 4", len(v.Order))
	}
	if len(v.Sorted.Ascending) < 6 {
		t.Errorf("sorted group has %d stamps, expected at least 6", len(v.Sorted.Ascending))
	}
	if len(v.Tick) < 5 {
		t.Errorf("tick group has %d vectors, expected at least 5", len(v.Tick))
	}
	if len(v.Observe) < 4 {
		t.Errorf("observe group has %d vectors, expected at least 4", len(v.Observe))
	}

	t.Run("parse", func(t *testing.T) {
		for _, c := range v.Parse {
			t.Run(c.ID, func(t *testing.T) {
				ms, counter, tiebreak, ok := ParseHLC(c.TS)
				if ok != c.OK {
					t.Fatalf("ParseHLC(%q) ok = %v, want %v", c.TS, ok, c.OK)
				}
				if !c.OK {
					return
				}
				if ms != c.MS || counter != c.Counter || tiebreak != c.Tiebreak {
					t.Fatalf("ParseHLC(%q) = (%d, %d, %q), want (%d, %d, %q)",
						c.TS, ms, counter, tiebreak, c.MS, c.Counter, c.Tiebreak)
				}
			})
		}
	})

	t.Run("order", func(t *testing.T) {
		for _, c := range v.Order {
			t.Run(c.ID, func(t *testing.T) {
				if !(c.Lesser < c.Greater) {
					t.Fatalf("string order violated: %q must sort before %q\n  %s", c.Lesser, c.Greater, c.Why)
				}
				// String order is only worth anything if it agrees with the
				// parsed tuple, so check the tuple too.
				lms, lc, lt, lok := ParseHLC(c.Lesser)
				gms, gc, gt, gok := ParseHLC(c.Greater)
				if !lok || !gok {
					t.Fatalf("an order vector carries an unparseable stamp: %q ok=%v, %q ok=%v", c.Lesser, lok, c.Greater, gok)
				}
				if !tupleLess(lms, lc, lt, gms, gc, gt) {
					t.Fatalf("tuple order disagrees with string order for %q < %q\n  %s", c.Lesser, c.Greater, c.Why)
				}
			})
		}
	})

	t.Run(v.Sorted.ID, func(t *testing.T) {
		shuffled := append([]string(nil), v.Sorted.Ascending...)
		rand.New(rand.NewSource(1)).Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		sort.Strings(shuffled)
		for i := range shuffled {
			if shuffled[i] != v.Sorted.Ascending[i] {
				t.Fatalf("sorting as plain strings did not reproduce the vector order at index %d:\n got  %q\n want %q",
					i, shuffled[i], v.Sorted.Ascending[i])
			}
		}
	})

	t.Run("tick", func(t *testing.T) {
		for _, c := range v.Tick {
			t.Run(c.ID, func(t *testing.T) {
				author := v.Keys[c.Author]
				if author == "" {
					t.Fatalf("vector names key %q, which is not in the file's `keys`", c.Author)
				}
				now := c.NowMS
				h := &HLC{author: author, nowFn: func() int64 { return now }}
				if c.Seed != "" {
					// `seed` is this node's OWN journal high-water mark — the
					// vectors' `why` text says so ("restart with a wall clock
					// behind the oplog"). It is not a remote claim, so it goes
					// through the unguarded seeding path the way NewHLC does,
					// not through Observe's drift bound. Two of these vectors
					// seed decades ahead of `now_ms` on purpose; refusing them
					// as drift would be refusing this node its own history.
					h.fold(c.Seed)
				}
				for i, want := range c.Expect {
					if i == 1 && c.ThenNowMS != 0 {
						now = c.ThenNowMS
					}
					if got := h.Tick(); got != want {
						t.Fatalf("tick %d = %q, want %q\n  %s", i, got, want, c.Why)
					}
				}
			})
		}
	})

	t.Run("observe", func(t *testing.T) {
		for _, c := range v.Observe {
			t.Run(c.ID, func(t *testing.T) {
				author := v.Keys[c.Author]
				if author == "" {
					t.Fatalf("vector names key %q, which is not in the file's `keys`", c.Author)
				}
				h := &HLC{author: author, nowFn: func() int64 { return c.NowMS }}
				for _, r := range c.Remote {
					h.Observe(r)
				}
				if got := h.Tick(); got != c.ExpectTick {
					t.Fatalf("tick after observing %v = %q, want %q\n  %s", c.Remote, got, c.ExpectTick, c.Why)
				}
			})
		}
	})
}

// tupleLess is the numeric reading of the order the string form encodes. It
// exists only so the `order` group can check that the two readings agree; the
// product never compares stamps any way but lexically.
func tupleLess(ams int64, ac uint32, at string, bms int64, bc uint32, bt string) bool {
	if ams != bms {
		return ams < bms
	}
	if ac != bc {
		return ac < bc
	}
	return at < bt
}
