package store

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/deepaktiwari09/dt-task-cli/internal/model"
	"gopkg.in/yaml.v3"
)

const (
	ProjectDir = ".task"
	TasksDir   = "tasks"
	ArchiveDir = "archive"
	TrashDir   = "trash"
)

type Store struct{ GlobalRoot string }

func New() (Store, error) {
	if override := strings.TrimSpace(os.Getenv("DT_TASK_HOME")); override != "" {
		root, err := filepath.Abs(override)
		if err != nil {
			return Store{}, err
		}
		return Store{GlobalRoot: root}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Store{}, fmt.Errorf("find home directory: %w", err)
	}
	return Store{GlobalRoot: filepath.Join(home, ".dt-task")}, nil
}

func (s Store) RegistryPath() string     { return filepath.Join(s.GlobalRoot, "registry.yaml") }
func (s Store) GlobalConfigPath() string { return filepath.Join(s.GlobalRoot, "config.yaml") }
func (s Store) GlobalStatePath() string  { return filepath.Join(s.GlobalRoot, "state.yaml") }
func (s Store) JournalPath(date string) string {
	return filepath.Join(s.GlobalRoot, "days", date+".md")
}
func (s Store) BackupRoot() string { return filepath.Join(s.GlobalRoot, "backups") }

func (s Store) EnsureGlobal() error {
	if info, err := os.Lstat(s.GlobalRoot); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink global state directory %s", s.GlobalRoot)
	}
	for _, dir := range []string{s.GlobalRoot, filepath.Join(s.GlobalRoot, "days"), filepath.Join(s.GlobalRoot, "locks"), s.BackupRoot()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create global directory %s: %w", dir, err)
		}
		if err := os.Chmod(dir, 0o700); err != nil && runtime.GOOS != "windows" {
			return fmt.Errorf("secure global directory %s: %w", dir, err)
		}
	}
	if _, err := os.Stat(s.RegistryPath()); errors.Is(err, os.ErrNotExist) {
		if err := s.SaveRegistry(model.NewRegistry()); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if _, err := os.Stat(s.GlobalConfigPath()); errors.Is(err, os.ErrNotExist) {
		if err := s.SaveGlobalConfig(model.NewGlobalConfig()); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if _, err := os.Stat(s.GlobalStatePath()); errors.Is(err, os.ErrNotExist) {
		if err := s.SaveGlobalState(model.NewGlobalState()); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		for _, path := range []string{s.RegistryPath(), s.GlobalConfigPath(), s.GlobalStatePath()} {
			if err := os.Chmod(path, 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s Store) LoadRegistry() (model.Registry, error) {
	var v model.Registry
	if err := readYAML(s.RegistryPath(), &v); err != nil {
		return v, err
	}
	if v.SchemaVersion == 0 {
		v.SchemaVersion = model.SchemaVersion
	}
	if v.SchemaVersion > model.SchemaVersion {
		return v, fmt.Errorf("unsupported registry schema_version %d", v.SchemaVersion)
	}
	if v.NextTaskID == 0 {
		v.NextTaskID = 1
	}
	return v, nil
}

func (s Store) SaveRegistry(v model.Registry) error {
	if v.SchemaVersion == 0 {
		v.SchemaVersion = model.SchemaVersion
	}
	if v.NextTaskID == 0 {
		v.NextTaskID = 1
	}
	seenAliases := map[string]bool{}
	seenRoots := map[string]bool{}
	for _, project := range v.Projects {
		if err := model.ValidateAlias(project.Alias); err != nil {
			return err
		}
		if project.Root == "" {
			return fmt.Errorf("project %s root is required", project.Alias)
		}
		if seenAliases[project.Alias] {
			return fmt.Errorf("duplicate project alias %q", project.Alias)
		}
		seenAliases[project.Alias] = true
		root, err := filepath.Abs(project.Root)
		if err != nil {
			return err
		}
		if seenRoots[root] {
			return fmt.Errorf("duplicate project root %q", project.Root)
		}
		seenRoots[root] = true
	}
	return writeYAML(s.RegistryPath(), v, 0o600)
}

func (s Store) LoadGlobalConfig() (model.GlobalConfig, error) {
	var v model.GlobalConfig
	if err := readYAML(s.GlobalConfigPath(), &v); err != nil {
		return v, err
	}
	if v.SchemaVersion == 0 {
		v.SchemaVersion = model.SchemaVersion
	}
	if v.SchemaVersion > model.SchemaVersion {
		return v, fmt.Errorf("unsupported config schema_version %d", v.SchemaVersion)
	}
	if v.DailyCapacityMinutes <= 0 {
		v.DailyCapacityMinutes = 360
	}
	if v.Timezone != "" && !strings.EqualFold(v.Timezone, "Local") {
		if _, err := time.LoadLocation(v.Timezone); err != nil {
			return v, fmt.Errorf("invalid timezone %q", v.Timezone)
		}
	}
	return v, nil
}

func (s Store) SaveGlobalConfig(v model.GlobalConfig) error {
	if v.SchemaVersion == 0 {
		v.SchemaVersion = model.SchemaVersion
	}
	if v.DailyCapacityMinutes <= 0 {
		return fmt.Errorf("daily capacity must be positive")
	}
	if v.Timezone != "" && !strings.EqualFold(v.Timezone, "Local") {
		if _, err := time.LoadLocation(v.Timezone); err != nil {
			return fmt.Errorf("invalid timezone %q", v.Timezone)
		}
	}
	return writeYAML(s.GlobalConfigPath(), v, 0o600)
}

func (s Store) LoadGlobalState() (model.GlobalState, error) {
	var v model.GlobalState
	if err := readYAML(s.GlobalStatePath(), &v); err != nil {
		return v, err
	}
	if v.SchemaVersion == 0 {
		v.SchemaVersion = model.SchemaVersion
	}
	if v.SchemaVersion > model.SchemaVersion {
		return v, fmt.Errorf("unsupported state schema_version %d", v.SchemaVersion)
	}
	if v.ActiveTimer != nil {
		if v.ActiveTimer.Session.TaskID == 0 || v.ActiveTimer.Session.ID == "" || v.ActiveTimer.Session.StartedAt.IsZero() {
			return v, fmt.Errorf("active timer session is malformed")
		}
		if v.ActiveTimer.Session.Minutes < 0 {
			return v, fmt.Errorf("active timer minutes cannot be negative")
		}
	}
	return v, nil
}

func (s Store) SaveGlobalState(v model.GlobalState) error {
	if v.SchemaVersion == 0 {
		v.SchemaVersion = model.SchemaVersion
	}
	if v.ActiveTimer != nil {
		if v.ActiveTimer.Session.TaskID == 0 || v.ActiveTimer.Session.ID == "" || v.ActiveTimer.Session.StartedAt.IsZero() {
			return fmt.Errorf("active timer session is malformed")
		}
		if v.ActiveTimer.Session.Minutes < 0 {
			return fmt.Errorf("active timer minutes cannot be negative")
		}
	}
	return writeYAML(s.GlobalStatePath(), v, 0o600)
}

func ProjectTaskRoot(root string) string    { return filepath.Join(root, ProjectDir, TasksDir) }
func ProjectArchiveRoot(root string) string { return filepath.Join(root, ProjectDir, ArchiveDir) }
func ProjectTrashRoot(root string) string   { return filepath.Join(root, ProjectDir, TrashDir) }

func rejectProjectSymlinks(root string) error {
	for _, path := range []string{filepath.Join(root, ProjectDir), ProjectTaskRoot(root), ProjectArchiveRoot(root), ProjectTrashRoot(root), filepath.Join(root, ProjectDir, "config.yaml")} {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink in project task store: %s", path)
		}
	}
	return nil
}

func (s Store) EnsureProject(root, alias string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if err := rejectProjectSymlinks(root); err != nil {
		return err
	}
	for _, dir := range []string{ProjectTaskRoot(root), ProjectArchiveRoot(root), ProjectTrashRoot(root)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		if err := os.Chmod(dir, 0o700); err != nil && runtime.GOOS != "windows" {
			return err
		}
	}
	configPath := filepath.Join(root, ProjectDir, "config.yaml")
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		if err := writeYAML(configPath, model.NewProjectConfig(alias), 0o600); err != nil {
			return err
		}
	} else if err == nil && runtime.GOOS != "windows" {
		if err := os.Chmod(configPath, 0o600); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return updateGitignore(root)
}

func (s Store) LoadProjectConfig(root string) (model.ProjectConfig, error) {
	var v model.ProjectConfig
	if err := readYAML(filepath.Join(root, ProjectDir, "config.yaml"), &v); err != nil {
		return v, err
	}
	if v.SchemaVersion == 0 {
		v.SchemaVersion = model.SchemaVersion
	}
	if v.SchemaVersion > model.SchemaVersion {
		return v, fmt.Errorf("unsupported project config schema_version %d", v.SchemaVersion)
	}
	if err := model.ValidateAlias(v.Alias); err != nil {
		return v, err
	}
	if v.DefaultPriority == "" {
		v.DefaultPriority = "P2"
	}
	if err := model.ValidatePriority(v.DefaultPriority); err != nil {
		return v, err
	}
	return v, nil
}

func (s Store) SaveProjectConfig(root string, v model.ProjectConfig) error {
	if v.SchemaVersion == 0 {
		v.SchemaVersion = model.SchemaVersion
	}
	if v.Alias == "" {
		return fmt.Errorf("project config alias is required")
	}
	if err := model.ValidateAlias(v.Alias); err != nil {
		return err
	}
	if v.DefaultPriority == "" {
		v.DefaultPriority = "P2"
	}
	if err := model.ValidatePriority(v.DefaultPriority); err != nil {
		return err
	}
	return writeYAML(filepath.Join(root, ProjectDir, "config.yaml"), v, 0o600)
}

func (s Store) SaveTask(root string, task model.Task) (string, error) {
	if err := rejectProjectSymlinks(root); err != nil {
		return "", err
	}
	if err := task.Validate(); err != nil {
		return "", err
	}
	if task.SchemaVersion == 0 {
		task.SchemaVersion = model.SchemaVersion
	}
	task.Slug = model.SafeSlug(task.Title)
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now()
	}
	task.UpdatedAt = time.Now()
	if task.RemainingMinutes == 0 && task.Status != model.StatusDone {
		task.RemainingMinutes = task.EstimateMinutes
	}
	name := task.Filename()
	base := ProjectTaskRoot(root)
	// Metadata edits preserve the task's current area. Explicit restore/status
	// operations clear Path before calling SaveTask to move a task back to the
	// active area.
	if task.Path != "" {
		switch filepath.Dir(task.Path) {
		case ProjectArchiveRoot(root):
			base = ProjectArchiveRoot(root)
		case ProjectTrashRoot(root):
			base = ProjectTrashRoot(root)
		}
	}
	path := filepath.Join(base, name)
	if task.Path != "" && task.Path != path {
		if err := ensureWithin(filepath.Join(root, ProjectDir), task.Path); err != nil {
			return "", err
		}
	}
	if task.Path != path {
		if _, err := os.Stat(path); err == nil {
			return "", fmt.Errorf("destination task already exists: %s", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	if err := writeMarkdown(path, task, 0o600); err != nil {
		return "", err
	}
	if task.Path != "" && task.Path != path {
		if err := os.Remove(task.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	task.Path = path
	return path, nil
}

func (s Store) MoveTask(root string, task model.Task, area string) (string, error) {
	if err := rejectProjectSymlinks(root); err != nil {
		return "", err
	}
	if task.Path == "" {
		return "", fmt.Errorf("task has no path")
	}
	if err := ensureWithin(filepath.Join(root, ProjectDir), task.Path); err != nil {
		return "", err
	}
	var dir string
	switch area {
	case ArchiveDir:
		dir = ProjectArchiveRoot(root)
	case TrashDir:
		dir = ProjectTrashRoot(root)
	default:
		return "", fmt.Errorf("invalid task area %q", area)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := filepath.Base(task.Path)
	dest := filepath.Join(dir, name)
	if dest != task.Path {
		if _, err := os.Stat(dest); err == nil {
			return "", fmt.Errorf("destination task already exists: %s", dest)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	if err := os.Rename(task.Path, dest); err != nil {
		return "", err
	}
	// Persist metadata changes (deleted_at/archived_at) after the move while
	// keeping the body intact. A move alone would leave stale frontmatter.
	if err := writeMarkdown(dest, task, 0o600); err != nil {
		if dest != task.Path {
			_ = os.Rename(dest, task.Path)
		}
		return "", err
	}
	task.Path = dest
	return dest, nil
}

func (s Store) PurgeTask(root string, task model.Task) error {
	if err := rejectProjectSymlinks(root); err != nil {
		return err
	}
	if task.Path == "" || filepath.Dir(task.Path) != ProjectTrashRoot(root) {
		return fmt.Errorf("task %d is not in trash", task.ID)
	}
	if err := ensureWithin(filepath.Join(root, ProjectDir, TrashDir), task.Path); err != nil {
		return err
	}
	return os.Remove(task.Path)
}

func (s Store) ListTasks(root, area string) ([]model.Task, error) {
	if err := rejectProjectSymlinks(root); err != nil {
		return nil, err
	}
	dir := ProjectTaskRoot(root)
	switch area {
	case ArchiveDir:
		dir = ProjectArchiveRoot(root)
	case TrashDir:
		dir = ProjectTrashRoot(root)
	case "":
	default:
		return nil, fmt.Errorf("invalid task area %q", area)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	if len(paths) == 0 {
		return nil, nil
	}
	tasks := make([]model.Task, len(paths))
	jobs := make(chan int)
	workers := runtime.GOMAXPROCS(0)
	if workers > 4 {
		workers = 4
	}
	if workers > len(paths) {
		workers = len(paths)
	}
	var group sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range jobs {
				task, loadErr := s.LoadTask(paths[index])
				if loadErr != nil {
					if errors.Is(loadErr, os.ErrNotExist) {
						continue
					}
					errMu.Lock()
					if firstErr == nil {
						firstErr = loadErr
					}
					errMu.Unlock()
					continue
				}
				tasks[index] = task
			}
		}()
	}
	for index := range paths {
		jobs <- index
	}
	close(jobs)
	group.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return tasks, nil
}

func (s Store) LoadTask(path string) (model.Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Task{}, err
	}
	var t model.Task
	body, err := parseMarkdown(data, &t)
	if err != nil {
		return t, fmt.Errorf("parse task %s: %w", path, err)
	}
	t.Body, t.Path = body, path
	if t.SchemaVersion == 0 {
		t.SchemaVersion = model.SchemaVersion
	}
	if err := t.Validate(); err != nil {
		return t, fmt.Errorf("invalid task %s: %w", path, err)
	}
	return t, nil
}

func (s Store) FindTask(reg model.Registry, id uint64) (model.Project, model.Task, error) {
	for _, p := range reg.Projects {
		for _, area := range []string{"", ArchiveDir, TrashDir} {
			tasks, err := s.ListTasks(p.Root, area)
			if err != nil {
				return p, model.Task{}, err
			}
			for _, t := range tasks {
				if t.ID == id {
					return p, t, nil
				}
			}
		}
	}
	return model.Project{}, model.Task{}, fmt.Errorf("task %d not found", id)
}

func (s Store) SaveJournal(j model.DayJournal) error {
	if j.SchemaVersion == 0 {
		j.SchemaVersion = model.SchemaVersion
	}
	if j.Date == "" {
		return fmt.Errorf("journal date is required")
	}
	if _, err := time.Parse("2006-01-02", j.Date); err != nil {
		return fmt.Errorf("journal date must be YYYY-MM-DD: %w", err)
	}
	return writeMarkdown(s.JournalPath(j.Date), j, 0o600)
}

func (s Store) LoadJournal(date string) (model.DayJournal, error) {
	path := s.JournalPath(date)
	var j model.DayJournal
	data, err := os.ReadFile(path)
	if err != nil {
		return j, err
	}
	body, err := parseMarkdown(data, &j)
	if err != nil {
		return j, err
	}
	j.Body = body
	if j.SchemaVersion == 0 {
		j.SchemaVersion = model.SchemaVersion
	}
	if j.SchemaVersion > model.SchemaVersion {
		return j, fmt.Errorf("unsupported journal schema_version %d", j.SchemaVersion)
	}
	if _, err := time.Parse("2006-01-02", j.Date); err != nil {
		return j, fmt.Errorf("journal date must be YYYY-MM-DD: %w", err)
	}
	return j, nil
}

func (s Store) BackupPath(prefix string) (string, error) {
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	dir := filepath.Join(s.BackupRoot(), prefix, stamp)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func (s Store) BackupProject(root, prefix string) (string, error) {
	dir, err := s.BackupPath(prefix)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(dir, "project-task-store")
	source := filepath.Join(root, ProjectDir)
	if _, statErr := os.Stat(source); errors.Is(statErr, os.ErrNotExist) {
		if err := os.MkdirAll(dest, 0o700); err != nil {
			return "", err
		}
	} else if statErr != nil {
		return "", statErr
	} else if err := copyTree(source, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func (s Store) ListJournals() ([]model.DayJournal, error) {
	entries, err := os.ReadDir(filepath.Join(s.GlobalRoot, "days"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var journals []model.DayJournal
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		j, err := s.LoadJournal(strings.TrimSuffix(entry.Name(), ".md"))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		journals = append(journals, j)
	}
	sort.Slice(journals, func(i, j int) bool { return journals[i].Date < journals[j].Date })
	return journals, nil
}

func WithLock(path string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	for i := 0; i < 200; i++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = f.WriteString(fmt.Sprintf("pid=%d\n", os.Getpid()))
			_ = f.Close()
			defer os.Remove(path)
			return fn()
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		info, statErr := os.Stat(path)
		if statErr == nil && time.Since(info.ModTime()) > 5*time.Minute {
			_ = os.Remove(path)
			continue
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out acquiring lock %s", path)
}

func AtomicInstallDir(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	return os.Rename(src, dest)
}

func readYAML(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func writeYAML(path string, value any, mode fs.FileMode) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return atomicWrite(path, data, mode)
}

func writeMarkdown(path string, value any, mode fs.FileMode) error {
	body := ""
	switch v := value.(type) {
	case model.Task:
		body = v.Body
	case model.DayJournal:
		body = v.Body
	}
	meta, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	data := bytes.NewBufferString("---\n")
	data.Write(meta)
	data.WriteString("---\n\n")
	data.WriteString(strings.TrimSpace(body))
	data.WriteByte('\n')
	return atomicWrite(path, data.Bytes(), mode)
}

func parseMarkdown(data []byte, out any) (string, error) {
	text := string(data)
	if !strings.HasPrefix(text, "---\n") {
		return "", fmt.Errorf("missing YAML frontmatter")
	}
	rest := text[4:]
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		return "", fmt.Errorf("unterminated YAML frontmatter")
	}
	if err := yaml.Unmarshal([]byte(rest[:idx]), out); err != nil {
		return "", err
	}
	return strings.TrimSpace(rest[idx+6:]), nil
}

func atomicWrite(path string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".dt-task-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err == nil {
		return nil
	}
	// Windows does not replace an existing file with Rename. Move the old
	// file aside, install the complete temporary file, and roll back on error.
	backup := fmt.Sprintf("%s.bak-%d", path, time.Now().UnixNano())
	if err := os.Rename(path, backup); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Rename(backup, path)
		return err
	}
	_ = os.Remove(backup)
	return nil
}

func copyTree(src, dest string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink in backup: %s", path)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

func ensureWithin(root, target string) error {
	r, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	t, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(r, t)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("path %s escapes %s", target, root)
	}
	return nil
}

func updateGitignore(root string) error {
	path := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		data = nil
	} else if err != nil {
		return err
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	filtered := make([]string, 0, len(lines)+1)
	canonicalSeen := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "/.task/" || trimmed == ".task/" || trimmed == "/.task" || trimmed == ".task" {
			if canonicalSeen {
				continue
			}
			canonicalSeen = true
			line = "/.task/"
		}
		filtered = append(filtered, line)
	}
	lines = filtered
	if !canonicalSeen {
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "/.task/")
	}
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return atomicWrite(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
