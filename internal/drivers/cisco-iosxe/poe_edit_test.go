package ciscoiosxe

import (
	"strings"
	"testing"
)

func TestPoEEditDoesNotRemoveAbsentModes(t *testing.T) {
	root, err := decode([]byte(`<port><power><inline><auto/></inline></power></port>`))
	if err != nil {
		t.Fatal(err)
	}
	edit := poeEdit("off", root.child("port"))
	if strings.Contains(edit, "<static") || strings.Contains(edit, "<never xmlns") || !strings.Contains(edit, "<never/>") {
		t.Fatalf("unsafe power edit: %s", edit)
	}
	if strings.Contains(edit, `operation="remove"`) {
		t.Fatal("off introduced mode removals")
	}
	edit = poeEdit("off", node{})
	if strings.Contains(edit, `operation="remove"`) {
		t.Fatal("default mode issued an unnecessary reset")
	}
	root, err = decode([]byte(`<port><power><inline><never/></inline></power></port>`))
	if err != nil {
		t.Fatal(err)
	}
	edit = poeEdit("auto", root.child("port"))
	if !strings.Contains(edit, `nc:operation="remove"`) || strings.Contains(edit, "<auto") {
		t.Fatalf("auto must only remove never: %s", edit)
	}
}
