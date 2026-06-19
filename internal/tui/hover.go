package tui

// hover.go centralizes mouse-hover tracking. Each clickable element gets a live
// "is the mouse over me" flag, recomputed on every mouse event (motion included)
// from the same hit-test helpers the click handlers use — so the hover highlight
// can never drift from where a click actually lands.

// clearHover resets every hover target to "none". Used at init and as the first
// step of each recompute pass.
func (m *Model) clearHover() {
	m.hoverNew = false
	m.crumbHover = -1
	m.sess.Hover = -1
	m.homeHover = -1
	m.gearHover = false
	m.actionHover = -1
	m.heatHoverC, m.heatHoverR = -1, -1
	m.newHover = -1
	m.prefHoverField, m.prefHoverOpt = -1, -1
}

// recomputeHover updates the hover targets for the current screen from the mouse
// position (x, y).
func (m *Model) recomputeHover(x, y int) {
	m.clearHover()

	// The breadcrumb trail sits on the top row of every screen.
	if y == 1 {
		m.crumbHover = m.crumbAt(x)
	}

	switch m.mode {
	case "history":
		if y == 6 { // sortable column headers
			m.sess.Hover = m.sessionColAt(x)
		}
		return
	case "prefs":
		m.prefHoverField, m.prefHoverOpt = m.prefHitAt(x, y)
		return
	case "new":
		m.newHover = m.newRowAt(y)
		return
	}

	// Cockpit modes (browse / actions / activity) all need a focused home.
	if len(m.homes) == 0 {
		return
	}
	if m.showManage() {
		m.actionHover = m.actionRowAt(x, y)
	} else {
		m.homeHover = m.homeRowAt(x, y)
		m.hoverNew = (m.mode == "browse" || m.mode == "activity") &&
			y == 8+len(m.homes) && x >= 0 && x < m.homesW()
		if y == 1 && x >= m.w-9 { // ⚙ prefs button, top-right
			m.gearHover = true
		}
	}
	// The activity heatmap spans the bottom of every cockpit mode.
	if c, r, ok := m.heatCellAt(x, y); ok {
		m.heatHoverC, m.heatHoverR = c, r
	}
}
