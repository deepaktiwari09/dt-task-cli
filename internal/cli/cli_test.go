package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deepaktiwari09/dt-task-cli/internal/store"
)

func TestCLIProjectAndDailyFlow(t *testing.T) {
	global := filepath.Join(t.TempDir(), "global")
	project := t.TempDir()
	home := t.TempDir()
	t.Setenv("DT_TASK_HOME", global)
	t.Setenv("HOME", home)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	root, app, err := New()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	app.Out = &output
	root.SetArgs([]string{"init", "--alias", "demo"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	root.SetArgs([]string{"--project", "demo", "task", "create", "--title", "Write tests", "--problem", "Coverage is low", "--outcome", "Regression is caught", "--acceptance", "Command passes", "--estimate", "20"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	root.SetArgs([]string{"--project", "demo", "task", "status", "1", "planned"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	root.SetArgs([]string{"--project", "demo", "day", "start"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Write tests") {
		t.Fatalf("daily output missing task: %s", output.String())
	}
}

func TestJSONEnvelopeGoldenShape(t *testing.T) {
	t.Setenv("DT_TASK_HOME", filepath.Join(t.TempDir(), "global"))
	t.Setenv("HOME", t.TempDir())
	root, app, err := New()
	if err != nil {
		t.Fatal(err)
	}
	app.JSON = true
	var output bytes.Buffer
	app.Out = &output
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(output.String())
	if !strings.HasPrefix(got, `{`) || !strings.Contains(got, `"version":1`) || !strings.Contains(got, `"data"`) {
		t.Fatalf("unexpected JSON envelope: %s", got)
	}
}

func TestSkillInstallAndStatus(t *testing.T) {
	t.Setenv("DT_TASK_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", filepath.Join(os.Getenv("HOME"), "codex"))
	root, app, err := New()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	app.Out = &output
	root.SetArgs([]string{"skill"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	root.SetArgs([]string{"--json", "skill", "--status"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "current") {
		t.Fatalf("skill status missing current: %s", output.String())
	}
}

func TestSkillStatusIsReadOnlyAndNonZeroWhenMissing(t *testing.T) {
	global := filepath.Join(t.TempDir(), "global")
	home := t.TempDir()
	t.Setenv("DT_TASK_HOME", global)
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))
	root, app, err := New()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	app.Out = &output
	root.SetArgs([]string{"--json", "skill", "--status"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected missing skill status failure")
	}
	if _, err := os.Stat(global); !os.IsNotExist(err) {
		t.Fatalf("status created global state: %v", err)
	}
	if !strings.Contains(output.String(), "missing") {
		t.Fatalf("status output = %s", output.String())
	}
}

func TestSkillInstallRollsBackFirstTarget(t *testing.T) {
	global := filepath.Join(t.TempDir(), "global")
	home := t.TempDir()
	t.Setenv("DT_TASK_HOME", global)
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))
	if err := os.MkdirAll(filepath.Join(home, ".agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".agents", "skills"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	firstTarget := filepath.Join(home, "codex", "skills", "dt-task")
	if err := os.MkdirAll(filepath.Join(firstTarget, "agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(firstTarget, "SKILL.md"), []byte("custom skill"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(firstTarget, "agents", "openai.yaml"), []byte("custom: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, _, err := New()
	if err != nil {
		t.Fatal(err)
	}
	root.SetArgs([]string{"skill"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected second target failure")
	}
	data, err := os.ReadFile(filepath.Join(firstTarget, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "custom skill" {
		t.Fatalf("first target was not rolled back: %q", data)
	}
}

func TestTaskEditPreservesOriginalOnInvalidEditorOutput(t *testing.T) {
	global, project, home := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("DT_TASK_HOME", global)
	t.Setenv("HOME", home)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	root, _, err := New()
	if err != nil {
		t.Fatal(err)
	}
	root.SetArgs([]string{"init", "--alias", "edit-test"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	root.SetArgs([]string{"task", "create", "--title", "Keep me", "--problem", "p", "--outcome", "o", "--acceptance", "a", "--estimate", "10"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(project, ".task", "tasks", "0001.[backlog].keep-me.md")
	original, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(home, "bad-editor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'invalid markdown\\n' > \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DT_TASK_EDITOR", script)
	root.SetArgs([]string{"task", "edit", "1"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected invalid editor failure")
	}
	updated, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != string(original) {
		t.Fatal("invalid editor output changed original task")
	}
}

func TestTaskLifecycleArchiveRestorePurge(t *testing.T) {
	global, project, home := filepath.Join(t.TempDir(), "global"), t.TempDir(), t.TempDir()
	t.Setenv("DT_TASK_HOME", global)
	t.Setenv("HOME", home)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	root, _, err := New()
	if err != nil {
		t.Fatal(err)
	}
	commands := [][]string{
		{"init", "--alias", "life"},
		{"task", "create", "--title", "Lifecycle", "--problem", "p", "--outcome", "o", "--acceptance", "a", "--estimate", "10"},
		{"task", "status", "1", "planned"},
		{"task", "status", "1", "in-progress"},
		{"task", "status", "1", "blocked", "--blocker", "waiting"},
		{"task", "status", "1", "resume", "--blocker="},
		{"task", "status", "1", "done"},
		{"task", "archive", "1"},
		{"task", "status", "1", "in-progress"},
		{"task", "status", "1", "done"},
		{"task", "delete", "1"},
		{"task", "restore", "1"},
		{"task", "delete", "1"},
	}
	for _, args := range commands {
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	root.SetArgs([]string{"task", "purge", "1"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected purge confirmation")
	}
	root.SetArgs([]string{"task", "purge", "1", "--yes"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".task", "trash", "0001.[done].lifecycle.md")); !os.IsNotExist(err) {
		t.Fatalf("trash task remains: %v", err)
	}
}

func TestTimerAndDayEndPersistSession(t *testing.T) {
	global, project, home := filepath.Join(t.TempDir(), "global"), t.TempDir(), t.TempDir()
	t.Setenv("DT_TASK_HOME", global)
	t.Setenv("HOME", home)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	root, _, err := New()
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "--alias", "timer"},
		{"task", "create", "--title", "Timed", "--problem", "p", "--outcome", "o", "--acceptance", "a", "--estimate", "20"},
		{"day", "start", "--add", "1"},
		{"task", "start", "1"},
		{"task", "stop", "1", "--minutes", "5"},
	} {
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	s := store.Store{GlobalRoot: global}
	date := ""
	if journals, listErr := s.ListJournals(); listErr == nil && len(journals) == 1 {
		date = journals[0].Date
	} else {
		t.Fatalf("journals: %v", listErr)
	}
	j, err := s.LoadJournal(date)
	if err != nil || len(j.Sessions) != 1 || j.Sessions[0].Minutes != 5 {
		t.Fatalf("session = %#v, err=%v", j.Sessions, err)
	}
	root.SetArgs([]string{"day", "end", "--note", "done"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	j, err = s.LoadJournal(date)
	if err != nil {
		t.Fatal(err)
	}
	if len(j.Sessions) != 1 || len(j.Notes) != 1 || j.Notes[0] != "done" {
		t.Fatalf("day end lost session/notes: %#v", j)
	}
	if state, stateErr := s.LoadGlobalState(); stateErr != nil || state.ActiveTimer != nil {
		t.Fatalf("active timer remains: %#v, err=%v", state, stateErr)
	}
}

func TestInterruptedTimerRecoveryFlags(t *testing.T) {
	global, project, home := filepath.Join(t.TempDir(), "global"), t.TempDir(), t.TempDir()
	t.Setenv("DT_TASK_HOME", global)
	t.Setenv("HOME", home)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	root, app, err := New()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	app.Out = &output
	for _, args := range [][]string{
		{"init", "--alias", "recover"},
		{"task", "create", "--title", "Recover", "--problem", "p", "--outcome", "o", "--acceptance", "a", "--estimate", "20"},
		{"task", "start", "1"},
	} {
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
	}
	s := store.Store{GlobalRoot: global}
	state, err := s.LoadGlobalState()
	if err != nil || state.ActiveTimer == nil {
		t.Fatalf("timer = %#v, err=%v", state, err)
	}
	state.ActiveTimer.Session.StartedAt = time.Now().Add(-25 * time.Hour)
	if err := s.SaveGlobalState(state); err != nil {
		t.Fatal(err)
	}
	root.SetArgs([]string{"day", "start"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "interrupted timer") {
		t.Fatalf("missing recovery warning: %s", output.String())
	}
	root.SetArgs([]string{"task", "stop", "1", "--continue"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	root.SetArgs([]string{"task", "stop", "1", "--continue=false", "--adjust", "--minutes", "3"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	state, err = s.LoadGlobalState()
	if err != nil || state.ActiveTimer != nil {
		t.Fatalf("timer not resolved: %#v, err=%v", state, err)
	}
}

func TestDoctorFixesFilenameWithBackup(t *testing.T) {
	global, project, home := filepath.Join(t.TempDir(), "global"), t.TempDir(), t.TempDir()
	t.Setenv("DT_TASK_HOME", global)
	t.Setenv("HOME", home)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	root, _, err := New()
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "--alias", "doctor"},
		{"task", "create", "--title", "Broken name", "--problem", "p", "--outcome", "o", "--acceptance", "a", "--estimate", "10"},
	} {
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
	}
	oldPath := filepath.Join(project, ".task", "tasks", "0001.[backlog].broken-name.md")
	badPath := filepath.Join(project, ".task", "tasks", "0001.[backlog].wrong.md")
	if err := os.Rename(oldPath, badPath); err != nil {
		t.Fatal(err)
	}
	root.SetArgs([]string{"doctor"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected doctor issue")
	}
	root.SetArgs([]string{"doctor", "--fix"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("canonical task missing: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(global, "backups", "doctor"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("doctor backup missing: %v", err)
	}
}

func TestProjectRenameSynchronizesJournalReferences(t *testing.T) {
	global, project, home := filepath.Join(t.TempDir(), "global"), t.TempDir(), t.TempDir()
	t.Setenv("DT_TASK_HOME", global)
	t.Setenv("HOME", home)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	root, _, err := New()
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "--alias", "old-name"},
		{"task", "create", "--title", "Rename me", "--problem", "p", "--outcome", "o", "--acceptance", "a", "--estimate", "10"},
		{"day", "start", "--add", "1"},
		{"project", "rename", "old-name", "new-name"},
	} {
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	s := store.Store{GlobalRoot: global}
	config, err := s.LoadProjectConfig(project)
	if err != nil || config.Alias != "new-name" {
		t.Fatalf("project config = %#v, err=%v", config, err)
	}
	journals, err := s.ListJournals()
	if err != nil || len(journals) != 1 || len(journals[0].Planned) != 1 || journals[0].Planned[0].Project != "new-name" {
		t.Fatalf("journal refs = %#v, err=%v", journals, err)
	}
}
