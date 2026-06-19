// Package syncer applies a plan: it copies the changed files from source to
// destination, backing up every overwritten/removed file first so the operation
// is reversible. The account/auth files are never in a plan, so are never touched.
package syncer

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"clihome/internal/plan"
)

// RestoreRoot is where restore points are saved: ~/.clihome/restore.
func RestoreRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".clihome", "restore")
}

// Timestamp is a sortable restore-point stamp, e.g. 20260616-094501.
func Timestamp() string { return time.Now().Format("20060102-150405") }

// Apply copies each changed unit onto the destination, first saving a restore
// point in restoreDir: the current destination version of every overwritten or
// removed file, plus an _added.txt listing newly-created files (so the point is
// a complete rollback). Returns the number of files written.
func Apply(p plan.Plan, restoreDir string) (int, error) {
	applied := 0
	var added []string
	for _, e := range p.Entries {
		if e.State != "change" {
			continue
		}
		for _, u := range e.Units {
			if u.Kind == "new" {
				added = append(added, u.Rel)
			} else if restoreDir != "" && exists(u.Dst) {
				if err := copyAny(u.Dst, filepath.Join(restoreDir, u.Rel)); err != nil {
					return applied, err
				}
			}
			if u.Kind == "removed" {
				if err := os.RemoveAll(u.Dst); err != nil {
					return applied, err
				}
			} else if err := copyAny(u.Src, u.Dst); err != nil {
				return applied, err
			}
			applied++
		}
	}
	if restoreDir != "" && len(added) > 0 {
		_ = os.MkdirAll(restoreDir, 0o755)
		_ = os.WriteFile(filepath.Join(restoreDir, "_added.txt"), []byte(strings.Join(added, "\n")+"\n"), 0o644)
	}
	return applied, nil
}

// RestorePoint is one saved rollback for a home: a timestamped snapshot of the
// files a sync overwrote/removed, plus the list of files it added.
type RestorePoint struct {
	Dir   string // absolute path to the snapshot dir
	Stamp string // timestamp folder name, e.g. 20260616-094501
	Files int    // number of files captured (restorable)
}

// RestorePoints lists every restore point for a home, most recent first.
func RestorePoints(homeName string) []RestorePoint {
	ents, err := os.ReadDir(RestoreRoot())
	if err != nil {
		return nil
	}
	var out []RestorePoint
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(RestoreRoot(), e.Name(), homeName)
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			continue
		}
		out = append(out, RestorePoint{Dir: dir, Stamp: e.Name(), Files: countRestorable(dir)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Stamp > out[j].Stamp })
	return out
}

// countRestorable counts captured files (snapshot files + lines in _added.txt).
func countRestorable(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == "_added.txt" {
			return nil
		}
		n++
		return nil
	})
	for _, rel := range addedList(dir) {
		if rel != "" {
			n++
		}
	}
	return n
}

// RestoreFile is one file captured in a restore point.
type RestoreFile struct {
	Rel   string // home-relative path
	Added bool   // true = a file the sync added (restoring this point removes it)
}

// RestoreFileList returns every file captured in a restore point, sorted: the
// snapshot files (restored on rollback) plus the added-files manifest (removed).
func RestoreFileList(rp RestorePoint) []RestoreFile {
	var out []RestoreFile
	_ = filepath.WalkDir(rp.Dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(rp.Dir, path)
		if rel != "_added.txt" {
			out = append(out, RestoreFile{Rel: rel})
		}
		return nil
	})
	for _, rel := range addedList(rp.Dir) {
		if rel != "" {
			out = append(out, RestoreFile{Rel: rel, Added: true})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out
}

// RestorePlan describes what rolling a home back to rp would change, as a
// plan.Plan: each snapshot file becomes a source unit (copied back over the
// home), and each file the sync added becomes a "removed" unit. This lets the
// restore flow reuse the same file-selection + diff UI as a normal sync.
func RestorePlan(rp RestorePoint, homeDir string) plan.Plan {
	var p plan.Plan
	add := func(u plan.Unit) {
		e := plan.Entry{Rel: u.Rel, State: "change", Units: []plan.Unit{u}}
		switch u.Kind {
		case "new":
			e.Nw = 1
		case "changed":
			e.Ch = 1
		case "removed":
			e.Rm = 1
		}
		p.Entries = append(p.Entries, e)
		p.NChanged++
		p.Files += e.Nw + e.Ch
		p.Removed += e.Rm
	}
	_ = filepath.WalkDir(rp.Dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(rp.Dir, path)
		if rel == "_added.txt" {
			return nil
		}
		dst := filepath.Join(homeDir, rel)
		switch {
		case !exists(dst):
			add(plan.Unit{Rel: rel, Src: path, Dst: dst, Kind: "new"})
		case !sameFile(path, dst): // already matches the snapshot → nothing to restore
			add(plan.Unit{Rel: rel, Src: path, Dst: dst, Kind: "changed"})
		}
		return nil
	})
	for _, rel := range addedList(rp.Dir) {
		if rel == "" {
			continue
		}
		if dst := filepath.Join(homeDir, rel); exists(dst) { // already gone → nothing to remove
			add(plan.Unit{Rel: rel, Src: "", Dst: dst, Kind: "removed"})
		}
	}
	sort.Slice(p.Entries, func(i, j int) bool { return p.Entries[i].Rel < p.Entries[j].Rel })
	return p
}

// Restore rolls a home back to a restore point: snapshot files are copied back
// over the home, and files the sync added are removed. Returns files changed.
func Restore(rp RestorePoint, homeDir string) (int, error) {
	changed := 0
	err := filepath.WalkDir(rp.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(rp.Dir, p)
		if rel == "_added.txt" {
			return nil
		}
		if e := copyAny(p, filepath.Join(homeDir, rel)); e != nil {
			return e
		}
		changed++
		return nil
	})
	if err != nil {
		return changed, err
	}
	for _, rel := range addedList(rp.Dir) {
		if rel == "" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(homeDir, rel)); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

// addedList reads the _added.txt manifest of files a sync newly created.
func addedList(dir string) []string {
	b, err := os.ReadFile(filepath.Join(dir, "_added.txt"))
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

// sameFile reports whether two paths have identical content (symlink-aware):
// same link target, or same bytes.
func sameFile(a, b string) bool {
	fa, e1 := os.Lstat(a)
	fb, e2 := os.Lstat(b)
	if e1 != nil || e2 != nil {
		return false
	}
	aSym := fa.Mode()&os.ModeSymlink != 0
	bSym := fb.Mode()&os.ModeSymlink != 0
	if aSym || bSym {
		if aSym && bSym {
			ta, _ := os.Readlink(a)
			tb, _ := os.Readlink(b)
			return ta == tb
		}
		return false
	}
	if fa.Size() != fb.Size() {
		return false
	}
	ba, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	bb, err := os.ReadFile(b)
	if err != nil {
		return false
	}
	return bytes.Equal(ba, bb)
}

// copyAny copies a file or symlink (symlinks preserved verbatim).
func copyAny(src, dst string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		_ = os.Remove(dst)
		return os.Symlink(target, dst)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	_ = os.Remove(dst)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fi.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
