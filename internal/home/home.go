// Package home discovers and manages the per-tool config home directories.
package home

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"

	"clihome/internal/provider"
)

// Home is one config home directory, e.g. ~/.claude2 or ~/.codex.
type Home struct {
	Provider *provider.Provider
	Name     string // dir name without the dot, e.g. "claude2"
	Num      int    // 1 for the default (~/.claude), 2 for ~/.claude2, …
	Dir      string
}

// Discover finds every ~/.<prefix>[n] directory across all registered providers.
func Discover() []Home {
	base, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}

	res := map[*provider.Provider]*regexp.Regexp{}
	order := map[*provider.Provider]int{}
	for i, p := range provider.All() {
		res[p] = regexp.MustCompile(`^\.` + p.Prefix + `(\d*)$`)
		order[p] = i
	}

	var homes []Home
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		for _, p := range provider.All() {
			m := res[p].FindStringSubmatch(e.Name())
			if m == nil {
				continue
			}
			num := 1
			if m[1] != "" {
				num, _ = strconv.Atoi(m[1])
			}
			homes = append(homes, Home{Provider: p, Name: e.Name()[1:], Num: num, Dir: filepath.Join(base, e.Name())})
			break
		}
	}
	sort.SliceStable(homes, func(i, j int) bool {
		if oi, oj := order[homes[i].Provider], order[homes[j].Provider]; oi != oj {
			return oi < oj
		}
		return homes[i].Num < homes[j].Num
	})
	return homes
}

// Create scaffolds the next free ~/.<prefix><n> for a provider.
func Create(p *provider.Provider) (Home, error) {
	used := map[int]bool{}
	for _, h := range Discover() {
		if h.Provider == p {
			used[h.Num] = true
		}
	}
	n := 1
	for used[n] {
		n++
	}
	name := p.Prefix
	if n > 1 {
		name = p.Prefix + strconv.Itoa(n)
	}
	base, err := os.UserHomeDir()
	if err != nil {
		return Home{}, err
	}
	dir := filepath.Join(base, "."+name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Home{}, err
	}
	return Home{Provider: p, Name: name, Num: n, Dir: dir}, nil
}

// TrashRoot is where deleted homes are moved: ~/.clihome/trash.
func TrashRoot() string {
	base, _ := os.UserHomeDir()
	return filepath.Join(base, ".clihome", "trash")
}

// Delete removes a home without ever rm -rf'ing it: the whole directory is moved
// into ~/.clihome/trash/<timestamp>/<name>, so it can be recovered by hand.
// Returns the trash path the home was moved to.
func Delete(h Home) (string, error) {
	stamp := time.Now().Format("20060102-150405")
	dst := filepath.Join(TrashRoot(), stamp, h.Name)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	// Try a rename first (fast, same filesystem); fall back to copy+remove when
	// the home and the trash live on different volumes.
	if err := os.Rename(h.Dir, dst); err != nil {
		if err := copyTree(h.Dir, dst); err != nil {
			return "", err
		}
		if err := os.RemoveAll(h.Dir); err != nil {
			return dst, err
		}
	}
	return dst, nil
}

// copyTree recursively copies a directory tree (files + symlinks), used as the
// cross-filesystem fallback for Delete.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

// LastActive returns the newest mtime among the provider's active files, or zero.
func LastActive(h Home) time.Time {
	var t time.Time
	for _, f := range h.Provider.ActiveFiles {
		if st, err := os.Stat(filepath.Join(h.Dir, f)); err == nil && st.ModTime().After(t) {
			t = st.ModTime()
		}
	}
	return t
}
