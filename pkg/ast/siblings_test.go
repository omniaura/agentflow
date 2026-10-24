package ast

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSiblingTools(t *testing.T) {
	dir := t.TempDir()
	for name, src := range map[string]string{
		"a.af":      ".title A\n\nHi.\n\n.agent x\n.tools find, track\n",
		"b.af":      ".tool track\n.param id The id.\n\nTracks.\n\n.output verdict\n.field ok bool Fine?\n",
		"c.af":      ".tool find\n\nFinds.\n",
		"broken.af": ".tool\n",
		"other.txt": ".tool nope\n\nNot .af.\n",
	} {
		os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644)
	}
	got := SiblingTools(filepath.Join(dir, "a.af"))
	var names []string
	for _, s := range got {
		names = append(names, filepath.Base(s.Path)+":"+s.Name())
	}
	// Outputs aren't tools; unparsable and non-.af files are skipped.
	if strings.Join(names, ",") != "b.af:track,c.af:find" {
		t.Fatalf("got %v", names)
	}
	// A file isn't its own sibling, even when it doesn't exist yet.
	if got := SiblingTools(filepath.Join(dir, "c.af")); len(got) != 2 || got[0].Name() != "x" || got[1].Name() != "track" {
		t.Fatalf("siblings of c.af = %+v", got)
	}

	if tool, _, err := FindTool(nil, nil, got, "track"); err != nil || tool.Name != "track" {
		t.Fatalf("FindTool track = %v, %v", tool, err)
	}
	if _, _, err := FindTool(nil, nil, got, "nope"); err == nil || !strings.Contains(err.Error(), "no tool or agent nope in this file or the other .af files of its directory (have track, find)") {
		t.Fatalf("missing tool: %v", err)
	}
	own, _ := NewFile("c.af", []byte(".tool track\n\nAgain.\n"))
	if _, _, err := FindTool(own.Tools, nil, got, "track"); err == nil || !strings.Contains(err.Error(), "track is declared in this file and b.af") {
		t.Fatalf("duplicate tool: %v", err)
	}
}

func TestSubagentCycle(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "b.af"), []byte(".title B\nHi.\n\n.agent b\n.tools a, look\n\nDoes b.\n"), 0o644)
	own, err := NewFile("a.af", []byte(".title A\nHi.\n\n.agent a\n.tools b\n\nDoes a.\n\n.agent c\n.tools b\n\nDoes c.\n"))
	if err != nil {
		t.Fatal(err)
	}
	agents := PackageAgents(own.Agents, SiblingTools(filepath.Join(dir, "a.af")))
	// Through another file of the package.
	if got := SubagentCycle("a", agents); strings.Join(got, ",") != "a,b,a" {
		t.Errorf("a: %v", got)
	}
	// c reaches the a-b cycle but isn't on it.
	if got := SubagentCycle("c", agents); got != nil {
		t.Errorf("c: %v", got)
	}
}
