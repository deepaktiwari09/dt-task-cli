package worktree

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode"

	"github.com/deepaktiwari09/dt-task-cli/internal/model"
)

const DirectoryName = ".worktrees"

type Entry struct {
	Slug     string `json:"slug"`
	Path     string `json:"path"`
	Branch   string `json:"branch"`
	Commit   string `json:"commit"`
	Dirty    bool   `json:"dirty"`
	Locked   bool   `json:"locked"`
	Prunable bool   `json:"prunable"`
}

type CreateOptions struct {
	ProjectRoot  string
	Slug         string
	Base         string
	Branch       string
	BranchPrefix string
	SetupCommand string
	RunSetup     bool
	Stdout       io.Writer
	Stderr       io.Writer
}

type CreateResult struct {
	Entry
	SetupConfigured bool `json:"setup_configured"`
	SetupRan        bool `json:"setup_ran"`
}

func Root(projectRoot string) string {
	return filepath.Join(projectRoot, DirectoryName)
}

func CurrentBranch(projectRoot string) (string, error) {
	projectRoot, err := absoluteProjectRoot(projectRoot)
	if err != nil {
		return "", err
	}
	if err := ensureGitRoot(projectRoot); err != nil {
		return "", err
	}
	data, err := runGit(projectRoot, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	branch := strings.TrimSpace(string(data))
	if branch == "" {
		return "", fmt.Errorf("Git checkout is detached; configure worktree_default_branch explicitly")
	}
	return branch, nil
}

func Create(opts CreateOptions) (CreateResult, error) {
	projectRoot, err := absoluteProjectRoot(opts.ProjectRoot)
	if err != nil {
		return CreateResult{}, err
	}
	if err := ensureGitRoot(projectRoot); err != nil {
		return CreateResult{}, err
	}
	slug, err := normalizeSlug(opts.Slug)
	if err != nil {
		return CreateResult{}, err
	}
	base := strings.TrimSpace(opts.Base)
	if base == "" {
		return CreateResult{}, fmt.Errorf("worktree base branch is required")
	}
	if _, err := runGit(projectRoot, "rev-parse", "--verify", base+"^{commit}"); err != nil {
		return CreateResult{}, fmt.Errorf("base %q is not available locally; fetch it or pass --base with an existing branch or commit: %w", base, err)
	}

	branch := strings.TrimSpace(opts.Branch)
	if branch == "" {
		prefix := strings.TrimSuffix(strings.TrimSpace(opts.BranchPrefix), "/")
		if prefix == "" {
			return CreateResult{}, fmt.Errorf("worktree branch prefix is required")
		}
		branch = prefix + "/" + slug
	}
	if _, err := runGit(projectRoot, "check-ref-format", "--branch", branch); err != nil {
		return CreateResult{}, fmt.Errorf("invalid worktree branch %q: %w", branch, err)
	}

	root := Root(projectRoot)
	if err := ensureManagedRoot(root); err != nil {
		return CreateResult{}, err
	}
	path := filepath.Join(root, slug)
	if _, err := os.Lstat(path); err == nil {
		return CreateResult{}, fmt.Errorf("worktree path already exists: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return CreateResult{}, fmt.Errorf("inspect worktree path: %w", err)
	}
	if entries, err := List(projectRoot); err != nil {
		return CreateResult{}, err
	} else {
		for _, entry := range entries {
			if entry.Branch == branch {
				return CreateResult{}, fmt.Errorf("branch %q is already used by worktree %s", branch, entry.Path)
			}
		}
	}

	if _, err := runGit(projectRoot, "worktree", "add", "-b", branch, path, base); err != nil {
		return CreateResult{}, err
	}

	result := CreateResult{Entry: Entry{Slug: slug, Path: path, Branch: branch}, SetupConfigured: strings.TrimSpace(opts.SetupCommand) != ""}
	if result.SetupConfigured && opts.RunSetup {
		result.SetupRan = true
		if err := runSetup(path, opts.SetupCommand, slug, branch, opts.Stdout, opts.Stderr); err != nil {
			return result, fmt.Errorf("worktree created at %s, but setup failed: %w; rerun setup manually or remove it after reviewing changes", path, err)
		}
	}
	entries, err := List(projectRoot)
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		if entry.Path == path {
			result.Entry = entry
			break
		}
	}
	return result, nil
}

func List(projectRoot string) ([]Entry, error) {
	projectRoot, err := absoluteProjectRoot(projectRoot)
	if err != nil {
		return nil, err
	}
	if err := ensureGitRoot(projectRoot); err != nil {
		return nil, err
	}
	root := Root(projectRoot)
	if err := validateManagedRoot(root); err != nil {
		return nil, err
	}
	data, err := runGit(projectRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	entries, err := parsePorcelain(string(data))
	if err != nil {
		return nil, err
	}
	filtered := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if !within(root, entry.Path) {
			continue
		}
		entry.Path, err = filepath.Abs(entry.Path)
		if err != nil {
			return nil, err
		}
		entry.Slug = filepath.Base(entry.Path)
		entry.Dirty, err = isDirty(entry.Path)
		if err != nil {
			if entry.Prunable || errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		filtered = append(filtered, entry)
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].Slug < filtered[j].Slug })
	return filtered, nil
}

func Path(projectRoot, slug string) (string, error) {
	slug, err := normalizeSlug(slug)
	if err != nil {
		return "", err
	}
	entries, err := List(projectRoot)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.Slug == slug {
			return entry.Path, nil
		}
	}
	return "", fmt.Errorf("worktree %q not found", slug)
}

func Remove(projectRoot, slug string, force bool) (Entry, error) {
	slug, err := normalizeSlug(slug)
	if err != nil {
		return Entry{}, err
	}
	entries, err := List(projectRoot)
	if err != nil {
		return Entry{}, err
	}
	var target Entry
	found := false
	for _, entry := range entries {
		if entry.Slug == slug {
			target = entry
			found = true
			break
		}
	}
	if !found {
		return Entry{}, fmt.Errorf("worktree %q not found", slug)
	}
	if target.Dirty && !force {
		return Entry{}, fmt.Errorf("worktree %q has uncommitted or untracked changes; review it or pass --force", slug)
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, target.Path)
	if _, err := runGit(projectRoot, args...); err != nil {
		return Entry{}, err
	}
	return target, nil
}

func normalizeSlug(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("worktree slug is required")
	}
	hasAlphaNumeric := false
	for _, r := range trimmed {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			hasAlphaNumeric = true
			break
		}
	}
	if !hasAlphaNumeric {
		return "", fmt.Errorf("worktree slug %q must contain a letter or number", raw)
	}
	return model.SafeSlug(trimmed), nil
}

func absoluteProjectRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("project root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func ensureGitRoot(root string) error {
	actual, err := runGit(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("project is not a Git repository: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(strings.TrimSpace(string(actual)))
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	if resolved != root {
		return fmt.Errorf("project root %s is not the Git checkout root %s", root, resolved)
	}
	return nil
}

func ensureManagedRoot(root string) error {
	if info, err := os.Lstat(root); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink worktree root: %s", root)
		}
		if !info.IsDir() {
			return fmt.Errorf("worktree root is not a directory: %s", root)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.MkdirAll(root, 0o755)
}

func validateManagedRoot(root string) error {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink worktree root: %s", root)
	}
	if !info.IsDir() {
		return fmt.Errorf("worktree root is not a directory: %s", root)
	}
	return nil
}

func within(root, candidate string) bool {
	root, rootErr := filepath.Abs(root)
	candidate, candidateErr := filepath.Abs(candidate)
	if rootErr != nil || candidateErr != nil {
		return false
	}
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "."
}

func isDirty(path string) (bool, error) {
	data, err := runGit(path, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(data)) != "", nil
}

func runGit(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	data, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(data))
		if message == "" {
			return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), message)
	}
	return data, nil
}

func runSetup(path, setup, slug, branch string, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", setup)
	} else {
		cmd = exec.Command("sh", "-lc", setup)
	}
	cmd.Dir = path
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = append(os.Environ(),
		"DT_TASK_WORKTREE_PATH="+path,
		"DT_TASK_WORKTREE_SLUG="+slug,
		"DT_TASK_WORKTREE_BRANCH="+branch,
	)
	return cmd.Run()
}

func parsePorcelain(input string) ([]Entry, error) {
	var entries []Entry
	var current *Entry
	flush := func() {
		if current != nil {
			entries = append(entries, *current)
		}
	}
	scanner := bufio.NewScanner(strings.NewReader(input))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			current = &Entry{Path: strings.TrimPrefix(line, "worktree ")}
		case current == nil:
			continue
		case strings.HasPrefix(line, "HEAD "):
			current.Commit = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			current.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		case line == "locked" || strings.HasPrefix(line, "locked "):
			current.Locked = true
		case line == "prunable" || strings.HasPrefix(line, "prunable "):
			current.Prunable = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	flush()
	return entries, nil
}
