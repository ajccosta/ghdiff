package main

import (
	"fmt"
	"strings"
	"testing"
)

// bigDiff is n added files of size lines each.
func bigDiff(n, size int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "diff --git a/f%d.go b/f%d.go\nnew file mode 100644\n--- /dev/null\n+++ b/f%d.go\n@@ -0,0 +1,%d @@\n", i, i, i, size)
		for j := 0; j < size; j++ {
			fmt.Fprintf(&b, "+var x%d = %d\n", j, j)
		}
	}
	return b.String()
}

func TestBigDiffLoadsOnDemand(t *testing.T) {
	d := bigDiff(10, 1000)
	m := newModel(Parse(strings.NewReader(d)), "t", []byte(d), target{})
	m.width, m.height = 120, 40
	m.relayout()
	// 1000+1 lines a file: three fit in loadBudget, the rest wait.
	for i, f := range m.files {
		if want := i >= 3; m.collapsed[i] != want || f.prepared == want {
			t.Fatalf("file %d: collapsed %v, prepared %v", i, m.collapsed[i], f.prepared)
		}
	}
	if m.files[0].Lines[1].Syntax == nil {
		t.Errorf("a loaded file has no syntax")
	}
	m.load(7)
	if m.collapsed[7] || !m.files[7].prepared || m.files[7].Lines[1].Syntax == nil {
		t.Errorf("load did not open file 7")
	}
	if !m.collapsed[8] || m.files[8].prepared {
		t.Errorf("load opened more than file 7")
	}
}

func TestSmallDiffLoadsAll(t *testing.T) {
	m := newModel(Parse(strings.NewReader(twoFiles)), "t", []byte(twoFiles), target{})
	if len(m.collapsed) != 0 {
		t.Errorf("collapsed %v", m.collapsed)
	}
}
