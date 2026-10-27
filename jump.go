package main

import (
	"strconv"
	"strings"
)

// GHDIFF_GOTO=path:side:start:end (side RIGHT or LEFT, GitHub's names) opens
// the diff at those lines with them selected; gh-dash sets it when a review
// comment's code is clicked. Once the PR's review threads load, the thread on
// those lines is opened too.

type jump struct {
	path       string
	side       string
	start, end int
	placed     bool // the view has been moved there
}

func parseJump(s string) *jump {
	parts := strings.Split(s, ":")
	if len(parts) < 4 {
		return nil
	}
	n := len(parts)
	start, err1 := strconv.Atoi(parts[n-2])
	end, err2 := strconv.Atoi(parts[n-1])
	if err1 != nil || err2 != nil || end <= 0 {
		return nil
	}
	if start <= 0 || start > end {
		start = end
	}
	return &jump{path: strings.Join(parts[:n-3], ":"), side: parts[n-3], start: start, end: end}
}

// applyJump moves to the jump's lines once there is a layout, and opens the
// review thread on them once threads have loaded.
func (m *model) applyJump() {
	j := m.jump
	if j == nil || m.width == 0 {
		return
	}
	fi := -1
	for i, f := range m.files {
		if f.Path() == j.path {
			fi = i
			break
		}
	}
	if fi < 0 {
		m.flash = j.path + " is not in this diff"
		m.jump = nil
		return
	}
	f := m.files[fi]
	hi := findLine(f, j.side, j.end)
	lo := findLine(f, j.side, j.start)
	if hi < 0 {
		if !j.placed {
			m.jumpTo(fi)
			m.flash = "Those lines are no longer in the diff"
		}
		m.jump = nil
		return
	}
	if lo < 0 || lo > hi {
		lo = hi
	}
	for _, t := range m.threadsAt(fi, hi) {
		if t.lo == lo && t.hi == hi {
			m.focus = focusDiff
			m.showThread(t)
			m.jump = nil
			return
		}
	}
	if j.placed {
		return
	}
	j.placed = true
	if m.collapsed[fi] {
		delete(m.collapsed, fi)
		m.relayout()
	}
	m.focus = focusDiff
	m.sel = selection{on: true, anchored: true, file: fi, anchor: lo, cursor: hi}
	if top := m.firstRow(fi, lo); top >= 0 {
		m.off = top - m.paneHeight()/3
		m.clamp()
		m.pin = -1
		m.treeSel = fi
	}
	m.showCursor()
	if m.fetchThreads() == nil { // no threads coming
		m.jump = nil
	}
}
