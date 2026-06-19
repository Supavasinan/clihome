// Package plan computes what a sync would change between two homes: per manifest
// entry, the files that are new / changed / removed. It is read-only (no writes).
package plan

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Unit is a single file/symlink that would change.
type Unit struct {
	Rel  string // home-relative path
	Src  string // source absolute path ("" for removed)
	Dst  string // destination absolute path
	Kind string // "new" | "changed" | "removed"
}

// Entry is one manifest entry (a file or a directory).
type Entry struct {
	Rel        string
	State      string // "change" | "insync" | "skip"
	Nw, Ch, Rm int
	Units      []Unit
}

// Plan is the full diff between a source and destination home.
type Plan struct {
	Entries                  []Entry
	NChanged, Files, Removed int
}

// Sync diffs srcDir onto dstDir. With all=false it follows the provider manifest;
// with all=true it mirrors the whole home except paths the deny predicate rejects.
// del controls whether destination-only files are reported as "removed".
func Sync(srcDir, dstDir string, manifest []string, deny func(string) bool, all, del bool) Plan {
	var rels []string
	denyFn := func(string) bool { return false }
	if all {
		denyFn = deny
		rels = resolveAll(srcDir, deny)
	} else {
		rels = resolve(srcDir, manifest)
	}
	var p Plan
	for _, rel := range rels {
		e := planEntry(srcDir, dstDir, rel, del, denyFn)
		p.Entries = append(p.Entries, e)
		if e.State == "change" {
			p.NChanged++
			p.Files += e.Nw + e.Ch
			p.Removed += e.Rm
		}
	}
	return p
}

func planEntry(srcDir, dstDir, rel string, del bool, deny func(string) bool) Entry {
	src := filepath.Join(srcDir, rel)
	dst := filepath.Join(dstDir, rel)
	if !existsL(src) {
		return Entry{Rel: rel, State: "skip"}
	}
	st, err := os.Lstat(src)
	if err != nil {
		return Entry{Rel: rel, State: "skip"}
	}

	var units []Unit
	if st.IsDir() { // Lstat: symlinks report !IsDir, so this is a real dir
		sf := listFiles(src)
		sset := make(map[string]bool, len(sf))
		for _, r := range sf {
			sset[r] = true
		}
		for _, r := range sf {
			full := rel + "/" + r
			if deny(full) {
				continue
			}
			sp, dp := filepath.Join(src, r), filepath.Join(dst, r)
			switch {
			case !existsL(dp):
				units = append(units, Unit{full, sp, dp, "new"})
			case !sameFile(sp, dp):
				units = append(units, Unit{full, sp, dp, "changed"})
			}
		}
		if del && existsL(dst) {
			for _, r := range listFiles(dst) {
				full := rel + "/" + r
				if deny(full) || sset[r] {
					continue
				}
				units = append(units, Unit{full, "", filepath.Join(dst, r), "removed"})
			}
		}
	} else {
		switch {
		case !existsL(dst):
			units = append(units, Unit{rel, src, dst, "new"})
		case !sameFile(src, dst):
			units = append(units, Unit{rel, src, dst, "changed"})
		}
	}

	e := Entry{Rel: rel, Units: units}
	for _, u := range units {
		switch u.Kind {
		case "new":
			e.Nw++
		case "changed":
			e.Ch++
		case "removed":
			e.Rm++
		}
	}
	if len(units) > 0 {
		e.State = "change"
	} else {
		e.State = "insync"
	}
	return e
}

// ── manifest resolution ──────────────────────────────────────────────────────

func resolve(srcDir string, manifest []string) []string {
	var out []string
	for _, pat := range manifest {
		if strings.Contains(pat, "*") {
			out = append(out, expandGlob(srcDir, pat)...)
		} else {
			out = append(out, pat)
		}
	}
	return out
}

func resolveAll(srcDir string, deny func(string) bool) []string {
	ents, err := os.ReadDir(srcDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if !deny(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func expandGlob(base, pattern string) []string {
	dirs := []string{""}
	for _, part := range strings.Split(pattern, "/") {
		var next []string
		for _, d := range dirs {
			if part == "*" {
				ents, _ := os.ReadDir(filepath.Join(base, d))
				for _, e := range ents {
					if e.IsDir() {
						next = append(next, join(d, e.Name()))
					}
				}
			} else if p := join(d, part); existsL(filepath.Join(base, p)) {
				next = append(next, p)
			}
		}
		dirs = next
	}
	return dirs
}

func join(d, name string) string {
	if d == "" {
		return name
	}
	return d + "/" + name
}

// ── fs helpers ───────────────────────────────────────────────────────────────

func existsL(p string) bool { _, err := os.Lstat(p); return err == nil }

// listFiles returns every file/symlink (not directory) under dir, as relative paths.
func listFiles(dir string) []string {
	var out []string
	var walk func(d, rel string)
	walk = func(d, rel string) {
		ents, err := os.ReadDir(d)
		if err != nil {
			return
		}
		for _, e := range ents {
			r := join(rel, e.Name())
			if e.IsDir() { // symlinks report !IsDir, so they fall through to the file branch
				walk(filepath.Join(d, e.Name()), r)
			} else {
				out = append(out, r)
			}
		}
	}
	walk(dir, "")
	return out
}

// sameFile is symlink-aware: identical link targets, or identical file bytes.
func sameFile(a, b string) bool {
	fa, err := os.Lstat(a)
	if err != nil {
		return false
	}
	fb, err := os.Lstat(b)
	if err != nil {
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
