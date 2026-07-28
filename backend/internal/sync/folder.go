package sync

// Folder sync: files as transport, never as truth (docs/SYNC.md §9).
//
// Instead of (or alongside) dialing peers over HTTP, every node writes the
// ops it authored to an append-only file in a shared folder and imports every
// other node's file back through the same idempotent ApplyOps used by
// network sync. The shared folder can be anything that copies files between
// machines — Syncthing, a NAS mount, a synced drive, or a USB stick carried
// between sites. Because each node writes ONLY its own ops-<pubkey>.jsonl,
// no two machines ever write the same file, so the file-sync layer never has
// a conflict to resolve. The database remains authoritative; the files are a
// durable, replayable log that a brand-new node pointed at the folder could
// rebuild from alone.
//
// DUPLICATION: a near-copy of FlowStock's backend/internal/sync/folder.go —
// same algorithm, different settings-key prefixes. Deliberate for now, tracked
// in docs/SYNC.md §11 ("Duplicated sync substrate"), which also records why
// the two may not import each other.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vul-os/propfix/backend/internal/store"
)

// FolderResult reports one folder-sync round.
type FolderResult struct {
	Dir      string `json:"dir"`
	Exported int    `json:"exported"`
	Imported int    `json:"imported"`
	Files    int    `json:"files"`
	Error    string `json:"error,omitempty"`
}

func exportFileName(nodeID string) string { return "ops-" + nodeID + ".jsonl" }

// FolderSync runs one export-then-import round against dir. Safe to call
// repeatedly; both directions are idempotent.
func (e *Engine) FolderSync(dir string) FolderResult {
	res := FolderResult{Dir: dir}
	if strings.TrimSpace(dir) == "" {
		res.Error = "no sync folder configured"
		return res
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		res.Error = err.Error()
		return res
	}

	if n, err := e.exportOwn(dir); err != nil {
		res.Error = err.Error()
		return res
	} else {
		res.Exported = n
	}
	imported, files, err := e.importOthers(dir)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Imported = imported
	res.Files = files
	return res
}

// exportOwn appends this node's newly-authored ops to its own file. A
// high-water mark in settings records the last exported HLC so exports are
// incremental; if the file has gone missing the mark is reset and the full
// history is rewritten, keeping the file complete for a late joiner reading
// it for the first time.
func (e *Engine) exportOwn(dir string) (int, error) {
	path := filepath.Join(dir, exportFileName(e.NodeID()))
	hwmKey := "sync_folder_export_hwm"
	hwm := e.s.GetSetting(hwmKey)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		hwm = "" // rebuild the full log
	}

	ops, err := e.OwnOpsAfter(hwm)
	if err != nil {
		return 0, err
	}
	if len(ops) == 0 {
		return 0, nil
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	bw := bufio.NewWriter(f)
	last := hwm
	for _, op := range ops {
		line, err := json.Marshal(op)
		if err != nil {
			return 0, err
		}
		if _, err := bw.Write(line); err != nil {
			return 0, err
		}
		if err := bw.WriteByte('\n'); err != nil {
			return 0, err
		}
		last = op.HLC
	}
	if err := bw.Flush(); err != nil {
		return 0, err
	}
	if err := f.Sync(); err != nil {
		return 0, err
	}
	_ = e.s.SetSetting(hwmKey, last)
	return len(ops), nil
}

// importOthers reads every other node's export file and applies the ops it
// has not seen yet. A per-file byte offset in settings makes imports
// incremental; only whole lines past the offset are consumed, so a file still
// being written by a file-sync client is read safely up to its last complete
// line.
func (e *Engine) importOthers(dir string) (int, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0, err
	}
	own := exportFileName(e.NodeID())
	totalApplied, files := 0, 0
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasPrefix(name, "ops-") || !strings.HasSuffix(name, ".jsonl") || name == own {
			continue
		}
		files++
		applied, err := e.importFile(filepath.Join(dir, name), name)
		if err != nil {
			return totalApplied, files, err
		}
		totalApplied += applied
	}
	return totalApplied, files, nil
}

func (e *Engine) importFile(path, name string) (int, error) {
	offKey := "sync_folder_import_off:" + name
	var off int64
	if v := e.s.GetSetting(offKey); v != "" {
		fmt.Sscan(v, &off)
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if info.Size() < off {
		off = 0 // file shrank or was rebuilt: re-read from the start
	}
	if info.Size() == off {
		return 0, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if _, err := f.Seek(off, 0); err != nil {
		return 0, err
	}

	var ops []store.Op
	consumed := off
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		adv := int64(len(line)) + 1 // include the newline the scanner stripped
		var op store.Op
		if err := json.Unmarshal(line, &op); err != nil {
			// A malformed or partial line: stop without advancing past it,
			// so a line still being flushed by the file-sync client is
			// retried whole on the next round.
			break
		}
		ops = append(ops, op)
		consumed += adv
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}
	applied := 0
	if len(ops) > 0 {
		if applied, err = e.ApplyOps(ops); err != nil {
			return 0, err
		}
	}
	_ = e.s.SetSetting(offKey, fmt.Sprintf("%d", consumed))
	return applied, nil
}
