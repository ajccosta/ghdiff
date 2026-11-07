package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// / searches the diff as less and vim do: matches show as you type (from
// where you were), Enter keeps the search, n and N go to the next and
// previous match while it is kept (instead of the next file), Esc while
// typing goes back, and Esc after clears it. Lowercase searches ignore case.
// Each row is searched from its col, so line numbers never match, and the
// text searched is what is drawn (formatted markdown included).

type hit struct{ row, from, to int } // display columns of a row's text

type searchState struct {
	query string
	start int // m.off when / was pressed
	hits  []hit
	cur   int
}

func (m *model) startSearch() {
	m.search = searchState{start: m.off}
	s := &m.search
	m.asking = &prompt{label: "/",
		note: func() string {
			if s.query == "" {
				return ""
			}
			return fmt.Sprintf("  %d %s", len(s.hits), plural(len(s.hits), "match", "matches"))
		},
		change: func(q string) {
			s.query = q
			m.findHits()
			m.goToHit(s.start, 1)
		},
		done: func(string) tea.Cmd {
			if s.query != "" && len(s.hits) == 0 {
				m.flash = "Pattern not found: " + s.query
				m.search = searchState{}
			}
			return nil
		},
		cancel: func() {
			m.off = s.start
			m.search = searchState{}
			m.clamp()
		},
	}
}

// searchNav handles n, N and Esc while a search is kept.
func (m *model) searchNav(k string) bool {
	s := &m.search
	switch {
	case k == "/":
		m.startSearch()
	case s.query == "":
		return false
	case k == "n":
		m.nextHit(1)
	case k == "N":
		m.nextHit(-1)
	case k == "esc":
		m.search = searchState{}
	default:
		return false
	}
	return true
}

// findHits finds the query in every row.
func (m *model) findHits() {
	s := &m.search
	s.hits = nil
	if s.query == "" {
		return
	}
	q := s.query
	fold := strings.ToLower(q) == q
	if fold {
		q = strings.ToLower(q)
	}
	for ri, r := range m.rows {
		text := []rune(ansi.Strip(r.text))
		if r.col >= len(text) {
			continue
		}
		hay := string(text[r.col:])
		if fold {
			hay = strings.ToLower(hay)
		}
		for i := 0; i <= len(hay)-len(q); {
			j := strings.Index(hay[i:], q)
			if j < 0 {
				break
			}
			from := r.col + ansi.StringWidth(hay[:i+j])
			s.hits = append(s.hits, hit{ri, from, from + ansi.StringWidth(q)})
			i += j + len(q)
		}
	}
	s.cur = min(s.cur, max(0, len(s.hits)-1))
}

// goToHit shows the first match at or after row r (dir 1), or at or before it.
func (m *model) goToHit(r, dir int) {
	s := &m.search
	if len(s.hits) == 0 {
		m.off = s.start
		m.clamp()
		return
	}
	s.cur = 0
	if dir < 0 {
		s.cur = len(s.hits) - 1
	}
	for i := range s.hits {
		k := i
		if dir < 0 {
			k = len(s.hits) - 1 - i
		}
		if dir > 0 && s.hits[k].row >= r || dir < 0 && s.hits[k].row <= r {
			s.cur = k
			break
		}
	}
	m.showHit()
}

// nextHit moves to the next (1) or previous (-1) match, round the ends.
func (m *model) nextHit(dir int) {
	s := &m.search
	if len(s.hits) == 0 {
		m.flash = "Pattern not found: " + s.query
		return
	}
	s.cur = (s.cur + dir + len(s.hits)) % len(s.hits)
	m.showHit()
}

// showHit scrolls the current match into view, a third of the way down
// when it was off screen.
func (m *model) showHit() {
	r := m.search.hits[m.search.cur].row
	if r < m.off || r >= m.off+m.paneHeight() {
		m.off = r - m.paneHeight()/3
		m.clamp()
	}
	m.pin = -1
	m.treeSel = m.currentFile()
}

// markHits paints the matches on row r's drawn text.
func (m *model) markHits(r int, text string) string {
	s := &m.search
	for i, h := range s.hits {
		if h.row != r {
			continue
		}
		st := lipgloss.NewStyle().Background(m.theme.ModFg).Foreground(lipgloss.Color("0"))
		if i == s.cur {
			st = st.Background(m.theme.Accent)
		}
		w := ansi.StringWidth(text)
		text = ansi.Cut(text, 0, h.from) + st.Render(ansi.Strip(ansi.Cut(text, h.from, h.to))) + ansi.Cut(text, h.to, w)
	}
	return text
}

// searchFoot is the footer while a prompt is open, or where a search is.
func (m *model) searchFoot() string {
	s := &m.search
	t := m.theme
	switch {
	case m.asking != nil:
		return m.promptLine()
	case s.query != "":
		return lipgloss.NewStyle().Foreground(t.Dim).Render(ansi.Truncate(
			fmt.Sprintf(" /%s  match %d/%d · n/N next/prev · esc clear · h help", s.query, s.cur+1, len(s.hits)), m.width, "…"))
	}
	return ""
}
