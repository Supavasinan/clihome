package tui

import (
	"clihome/internal/home"
	"clihome/internal/plan"
)

// stateOf classifies home i: "base" (the source of truth), "clean"/"drift"
// (matches/differs from base), or "—" (no peer to compare against).
func (m Model) stateOf(i int) string {
	h := m.homes[i]
	if h.Num == 1 {
		if m.hasPeer(h) {
			return "base"
		}
		return "—"
	}
	base := m.baseOf(h)
	if base == nil {
		return "—"
	}
	if plan.Sync(base.Dir, h.Dir, h.Provider.Manifest, h.Provider.Deny, false, false).NChanged > 0 {
		return "drift"
	}
	return "clean"
}

// recompute derives the sync source → target for the focused home and the diff
// between them, driving what the Sync action does.
func (m *Model) recompute() {
	h := &m.homes[m.cursor]
	base := m.baseOf(*h)
	m.fpSrc, m.fpDst = nil, nil
	if base == nil {
		m.fpKind = "single"
		return
	}
	if h.Name == base.Name {
		// The base pushes its config out to whichever peer has drifted.
		peer := m.firstDriftedPeer(*h)
		if peer == nil {
			if m.hasPeer(*h) {
				m.fpKind = "synced-base"
			} else {
				m.fpKind = "single"
			}
			return
		}
		m.fpSrc, m.fpDst = base, peer
	} else {
		// A non-base home pulls from the base.
		m.fpSrc, m.fpDst = base, h
	}
	m.fp = plan.Sync(m.fpSrc.Dir, m.fpDst.Dir, m.fpSrc.Provider.Manifest, m.fpSrc.Provider.Deny, false, false)
	if m.fp.NChanged == 0 {
		m.fpKind = "clean"
	} else {
		m.fpKind = "diff"
	}
}

// firstDriftedPeer returns the first non-base same-tool home that differs from base.
func (m Model) firstDriftedPeer(base home.Home) *home.Home {
	for i := range m.homes {
		if m.homes[i].Provider == base.Provider && m.homes[i].Name != base.Name && m.states[i] == "drift" {
			return &m.homes[i]
		}
	}
	return nil
}

func (m Model) baseOf(h home.Home) *home.Home {
	for i := range m.homes {
		if m.homes[i].Provider == h.Provider && m.homes[i].Num == 1 {
			return &m.homes[i]
		}
	}
	return nil
}

func (m Model) hasPeer(h home.Home) bool {
	n := 0
	for _, x := range m.homes {
		if x.Provider == h.Provider {
			n++
		}
	}
	return n > 1
}
