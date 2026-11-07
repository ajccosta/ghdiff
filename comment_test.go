package main

import (
	"strings"
	"testing"
)

func TestComment_AsksForTheText(t *testing.T) {
	m := newSelTestModel(t)
	m.target = target{repo: "o/r", pr: 1}
	m.sel = selection{on: true, anchor: pos{0, 2}, cursor: pos{0, 2}} // "-two"
	m.press("v", "down", "c")
	if m.asking == nil || m.asking.label != "Comment on a.go:2: " {
		t.Fatalf("prompt = %+v", m.asking)
	}
	if !strings.Contains(m.promptLine(), "Comment on a.go:2") {
		t.Fatalf("footer = %q", m.promptLine())
	}
}

func TestComment_NotAcrossHunks(t *testing.T) {
	m := newSelTestModel(t)
	m.target = target{repo: "o/r", pr: 1}
	m.sel = selection{on: true, anchor: pos{0, 2}, cursor: pos{0, 2}}
	m.press("v", "down", "down", "down")
	m.press("c")
	if m.asking != nil || !strings.Contains(m.flash, "span hunks") {
		t.Fatalf("flash = %q", m.flash)
	}
}

func TestComment_NeedsAPR(t *testing.T) {
	m := newSelTestModel(t)
	m.sel = selection{on: true, anchor: pos{0, 2}, cursor: pos{0, 2}}
	m.press("v", "c")
	if m.asking != nil || !strings.Contains(m.flash, "PR") {
		t.Fatalf("flash = %q", m.flash)
	}
}
