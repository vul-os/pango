package sync

// How each replicated table merges (docs/SYNC.md §3), as data.
//
// This used to be a bare `unionTables` set read only by applyToTable. It is now
// the single place the question is answered, because a second consumer arrived:
// the substrate binding (internal/sync/substrate) has to map every replicated
// table onto one of the shared algebra's two ownership classes, and a table
// classified one way by the built-in engine and the other way by the substrate
// engine would converge differently on the two — which is silent divergence,
// the failure mode neither engine reports.
//
// So there is one list, exported, and classes_test.go holds it against the live
// schema and against the journal's actual writers. applyToTable still reads the
// COLUMN set from SQLite's own catalogue rather than from here (see its doc):
// this map says how a table merges, never what shape it is.

// MergeClass names the merge rule for a replicated table.
type MergeClass int

const (
	// ClassUnknown is a table this node has no merge rule for: not replicated,
	// or a newer peer's table this build has never heard of. Ops for it are
	// dropped rather than guessed at.
	ClassUnknown MergeClass = iota
	// ClassRegister is last-writer-wins per row, arbitrated by HLC. Reference
	// data and the single-writer aggregates: the newest edit wins on every
	// node, and a deletion is a replicated `deleted` flag like any other write.
	ClassRegister
	// ClassUnion is add-only: rows are immutable once written and applying an
	// op either inserts a new row or is a no-op. Money, hours, evidence and
	// content-addressed blobs — see docs/SYNC.md §4 for why a stored total
	// under last-writer-wins loses one of two concurrent entries silently.
	ClassUnion
)

func (c MergeClass) String() string {
	switch c {
	case ClassRegister:
		return "register"
	case ClassUnion:
		return "union"
	default:
		return "unknown"
	}
}

// MergeClasses is the rule per replicated table. A table absent from this map
// is not replicated by this build.
//
// Adding a table to the schema is not enough to replicate it: it needs a
// store.Journal writer AND an entry here. classes_test.go fails if those two
// disagree, which is what stops a new table from quietly acquiring whichever
// rule the code happens to fall through to.
var MergeClasses = map[string]MergeClass{
	// Reference data and single-writer aggregates.
	"organisation":             ClassRegister,
	"party":                    ClassRegister,
	"building":                 ClassRegister,
	"unit":                     ClassRegister,
	"inspection_template":      ClassRegister,
	"inspection_template_item": ClassRegister,
	"inspection":               ClassRegister,
	"job":                      ClassRegister,

	// Append-only. None of these carries a `deleted` column, because nothing
	// here is ever retracted — a correction is a new row (docs/SYNC.md §4).
	"cost_entry": ClassUnion,
	"time_entry": ClassUnion,
	"job_event":  ClassUnion,
	"finding":    ClassUnion,
	"attachment": ClassUnion,
}

// ClassOf reports how tbl merges.
func ClassOf(tbl string) MergeClass { return MergeClasses[tbl] }
