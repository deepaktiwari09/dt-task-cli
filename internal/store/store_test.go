package store

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/deepaktiwari09/dt-task-cli/internal/model"
)

func TestProjectInitIsIdempotentAndGitignoreIsCanonical(t *testing.T) {
	root := t.TempDir()
	s := Store{GlobalRoot: filepath.Join(t.TempDir(), "global")}
	if err := s.EnsureGlobal(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("tmp\n/.task/\n/.task/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureProject(root, "demo"); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureProject(root, "demo"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "/.task/") != 1 {
		t.Fatalf("gitignore = %q", data)
	}
}

func TestGitignoreNormalizesTaskVariants(t *testing.T) {
	root := t.TempDir()
	s := Store{GlobalRoot: filepath.Join(t.TempDir(), "global")}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".task\n/.task/\nkeep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureProject(root, "demo"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "/.task/") != 1 || strings.Contains(string(data), ".task\n") {
		t.Fatalf("gitignore = %q", data)
	}
}

func TestTaskRoundTrip(t *testing.T) {
	root := t.TempDir()
	s := Store{GlobalRoot: filepath.Join(t.TempDir(), "global")}
	if err := s.EnsureProject(root, "demo"); err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: 1, Title: "Round trip", Status: model.StatusBacklog, Priority: "P2", EstimateMinutes: 20, RemainingMinutes: 20, Body: "## Problem\n\nExample"}
	path, err := s.SaveTask(root, task)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadTask(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != task.ID || got.Body != task.Body {
		t.Fatalf("got %#v", got)
	}
}

func TestSaveTaskRejectsFilenameCollision(t *testing.T) {
	root := t.TempDir()
	s := Store{GlobalRoot: filepath.Join(t.TempDir(), "global")}
	if err := s.EnsureProject(root, "demo"); err != nil {
		t.Fatal(err)
	}
	first := model.Task{ID: 1, Title: "One", Status: model.StatusBacklog, Priority: "P2", EstimateMinutes: 20, RemainingMinutes: 20}
	second := model.Task{ID: 2, Title: "Two", Status: model.StatusBacklog, Priority: "P2", EstimateMinutes: 20, RemainingMinutes: 20}
	_, err := s.SaveTask(root, first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveTask(root, second); err != nil {
		t.Fatal(err)
	}
	foreignPath := filepath.Join(ProjectTaskRoot(root), "foreign.md")
	if err := os.WriteFile(foreignPath, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	second.Path = foreignPath
	if _, err := s.SaveTask(root, second); err == nil {
		t.Fatal("expected collision")
	}
}

func BenchmarkListTasks1000(b *testing.B) {
	root := b.TempDir()
	s := Store{GlobalRoot: filepath.Join(b.TempDir(), "global")}
	if err := s.EnsureProject(root, "bench"); err != nil {
		b.Fatal(err)
	}
	b.StopTimer()
	for id := uint64(1); id <= 1000; id++ {
		task := model.Task{ID: id, Title: "Task " + string(rune('A'+id%26)), Status: model.StatusBacklog, Priority: "P2", EstimateMinutes: 20, RemainingMinutes: 20}
		if _, err := s.SaveTask(root, task); err != nil {
			b.Fatal(err)
		}
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.ListTasks(root, ""); err != nil {
			b.Fatal(err)
		}
	}
}

func TestWithLockSerializesConcurrentWriters(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "locks", "state.lock")
	output := filepath.Join(t.TempDir(), "events")
	const workers = 12
	var group sync.WaitGroup
	group.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer group.Done()
			if err := WithLock(lock, func() error {
				file, err := os.OpenFile(output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
				if err != nil {
					return err
				}
				defer file.Close()
				_, err = file.WriteString("event\n")
				return err
			}); err != nil {
				t.Errorf("lock: %v", err)
			}
		}()
	}
	group.Wait()
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "event\n"); got != workers {
		t.Fatalf("events = %d, want %d", got, workers)
	}
}

func TestFutureSchemaIsRejected(t *testing.T) {
	s := Store{GlobalRoot: filepath.Join(t.TempDir(), "global")}
	if err := s.EnsureGlobal(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.RegistryPath(), []byte("schema_version: 99\nnext_task_id: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadRegistry(); err == nil {
		t.Fatal("expected future schema rejection")
	}
}
