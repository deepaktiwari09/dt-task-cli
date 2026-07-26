package worktree

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateListPathRemove(t *testing.T) {
	root := initGitRepo(t)
	result, err := Create(CreateOptions{ProjectRoot: root, Slug: "Fix Login", Base: "main", BranchPrefix: "deepak/codex"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Slug != "fix-login" || result.Branch != "deepak/codex/fix-login" {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(result.Path); err != nil {
		t.Fatalf("worktree path missing: %v", err)
	}
	entries, err := List(root)
	if err != nil || len(entries) != 1 || entries[0].Dirty {
		t.Fatalf("entries = %#v, err = %v", entries, err)
	}
	path, err := Path(root, "fix-login")
	if err != nil || path != result.Path {
		t.Fatalf("path = %q, err = %v", path, err)
	}
	removed, err := Remove(root, "fix-login", false)
	if err != nil || removed.Path != result.Path {
		t.Fatalf("removed = %#v, err = %v", removed, err)
	}
	if _, err := os.Stat(result.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree still exists: %v", err)
	}
	if branch := git(t, root, "branch", "--list", "deepak/codex/fix-login"); !strings.Contains(branch, "deepak/codex/fix-login") {
		t.Fatalf("worktree removal unexpectedly deleted branch: %q", branch)
	}
}

func TestCreateRunsConfiguredSetup(t *testing.T) {
	root := initGitRepo(t)
	result, err := Create(CreateOptions{
		ProjectRoot:  root,
		Slug:         "setup",
		Base:         "main",
		BranchPrefix: "deepak/codex",
		SetupCommand: "printf '%s' \"$DT_TASK_WORKTREE_SLUG\" > setup.txt",
		RunSetup:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.SetupConfigured || !result.SetupRan {
		t.Fatalf("setup state = %#v", result)
	}
	data, err := os.ReadFile(filepath.Join(result.Path, "setup.txt"))
	if err != nil || string(data) != "setup" {
		t.Fatalf("setup output = %q, err = %v", data, err)
	}
	if _, err := Remove(root, "setup", true); err != nil {
		t.Fatal(err)
	}
}

func TestSetupFailurePreservesWorktree(t *testing.T) {
	root := initGitRepo(t)
	result, err := Create(CreateOptions{
		ProjectRoot:  root,
		Slug:         "broken-setup",
		Base:         "main",
		BranchPrefix: "deepak/codex",
		SetupCommand: "exit 7",
		RunSetup:     true,
	})
	if err == nil || !strings.Contains(err.Error(), result.Path) {
		t.Fatalf("err = %v, result = %#v", err, result)
	}
	if _, statErr := os.Stat(result.Path); statErr != nil {
		t.Fatalf("failed setup removed worktree: %v", statErr)
	}
	if _, err := Remove(root, "broken-setup", false); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveRefusesDirtyWorktreeUnlessForced(t *testing.T) {
	root := initGitRepo(t)
	result, err := Create(CreateOptions{ProjectRoot: root, Slug: "dirty", Base: "main", BranchPrefix: "deepak/codex"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(result.Path, "untracked.txt"), []byte("work"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Remove(root, "dirty", false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("dirty removal error = %v", err)
	}
	if _, err := Remove(root, "dirty", true); err != nil {
		t.Fatal(err)
	}
}

func TestCreateRejectsMissingBaseAndInvalidSlug(t *testing.T) {
	root := initGitRepo(t)
	if _, err := Create(CreateOptions{ProjectRoot: root, Slug: "valid", Base: "missing", BranchPrefix: "deepak/codex"}); err == nil || !strings.Contains(err.Error(), "not available locally") {
		t.Fatalf("missing base error = %v", err)
	}
	if _, err := Create(CreateOptions{ProjectRoot: root, Slug: "!!!", Base: "main", BranchPrefix: "deepak/codex"}); err == nil || !strings.Contains(err.Error(), "letter or number") {
		t.Fatalf("invalid slug error = %v", err)
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "checkout", "-b", "main")
	git(t, root, "config", "user.email", "dt-task-test@example.invalid")
	git(t, root, "config", "user.name", "dt-task test")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "README.md")
	git(t, root, "commit", "-m", "initial")
	return root
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, data)
	}
	return string(data)
}
