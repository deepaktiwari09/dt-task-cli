package skillbundle

import (
	"testing"
)

func TestEmbeddedSkillsValidate(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatal(err)
	}
	want := []string{"dt-task", "dt-task-worktree"}
	got := SkillNames()
	if len(got) != len(want) {
		t.Fatalf("skill names = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("skill names = %#v, want %#v", got, want)
		}
	}
}
