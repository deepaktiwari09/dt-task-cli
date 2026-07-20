package cli

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/deepaktiwari09/dt-task-cli/internal/logging"
	"github.com/deepaktiwari09/dt-task-cli/internal/model"
	"github.com/deepaktiwari09/dt-task-cli/internal/skillbundle"
	"github.com/deepaktiwari09/dt-task-cli/internal/store"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const version = "0.1.0"

type App struct {
	Store       store.Store
	ProjectFlag string
	JSON        bool
	NoColor     bool
	Quiet       bool
	Out         io.Writer
	Err         io.Writer
	Log         *slog.Logger
	outputSent  bool
}

type UsageError struct{ Err error }

func (e UsageError) Error() string { return e.Err.Error() }
func (e UsageError) Unwrap() error { return e.Err }
func Usage(err error) error        { return UsageError{Err: err} }

func ExitCode(err error) int {
	var usage UsageError
	if errors.As(err, &usage) {
		return 2
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"unknown command", "unknown flag", "requires ", "accepts ", "invalid argument", "invalid task id", "task id must", " is required", "must be ", "invalid status", "invalid priority", "invalid status transition", "dependency cycle", "cannot be combined", "cannot be negative"} {
		if strings.Contains(message, marker) {
			return 2
		}
	}
	return 1
}

func (a *App) WriteError(err error) {
	// Commands such as doctor and skill --status intentionally return a
	// non-zero status after emitting a complete result. Avoid appending a
	// second, non-contract JSON document (or duplicate human diagnostics).
	if a.outputSent {
		return
	}
	code := ExitCode(err)
	if a.JSON {
		_ = json.NewEncoder(a.Err).Encode(map[string]any{"version": 1, "error": map[string]any{"code": code, "message": err.Error()}})
		return
	}
	fmt.Fprintln(a.Err, "error:", err)
}

func New() (*cobra.Command, *App, error) {
	s, err := store.New()
	if err != nil {
		return nil, nil, err
	}
	a := &App{Store: s, Out: os.Stdout, Err: os.Stderr, NoColor: os.Getenv("NO_COLOR") != "", Log: logging.New(os.Stderr)}
	root := &cobra.Command{Use: "dt-task", Short: "Local PRD and daily work manager", SilenceUsage: true, SilenceErrors: true}
	root.PersistentPreRun = func(cmd *cobra.Command, args []string) { a.outputSent = false }
	root.PersistentFlags().StringVar(&a.ProjectFlag, "project", "", "project alias")
	root.PersistentFlags().BoolVar(&a.JSON, "json", false, "emit JSON")
	root.PersistentFlags().BoolVar(&a.NoColor, "no-color", false, "disable color")
	root.PersistentFlags().BoolVar(&a.Quiet, "quiet", false, "suppress informational output")
	root.AddCommand(a.initCommand(), a.projectCommand(), a.captureCommand(), a.taskCommand(), a.dayCommand(), a.analyticsCommand(), a.doctorCommand(), a.configCommand(), a.skillCommand(), a.completionCommand(), a.versionCommand())
	return root, a, nil
}

func (a *App) ensure() error {
	if err := a.Store.EnsureGlobal(); err != nil {
		return err
	}
	config, err := a.Store.LoadGlobalConfig()
	if err != nil {
		return err
	}
	if config.NoColor {
		a.NoColor = true
	}
	return nil
}

func isTTY() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (a *App) configuredNow() (time.Time, error) {
	cfg, err := a.Store.LoadGlobalConfig()
	if err != nil {
		return time.Time{}, err
	}
	if cfg.Timezone == "" || strings.EqualFold(cfg.Timezone, "Local") {
		return time.Now(), nil
	}
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid configured timezone %q: %w", cfg.Timezone, err)
	}
	return time.Now().In(loc), nil
}

func (a *App) output(data any, human string) error {
	if a.Quiet {
		return nil
	}
	if a.JSON {
		err := json.NewEncoder(a.Out).Encode(map[string]any{"version": 1, "data": data})
		if err == nil {
			a.outputSent = true
		}
		return err
	}
	if human != "" {
		_, err := fmt.Fprintln(a.Out, human)
		if err == nil {
			a.outputSent = true
		}
		return err
	}
	return nil
}

func (a *App) initCommand() *cobra.Command {
	var alias string
	cmd := &cobra.Command{Use: "init [directory]", Short: "Initialize a project task store", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		started := time.Now()
		if err := a.ensure(); err != nil {
			return err
		}
		root, err := os.Getwd()
		if err != nil {
			return err
		}
		if len(args) == 1 {
			root, err = filepath.Abs(args[0])
			if err != nil {
				return err
			}
		}
		root, err = filepath.EvalSymlinks(root)
		if err != nil {
			return err
		}
		if alias == "" {
			alias = model.SafeSlug(filepath.Base(root))
		}
		if err := model.ValidateAlias(alias); err != nil {
			return Usage(err)
		}
		var project model.Project
		err = store.WithLock(filepath.Join(a.Store.GlobalRoot, "locks", "registry.lock"), func() error {
			reg, err := a.Store.LoadRegistry()
			if err != nil {
				return err
			}
			created := false
			if p, ok := reg.ProjectForRoot(root); ok {
				project = p
				alias = p.Alias
			} else {
				if _, ok := reg.FindProject(alias); ok {
					return fmt.Errorf("project alias %q already exists", alias)
				}
				project = model.Project{SchemaVersion: model.SchemaVersion, Alias: alias, Root: root, RegisteredAt: time.Now()}
				if err := reg.AddProject(project); err != nil {
					return err
				}
				created = true
			}
			if err := a.Store.EnsureProject(root, alias); err != nil {
				return err
			}
			if created {
				if err := a.Store.SaveRegistry(reg); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		a.Log.Info("project initialized", "operation", "init", "alias", project.Alias, "duration_ms", time.Since(started).Milliseconds(), "result", "success")
		return a.output(map[string]any{"alias": project.Alias, "root": project.Root, "initialized": true}, fmt.Sprintf("initialized %s (%s)", project.Alias, project.Root))
	}}
	cmd.Flags().StringVar(&alias, "alias", "", "project alias")
	return cmd
}

func (a *App) projectCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "project", Short: "Manage registered projects"}
	cmd.AddCommand(
		&cobra.Command{Use: "list", Short: "List projects", RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			reg, err := a.Store.LoadRegistry()
			if err != nil {
				return err
			}
			return a.output(reg.Projects, formatProjects(reg.Projects))
		}},
		&cobra.Command{Use: "rename <old> <new>", Short: "Rename a project alias", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			var result model.Project
			err := store.WithLock(filepath.Join(a.Store.GlobalRoot, "locks", "registry.lock"), func() error {
				reg, err := a.Store.LoadRegistry()
				if err != nil {
					return err
				}
				for i := range reg.Projects {
					if reg.Projects[i].Alias == args[0] {
						if err := model.ValidateAlias(args[1]); err != nil {
							return Usage(err)
						}
						if _, ok := reg.FindProject(args[1]); ok {
							return fmt.Errorf("project alias %q already exists", args[1])
						}
						projectRoot := reg.Projects[i].Root
						config, configErr := a.Store.LoadProjectConfig(projectRoot)
						if configErr != nil && !errors.Is(configErr, os.ErrNotExist) {
							return configErr
						}
						journals, journalsErr := a.Store.ListJournals()
						if journalsErr != nil {
							return journalsErr
						}
						snapshots, snapshotErr := snapshotPaths(append([]string{a.Store.RegistryPath(), filepath.Join(projectRoot, store.ProjectDir, "config.yaml")}, journalPaths(a.Store, journals)...))
						if snapshotErr != nil {
							return snapshotErr
						}
						reg.Projects[i].Alias = args[1]
						result = reg.Projects[i]
						sort.Slice(reg.Projects, func(left, right int) bool { return reg.Projects[left].Alias < reg.Projects[right].Alias })
						if err := a.Store.SaveRegistry(reg); err != nil {
							_ = restoreSnapshots(snapshots)
							return err
						}
						if configErr != nil {
							config = model.NewProjectConfig(args[1])
						}
						config.Alias = args[1]
						if err := a.Store.SaveProjectConfig(projectRoot, config); err != nil {
							_ = restoreSnapshots(snapshots)
							return err
						}
						for _, journal := range journals {
							changed := false
							for j := range journal.Planned {
								if journal.Planned[j].Project == args[0] {
									journal.Planned[j].Project = args[1]
									changed = true
								}
							}
							for j := range journal.Tomorrow {
								if journal.Tomorrow[j].Project == args[0] {
									journal.Tomorrow[j].Project = args[1]
									changed = true
								}
							}
							for j := range journal.Sessions {
								if journal.Sessions[j].Project == args[0] {
									journal.Sessions[j].Project = args[1]
									changed = true
								}
							}
							if changed {
								if err := a.Store.SaveJournal(journal); err != nil {
									_ = restoreSnapshots(snapshots)
									return err
								}
							}
						}
						return nil
					}
				}
				return fmt.Errorf("project alias %q not found", args[0])
			})
			if err != nil {
				return err
			}
			return a.output(result, fmt.Sprintf("renamed %s to %s", args[0], args[1]))
		}},
		&cobra.Command{Use: "remove <alias>", Short: "Unregister a project without deleting its files", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			return store.WithLock(filepath.Join(a.Store.GlobalRoot, "locks", "registry.lock"), func() error {
				reg, err := a.Store.LoadRegistry()
				if err != nil {
					return err
				}
				found := false
				kept := reg.Projects[:0]
				for _, p := range reg.Projects {
					if p.Alias == args[0] {
						found = true
					} else {
						kept = append(kept, p)
					}
				}
				if !found {
					return fmt.Errorf("project alias %q not found", args[0])
				}
				reg.Projects = kept
				if err := a.Store.SaveRegistry(reg); err != nil {
					return err
				}
				return a.output(map[string]any{"removed": args[0]}, "removed "+args[0])
			})
		}},
	)
	return cmd
}

func (a *App) captureCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "capture <title>", Short: "Capture a quick backlog draft", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.createTask(args[0], true)
	}}
	return cmd
}

func (a *App) taskCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "task", Short: "Create and manage PRD tasks"}
	var title, problem, outcome, acceptance, priority, due, tags, body, scope, nonGoals, risks, qaNotes, blocker string
	var estimate, remaining int
	var dependencyArgs []string
	create := &cobra.Command{Use: "create", Short: "Create a complete PRD task", RunE: func(cmd *cobra.Command, args []string) error {
		if !isTTY() && (title == "" || problem == "" || outcome == "" || acceptance == "" || estimate == 0) {
			return Usage(fmt.Errorf("non-interactive task create requires --title, --problem, --outcome, --acceptance, and --estimate"))
		}
		if title == "" {
			title = a.prompt("Title: ")
		}
		if problem == "" {
			problem = a.prompt("Problem: ")
		}
		if outcome == "" {
			outcome = a.prompt("Outcome: ")
		}
		if acceptance == "" {
			acceptance = a.prompt("Acceptance criteria: ")
		}
		if estimate == 0 {
			estimate = a.promptInt("Estimate minutes", 30)
		}
		dependencies, err := parseIDs(dependencyArgs)
		if err != nil {
			return Usage(err)
		}
		return a.createTask(title, false, taskOptions{problem: problem, outcome: outcome, acceptance: acceptance, priority: priority, estimate: estimate, remaining: remaining, due: due, tags: parseCSV(tags), deps: dependencies, body: body, scope: scope, nonGoals: nonGoals, risks: risks, qaNotes: qaNotes})
	}}
	create.Flags().StringVar(&title, "title", "", "task title")
	create.Flags().StringVar(&problem, "problem", "", "problem statement")
	create.Flags().StringVar(&outcome, "outcome", "", "desired outcome")
	create.Flags().StringVar(&acceptance, "acceptance", "", "acceptance criteria")
	create.Flags().StringVar(&priority, "priority", "", "P0-P3 priority")
	create.Flags().IntVar(&estimate, "estimate", 0, "estimate in minutes")
	create.Flags().IntVar(&remaining, "remaining", 0, "remaining minutes (defaults to estimate)")
	create.Flags().StringVar(&due, "due", "", "due date YYYY-MM-DD")
	create.Flags().StringVar(&tags, "tags", "", "comma-separated tags")
	create.Flags().StringSliceVar(&dependencyArgs, "depends", nil, "task IDs this task depends on")
	create.Flags().StringVar(&body, "body", "", "additional Markdown body")
	create.Flags().StringVar(&scope, "scope", "", "in-scope work")
	create.Flags().StringVar(&nonGoals, "non-goals", "", "out-of-scope work")
	create.Flags().StringVar(&risks, "risks", "", "known risks")
	create.Flags().StringVar(&qaNotes, "qa-notes", "", "QA notes")

	var status, listPriority, listTag, listDue string
	var allProjects, includeArchived bool
	list := &cobra.Command{Use: "list", Short: "List tasks", RunE: func(cmd *cobra.Command, args []string) error {
		if err := a.ensure(); err != nil {
			return err
		}
		if status != "" {
			if err := model.ValidateStatus(status); err != nil {
				return Usage(err)
			}
		}
		if listPriority != "" {
			if err := model.ValidatePriority(listPriority); err != nil {
				return Usage(err)
			}
		}
		if listDue != "" {
			if _, err := time.Parse("2006-01-02", listDue); err != nil {
				return Usage(fmt.Errorf("--due must be YYYY-MM-DD"))
			}
		}
		reg, err := a.Store.LoadRegistry()
		if err != nil {
			return err
		}
		projects, err := a.selectedProjects(reg, allProjects)
		if err != nil {
			return err
		}
		rows := []taskRow{}
		for _, p := range projects {
			areas := []string{""}
			if includeArchived {
				areas = append(areas, store.ArchiveDir)
			}
			for _, area := range areas {
				tasks, err := a.Store.ListTasks(p.Root, area)
				if err != nil {
					return err
				}
				for _, t := range tasks {
					if status != "" && t.Status != status {
						continue
					}
					if listPriority != "" && t.Priority != listPriority {
						continue
					}
					if listTag != "" && !contains(t.Tags, listTag) {
						continue
					}
					if listDue != "" && t.DueDate != listDue {
						continue
					}
					rows = append(rows, taskRow{Project: p.Alias, Task: t})
				}
			}
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Task.ID < rows[j].Task.ID })
		return a.output(rows, formatTasks(rows))
	}}
	list.Flags().StringVar(&status, "status", "", "filter status")
	list.Flags().StringVar(&listPriority, "priority", "", "filter priority")
	list.Flags().StringVar(&listTag, "tag", "", "filter tag")
	list.Flags().StringVar(&listDue, "due", "", "filter due date")
	list.Flags().BoolVar(&allProjects, "all-projects", false, "include all registered projects")
	list.Flags().BoolVar(&includeArchived, "include-archived", false, "include archived tasks")

	show := &cobra.Command{Use: "show <id>", Short: "Show a PRD task", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		p, t, err := a.findTaskArg(args[0])
		if err != nil {
			return err
		}
		return a.output(map[string]any{"project": p.Alias, "task": t}, formatTask(p.Alias, t))
	}}
	edit := &cobra.Command{Use: "edit <id>", Short: "Edit a task in $EDITOR", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		p, t, err := a.findTaskArg(args[0])
		if err != nil {
			return err
		}
		return a.editTask(p, t)
	}}
	var newStatus string
	setStatus := &cobra.Command{Use: "status <id> <status|resume>", Short: "Change task status", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		p, t, err := a.findTaskArg(args[0])
		if err != nil {
			return err
		}
		newStatus = args[1]
		if newStatus == "resume" {
			newStatus = t.PreviousStatus
			if newStatus == "" {
				newStatus = model.StatusPlanned
			}
		}
		return a.setStatus(p, t, newStatus, blocker)
	}}
	setStatus.Flags().StringVar(&blocker, "blocker", "", "blocker reason")
	depend := &cobra.Command{Use: "depend", Short: "Manage task dependencies"}
	depend.AddCommand(
		&cobra.Command{Use: "add <id> <dependency-id>", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			p, t, err := a.findTaskArg(args[0])
			if err != nil {
				return err
			}
			dep, err := strconv.ParseUint(args[1], 10, 64)
			if err != nil {
				return Usage(fmt.Errorf("invalid task id %q", args[1]))
			}
			if dep == 0 {
				return Usage(fmt.Errorf("invalid task id %q", args[1]))
			}
			if containsID(t.Dependencies, dep) {
				return fmt.Errorf("dependency already exists")
			}
			t.Dependencies = append(t.Dependencies, dep)
			if err := a.validateDependencies(t); err != nil {
				return Usage(err)
			}
			_, err = a.Store.SaveTask(p.Root, t)
			return err
		}},
		&cobra.Command{Use: "remove <id> <dependency-id>", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			p, t, err := a.findTaskArg(args[0])
			if err != nil {
				return err
			}
			dep, err := strconv.ParseUint(args[1], 10, 64)
			if err != nil {
				return Usage(fmt.Errorf("invalid task id %q", args[1]))
			}
			if dep == 0 {
				return Usage(fmt.Errorf("invalid task id %q", args[1]))
			}
			next := t.Dependencies[:0]
			for _, id := range t.Dependencies {
				if id != dep {
					next = append(next, id)
				}
			}
			t.Dependencies = next
			_, err = a.Store.SaveTask(p.Root, t)
			return err
		}},
	)
	start := &cobra.Command{Use: "start <id>", Short: "Start a focus session", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		p, t, err := a.findTaskArg(args[0])
		if err != nil {
			return err
		}
		return a.startTask(p, t)
	}}
	var stopMinutes int
	var discardSession bool
	var continueSession, adjustSession bool
	stop := &cobra.Command{Use: "stop [id]", Short: "Stop the active focus session", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if continueSession && discardSession {
			return Usage(fmt.Errorf("--continue and --discard cannot be combined"))
		}
		if adjustSession && stopMinutes <= 0 {
			return Usage(fmt.Errorf("--adjust requires --minutes"))
		}
		if stopMinutes < 0 {
			return Usage(fmt.Errorf("--minutes cannot be negative"))
		}
		if continueSession {
			return a.continueTask(args)
		}
		return a.stopTask(args, stopMinutes, discardSession, false)
	}}
	stop.Flags().IntVar(&stopMinutes, "minutes", 0, "override recorded elapsed minutes")
	stop.Flags().BoolVar(&discardSession, "discard", false, "discard the active session without recording time")
	stop.Flags().BoolVar(&continueSession, "continue", false, "keep an interrupted session running")
	stop.Flags().BoolVar(&adjustSession, "adjust", false, "record a manually adjusted duration; use with --minutes")
	archive := &cobra.Command{Use: "archive <id>", Short: "Archive a done task", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		p, t, err := a.findTaskArg(args[0])
		if err != nil {
			return err
		}
		if t.Status != model.StatusDone {
			return fmt.Errorf("only done tasks can be archived")
		}
		if filepath.Dir(t.Path) != store.ProjectTaskRoot(p.Root) {
			return Usage(fmt.Errorf("task %d is already archived or not active", t.ID))
		}
		t.ArchivedAt = time.Now().Format(time.RFC3339)
		_, err = a.Store.MoveTask(p.Root, t, store.ArchiveDir)
		if err != nil {
			return err
		}
		return a.output(map[string]any{"archived": t.ID}, fmt.Sprintf("archived %d", t.ID))
	}}
	remove := &cobra.Command{Use: "delete <id>", Short: "Move a task to trash", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		p, t, err := a.findTaskArg(args[0])
		if err != nil {
			return err
		}
		area := filepath.Dir(t.Path)
		if area != store.ProjectTaskRoot(p.Root) && area != store.ProjectArchiveRoot(p.Root) {
			return Usage(fmt.Errorf("task %d is already deleted or outside task storage", t.ID))
		}
		t.DeletedAt = time.Now().Format(time.RFC3339)
		_, err = a.Store.MoveTask(p.Root, t, store.TrashDir)
		if err != nil {
			return err
		}
		return a.output(map[string]any{"deleted": t.ID, "recoverable": true}, fmt.Sprintf("moved %d to trash", t.ID))
	}}
	restore := &cobra.Command{Use: "restore <id>", Short: "Restore a trashed task", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		p, t, err := a.findTaskArg(args[0])
		if err != nil {
			return err
		}
		if filepath.Dir(t.Path) != store.ProjectTrashRoot(p.Root) {
			return Usage(fmt.Errorf("task %d is not in trash", t.ID))
		}
		oldPath := t.Path
		t.DeletedAt = ""
		t.ArchivedAt = ""
		t.Path = ""
		_, err = a.Store.SaveTask(p.Root, t)
		if err != nil {
			return err
		}
		if removeErr := os.Remove(oldPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return removeErr
		}
		return a.output(map[string]any{"restored": t.ID}, fmt.Sprintf("restored %d", t.ID))
	}}
	var purgeConfirmed bool
	purge := &cobra.Command{Use: "purge <id>", Short: "Permanently remove a trashed task", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		started := time.Now()
		if !purgeConfirmed {
			return Usage(fmt.Errorf("purge requires --yes"))
		}
		p, t, err := a.findTaskArg(args[0])
		if err != nil {
			return err
		}
		if err := a.Store.PurgeTask(p.Root, t); err != nil {
			return err
		}
		a.Log.Warn("task purged", "operation", "task.purge", "task_id", t.ID, "project", p.Alias, "duration_ms", time.Since(started).Milliseconds(), "result", "success")
		return a.output(map[string]any{"purged": t.ID}, fmt.Sprintf("purged %d", t.ID))
	}}
	purge.Flags().BoolVar(&purgeConfirmed, "yes", false, "confirm permanent removal")
	cmd.AddCommand(create, list, show, edit, setStatus, depend, start, stop, archive, remove, restore, purge)
	return cmd
}

type taskOptions struct {
	problem, outcome, acceptance, priority string
	scope, nonGoals, risks, qaNotes        string
	estimate, remaining                    int
	due                                    string
	tags                                   []string
	deps                                   []uint64
	body                                   string
}
type taskRow struct {
	Project string     `json:"project"`
	Task    model.Task `json:"task"`
}

func (a *App) createTask(title string, draft bool, opts ...taskOptions) error {
	started := time.Now()
	if err := a.ensure(); err != nil {
		return err
	}
	p, err := a.resolveProject()
	if err != nil {
		return err
	}
	o := taskOptions{estimate: 30}
	if len(opts) > 0 {
		o = opts[0]
	}
	config, configErr := a.Store.LoadProjectConfig(p.Root)
	if configErr != nil {
		return configErr
	}
	if config.Alias != p.Alias {
		return fmt.Errorf("project config alias %q does not match registered alias %q; run doctor", config.Alias, p.Alias)
	}
	if o.priority == "" {
		o.priority = config.DefaultPriority
	}
	o.tags = appendUnique(config.DefaultTags, o.tags...)
	if o.priority == "" {
		o.priority = "P2"
	}
	if o.estimate <= 0 {
		if o.estimate < 0 {
			return Usage(fmt.Errorf("estimate minutes must be positive"))
		}
		o.estimate = 30
	}
	if o.remaining == 0 {
		o.remaining = o.estimate
	}
	if o.remaining < 0 {
		return Usage(fmt.Errorf("remaining minutes cannot be negative"))
	}
	if !draft && strings.TrimSpace(o.problem) == "" {
		return fmt.Errorf("problem is required")
	}
	if !draft && strings.TrimSpace(o.outcome) == "" {
		return fmt.Errorf("outcome is required")
	}
	if !draft && strings.TrimSpace(o.acceptance) == "" {
		return fmt.Errorf("acceptance criteria is required")
	}
	if err := model.ValidatePriority(o.priority); err != nil {
		return err
	}
	var task model.Task
	err = store.WithLock(filepath.Join(a.Store.GlobalRoot, "locks", "registry.lock"), func() error {
		reg, err := a.Store.LoadRegistry()
		if err != nil {
			return err
		}
		id := reg.AllocateID()
		body := o.body
		if body == "" {
			body = taskBody(o.problem, o.outcome, o.acceptance, o.scope, o.nonGoals, o.risks, o.qaNotes, draft)
		}
		task = model.Task{SchemaVersion: model.SchemaVersion, ID: id, Title: strings.TrimSpace(title), Slug: model.SafeSlug(title), Problem: strings.TrimSpace(o.problem), Outcome: strings.TrimSpace(o.outcome), Acceptance: strings.TrimSpace(o.acceptance), Scope: strings.TrimSpace(o.scope), NonGoals: strings.TrimSpace(o.nonGoals), Risks: strings.TrimSpace(o.risks), QANotes: strings.TrimSpace(o.qaNotes), Status: model.StatusBacklog, Priority: o.priority, EstimateMinutes: o.estimate, RemainingMinutes: o.remaining, CreatedAt: time.Now(), UpdatedAt: time.Now(), DueDate: o.due, Tags: o.tags, Dependencies: o.deps, Draft: draft, Body: body}
		if err := task.Validate(); err != nil {
			return err
		}
		if err := a.validateDependencies(task); err != nil {
			return Usage(err)
		}
		// Commit the counter first. If the task write fails, the consumed ID is
		// intentionally lost; reusing it could collide with a partially written file.
		if err := a.Store.SaveRegistry(reg); err != nil {
			return err
		}
		path, err := a.Store.SaveTask(p.Root, task)
		if err != nil {
			return err
		}
		task.Path = path
		a.Log.Info("task created", "operation", "task.create", "task_id", task.ID, "project", p.Alias, "status", task.Status, "duration_ms", time.Since(started).Milliseconds(), "result", "success")
		return nil
	})
	if err != nil {
		return err
	}
	return a.output(map[string]any{"project": p.Alias, "task": task}, fmt.Sprintf("created %d (%s)", task.ID, p.Alias))
}

func (a *App) resolveProject() (model.Project, error) {
	reg, err := a.Store.LoadRegistry()
	if err != nil {
		return model.Project{}, err
	}
	if a.ProjectFlag != "" {
		if p, ok := reg.FindProject(a.ProjectFlag); ok {
			return p, nil
		}
		return model.Project{}, fmt.Errorf("project alias %q not found", a.ProjectFlag)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return model.Project{}, err
	}
	root := cwd
	for {
		if info, statErr := os.Stat(filepath.Join(root, store.ProjectDir)); statErr == nil && info.IsDir() {
			candidate := root
			if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
				candidate = resolved
			}
			if p, ok := reg.ProjectForRoot(candidate); ok {
				return p, nil
			}
		}
		next := filepath.Dir(root)
		if next == root {
			break
		}
		root = next
	}
	return model.Project{}, fmt.Errorf("no registered dt-task project; run dt-task init or pass --project")
}

func (a *App) selectedProjects(reg model.Registry, all bool) ([]model.Project, error) {
	if all {
		return reg.Projects, nil
	}
	p, err := a.resolveProject()
	if err != nil {
		return nil, err
	}
	return []model.Project{p}, nil
}

func (a *App) findTaskArg(raw string) (model.Project, model.Task, error) {
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return model.Project{}, model.Task{}, Usage(fmt.Errorf("task id must be numeric: %w", err))
	}
	reg, err := a.Store.LoadRegistry()
	if err != nil {
		return model.Project{}, model.Task{}, err
	}
	if a.ProjectFlag != "" {
		p, err := a.resolveProject()
		if err != nil {
			return p, model.Task{}, err
		}
		for _, area := range []string{"", store.ArchiveDir, store.TrashDir} {
			ts, e := a.Store.ListTasks(p.Root, area)
			if e != nil {
				return p, model.Task{}, e
			}
			for _, t := range ts {
				if t.ID == id {
					return p, t, nil
				}
			}
		}
		return p, model.Task{}, fmt.Errorf("task %d not found in %s", id, p.Alias)
	}
	return a.Store.FindTask(reg, id)
}

func (a *App) setStatus(p model.Project, t model.Task, status, blocker string) error {
	started := time.Now()
	if strings.TrimSpace(blocker) != "" && status != model.StatusBlocked {
		return Usage(fmt.Errorf("--blocker is only valid when blocking a task"))
	}
	if filepath.Dir(t.Path) == store.ProjectTrashRoot(p.Root) {
		return Usage(fmt.Errorf("task %d is in trash; restore it first", t.ID))
	}
	if err := model.ValidateTransition(t.Status, status); err != nil {
		return err
	}
	if status == model.StatusBlocked && blocker == "" {
		blocker = t.Blocker
	}
	if status == model.StatusBlocked && strings.TrimSpace(blocker) == "" && t.Blocker == "" {
		return fmt.Errorf("--blocker is required when blocking a task")
	}
	if status == model.StatusBlocked {
		if t.Status != model.StatusBlocked || t.PreviousStatus == "" {
			t.PreviousStatus = t.Status
		}
		t.Blocker = blocker
		t.BlockedAt = time.Now().Format(time.RFC3339)
	}
	if t.Status == model.StatusBlocked && status != model.StatusBlocked {
		if blockedAt, parseErr := time.Parse(time.RFC3339, t.BlockedAt); parseErr == nil {
			if minutes := int(time.Since(blockedAt).Minutes()); minutes > 0 {
				t.BlockedMinutes += minutes
			}
		}
		t.Blocker = ""
		t.BlockedAt = ""
		t.PreviousStatus = ""
	}
	oldPath := ""
	if filepath.Dir(t.Path) == store.ProjectArchiveRoot(p.Root) && status != model.StatusDone {
		// Reopening archived work returns it to the active task directory.
		oldPath = t.Path
		t.Path = ""
	}
	wasDone := t.Status == model.StatusDone
	t.Status = status
	if status != model.StatusDone {
		t.ArchivedAt = ""
	}
	if status == model.StatusDone {
		t.RemainingMinutes = 0
		t.CompletedAt = time.Now().Format(time.RFC3339)
	} else if t.CompletedAt != "" {
		t.CompletedAt = ""
		if wasDone && t.RemainingMinutes == 0 {
			t.RemainingMinutes = t.EstimateMinutes
		}
	}
	_, err := a.Store.SaveTask(p.Root, t)
	if err != nil {
		return err
	}
	if oldPath != "" {
		if removeErr := os.Remove(oldPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return removeErr
		}
	}
	if err == nil {
		a.Log.Info("task status changed", "operation", "task.status", "task_id", t.ID, "status", t.Status, "project", p.Alias, "duration_ms", time.Since(started).Milliseconds(), "result", "success")
	}
	return a.output(map[string]any{"id": t.ID, "status": t.Status}, fmt.Sprintf("task %d → %s", t.ID, t.Status))
}

func (a *App) validateDependencies(t model.Task) error {
	reg, err := a.Store.LoadRegistry()
	if err != nil {
		return err
	}
	visiting := map[uint64]bool{}
	var walk func(uint64) error
	walk = func(id uint64) error {
		if id == t.ID {
			return fmt.Errorf("dependency cycle includes task %d", t.ID)
		}
		if visiting[id] {
			return fmt.Errorf("dependency cycle includes task %d", id)
		}
		_, dependency, findErr := a.Store.FindTask(reg, id)
		if findErr != nil {
			return fmt.Errorf("dependency %d not found", id)
		}
		visiting[id] = true
		for _, nested := range dependency.Dependencies {
			if err := walk(nested); err != nil {
				return err
			}
		}
		delete(visiting, id)
		return nil
	}
	for _, dep := range t.Dependencies {
		if err := walk(dep); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) dependenciesReady(t model.Task) error {
	reg, err := a.Store.LoadRegistry()
	if err != nil {
		return err
	}
	for _, dep := range t.Dependencies {
		_, dependency, findErr := a.Store.FindTask(reg, dep)
		if findErr != nil {
			return findErr
		}
		if dependency.Status != model.StatusDone {
			return fmt.Errorf("task %d is not ready: dependency %d is %s", t.ID, dep, dependency.Status)
		}
	}
	return nil
}

func (a *App) editTask(p model.Project, t model.Task) error {
	started := time.Now()
	if filepath.Dir(t.Path) == store.ProjectTrashRoot(p.Root) {
		return Usage(fmt.Errorf("task %d is in trash; restore it first", t.ID))
	}
	tmp, err := os.CreateTemp(filepath.Dir(t.Path), ".dt-task-edit-*.md")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	original, err := os.ReadFile(t.Path)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(original); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	command, err := a.editorCommand(tmpPath)
	if err != nil {
		return err
	}
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return err
	}
	updated, err := a.Store.LoadTask(tmpPath)
	if err != nil {
		return Usage(fmt.Errorf("edited task is invalid; original was preserved: %w", err))
	}
	if updated.ID != t.ID {
		return Usage(fmt.Errorf("task id cannot change; original was preserved"))
	}
	if updated.Status != t.Status {
		return Usage(fmt.Errorf("status changes must use task status; original was preserved"))
	}
	if updated.Status == model.StatusBlocked && strings.TrimSpace(updated.Blocker) == "" {
		return Usage(fmt.Errorf("blocked tasks require a blocker; original was preserved"))
	}
	if err := a.validateDependencies(updated); err != nil {
		return Usage(fmt.Errorf("edited task dependencies are invalid; original was preserved: %w", err))
	}
	updated.Path = t.Path
	_, err = a.Store.SaveTask(p.Root, updated)
	if err == nil {
		a.Log.Info("task edited", "operation", "task.edit", "task_id", t.ID, "project", p.Alias, "duration_ms", time.Since(started).Milliseconds(), "result", "success")
	}
	return err
}

func (a *App) editorCommand(path string) (*exec.Cmd, error) {
	editor := os.Getenv("DT_TASK_EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		config, configErr := a.Store.LoadGlobalConfig()
		if configErr != nil {
			return nil, configErr
		}
		editor = config.Editor
	}
	if editor == "" {
		return nil, fmt.Errorf("set EDITOR, VISUAL, or DT_TASK_EDITOR")
	}
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return nil, fmt.Errorf("invalid editor")
	}
	return exec.Command(parts[0], append(parts[1:], path)...), nil
}

func (a *App) startTask(p model.Project, t model.Task) error {
	started := time.Now()
	if filepath.Dir(t.Path) == store.ProjectTrashRoot(p.Root) {
		return Usage(fmt.Errorf("task %d is in trash; restore it first", t.ID))
	}
	if t.Status == model.StatusDone {
		return fmt.Errorf("cannot start a done task")
	}
	if t.Status == model.StatusBlocked {
		return fmt.Errorf("unblock task before starting")
	}
	if err := a.dependenciesReady(t); err != nil {
		return err
	}
	if err := a.ensure(); err != nil {
		return err
	}
	return store.WithLock(filepath.Join(a.Store.GlobalRoot, "locks", "state.lock"), func() error {
		state, err := a.Store.LoadGlobalState()
		if err != nil {
			return err
		}
		if state.ActiveTimer != nil {
			return fmt.Errorf("task %d is already active; stop it first", state.ActiveTimer.Session.TaskID)
		}
		if t.Status == model.StatusBacklog || t.Status == model.StatusPlanned {
			t.Status = model.StatusInProgress
			if _, err := a.Store.SaveTask(p.Root, t); err != nil {
				return err
			}
		}
		session := model.WorkSession{ID: fmt.Sprintf("%d-%d", t.ID, time.Now().UnixNano()), TaskID: t.ID, Project: p.Alias, StartedAt: time.Now()}
		state.ActiveTimer = &model.ActiveTimer{Session: session}
		if err := a.Store.SaveGlobalState(state); err != nil {
			return err
		}
		a.Log.Info("task started", "operation", "task.start", "task_id", t.ID, "project", p.Alias, "duration_ms", time.Since(started).Milliseconds(), "result", "success")
		return a.output(map[string]any{"started": session}, fmt.Sprintf("started task %d", t.ID))
	})
}

func (a *App) stopTask(args []string, overrideMinutes int, discard bool, quiet bool) error {
	if err := a.ensure(); err != nil {
		return err
	}
	// Lock order is days -> state. day start/end already hold days.lock;
	// direct stop commands acquire it before state.lock to avoid lost journal
	// updates and lock inversions.
	return store.WithLock(filepath.Join(a.Store.GlobalRoot, "locks", "days.lock"), func() error {
		return a.stopTaskState(args, overrideMinutes, discard, quiet)
	})
}

func (a *App) stopTaskState(args []string, overrideMinutes int, discard bool, quiet bool) error {
	started := time.Now()
	if err := a.ensure(); err != nil {
		return err
	}
	return store.WithLock(filepath.Join(a.Store.GlobalRoot, "locks", "state.lock"), func() error {
		state, err := a.Store.LoadGlobalState()
		if err != nil {
			return err
		}
		if state.ActiveTimer == nil {
			return fmt.Errorf("no active task session")
		}
		timer := state.ActiveTimer
		active := timer.Session
		if len(args) == 1 {
			id, parseErr := strconv.ParseUint(args[0], 10, 64)
			if parseErr != nil {
				return Usage(fmt.Errorf("invalid task id %q", args[0]))
			}
			if id != active.TaskID {
				return fmt.Errorf("active session belongs to task %d", active.TaskID)
			}
		}
		if discard {
			state.ActiveTimer = nil
			if err := a.Store.SaveGlobalState(state); err != nil {
				return err
			}
			if !quiet {
				return a.output(map[string]any{"discarded": active}, fmt.Sprintf("discarded task %d session", active.TaskID))
			}
			return nil
		}
		if active.StoppedAt.IsZero() {
			active.StoppedAt = time.Now()
			active.Minutes = int(active.StoppedAt.Sub(active.StartedAt).Minutes())
			if active.Minutes < 1 {
				active.Minutes = 1
			}
			if overrideMinutes > 0 {
				active.Minutes = overrideMinutes
			}
			timer.Session = active
			if err := a.Store.SaveGlobalState(state); err != nil {
				return err
			}
		} else if overrideMinutes > 0 && !timer.TaskAdjusted {
			active.Minutes = overrideMinutes
			timer.Session = active
			if err := a.Store.SaveGlobalState(state); err != nil {
				return err
			}
		}
		p, t, err := a.findAnyTaskByID(active.TaskID)
		if err != nil {
			return err
		}
		if filepath.Dir(t.Path) == store.ProjectTrashRoot(p.Root) {
			return fmt.Errorf("active task %d is in trash; restore it before stopping", active.TaskID)
		}
		if !timer.TaskAdjusted {
			if t.LastSessionID != active.ID {
				if t.RemainingMinutes > active.Minutes {
					t.RemainingMinutes -= active.Minutes
				} else if t.Status != model.StatusDone {
					t.RemainingMinutes = 0
				}
				t.LastSessionID = active.ID
				if _, err := a.Store.SaveTask(p.Root, t); err != nil {
					return err
				}
			}
			timer.TaskAdjusted = true
			if err := a.Store.SaveGlobalState(state); err != nil {
				return err
			}
		}
		if !timer.JournalRecorded {
			clock, clockErr := a.configuredNow()
			if clockErr != nil {
				return clockErr
			}
			date := clock.Format("2006-01-02")
			j, err := a.Store.LoadJournal(date)
			if errors.Is(err, os.ErrNotExist) {
				cfg, e := a.Store.LoadGlobalConfig()
				if e != nil {
					return e
				}
				j = model.NewDayJournal(date, cfg.DailyCapacityMinutes)
			} else if err != nil {
				return err
			}
			seen := false
			for _, session := range j.Sessions {
				if session.ID == active.ID {
					seen = true
					break
				}
			}
			if !seen {
				j.Sessions = append(j.Sessions, active)
				if err := a.Store.SaveJournal(j); err != nil {
					return err
				}
			}
			timer.JournalRecorded = true
			if err := a.Store.SaveGlobalState(state); err != nil {
				return err
			}
		}
		state.ActiveTimer = nil
		if err := a.Store.SaveGlobalState(state); err != nil {
			return err
		}
		a.Log.Info("task stopped", "operation", "task.stop", "task_id", active.TaskID, "minutes", active.Minutes, "project", p.Alias, "duration_ms", time.Since(started).Milliseconds(), "result", "success")
		if quiet {
			return nil
		}
		return a.output(map[string]any{"stopped": active}, fmt.Sprintf("stopped task %d (%d minutes)", active.TaskID, active.Minutes))
	})
}

func (a *App) continueTask(args []string) error {
	if err := a.ensure(); err != nil {
		return err
	}
	return store.WithLock(filepath.Join(a.Store.GlobalRoot, "locks", "state.lock"), func() error {
		state, err := a.Store.LoadGlobalState()
		if err != nil {
			return err
		}
		if state.ActiveTimer == nil {
			return fmt.Errorf("no interrupted task session")
		}
		active := state.ActiveTimer.Session
		if len(args) == 1 {
			id, parseErr := strconv.ParseUint(args[0], 10, 64)
			if parseErr != nil {
				return Usage(fmt.Errorf("task id must be numeric: %w", parseErr))
			}
			if id != active.TaskID {
				return fmt.Errorf("active session belongs to task %d", active.TaskID)
			}
		}
		if _, _, err := a.findAnyTaskByID(active.TaskID); err != nil {
			return err
		}
		a.Log.Info("task session continued", "task_id", active.TaskID)
		return a.output(map[string]any{"continued": active}, fmt.Sprintf("continued task %d session", active.TaskID))
	})
}

func (a *App) dayCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "day", Short: "Plan and review your workday"}
	var add, remove []string
	var notes, blockers, tomorrow []string
	start := &cobra.Command{Use: "start", Short: "Open today’s plan", RunE: func(cmd *cobra.Command, args []string) error { return a.dayStart(add, remove) }}
	start.Flags().StringSliceVar(&add, "add", nil, "task IDs to add to today")
	start.Flags().StringSliceVar(&remove, "remove", nil, "task IDs to remove from today")
	end := &cobra.Command{Use: "end", Short: "Close today and carry unfinished work", RunE: func(cmd *cobra.Command, args []string) error { return a.dayEnd(notes, blockers, tomorrow) }}
	end.Flags().StringSliceVar(&notes, "note", nil, "review note")
	end.Flags().StringSliceVar(&blockers, "blocker", nil, "day blocker")
	end.Flags().StringSliceVar(&tomorrow, "tomorrow", nil, "task IDs to add to tomorrow")
	cmd.AddCommand(
		start,
		&cobra.Command{Use: "status", Short: "Show today’s plan", RunE: func(cmd *cobra.Command, args []string) error { return a.dayStatus() }},
		end,
	)
	return cmd
}

func (a *App) dayStart(addArgs, removeArgs []string) error {
	if err := a.ensure(); err != nil {
		return err
	}
	return store.WithLock(filepath.Join(a.Store.GlobalRoot, "locks", "days.lock"), func() error {
		return a.dayStartLocked(addArgs, removeArgs)
	})
}

func (a *App) dayStartLocked(addArgs, removeArgs []string) error {
	now, err := a.configuredNow()
	if err != nil {
		return err
	}
	timerWarning, err := a.recoverInterruptedTimer(now)
	if err != nil {
		return err
	}
	date := now.Format("2006-01-02")
	cfg, err := a.Store.LoadGlobalConfig()
	if err != nil {
		return err
	}
	j, err := a.Store.LoadJournal(date)
	if errors.Is(err, os.ErrNotExist) {
		j = model.NewDayJournal(date, cfg.DailyCapacityMinutes)
		j.OpenedAt = now
		if prev, e := a.Store.LoadJournal(now.AddDate(0, 0, -1).Format("2006-01-02")); e == nil {
			j.Planned = append(j.Planned, prev.Tomorrow...)
		}
		if err := a.Store.SaveJournal(j); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if !j.ClosedAt.IsZero() {
		return Usage(fmt.Errorf("day %s is already closed", date))
	}
	reg, err := a.Store.LoadRegistry()
	if err != nil {
		return err
	}
	addIDs, err := parseIDs(addArgs)
	if err != nil {
		return Usage(err)
	}
	removeIDs, err := parseIDs(removeArgs)
	if err != nil {
		return Usage(err)
	}
	for _, id := range addIDs {
		project, task, findErr := a.Store.FindTask(reg, id)
		if findErr != nil {
			return findErr
		}
		if task.Status != model.StatusDone {
			before := len(j.Planned)
			j.EnsureTask(model.TaskRef{ID: id, Project: project.Alias})
			if len(j.Planned) != before {
				if task.Status == model.StatusBacklog {
					task.Status = model.StatusPlanned
					if _, saveErr := a.Store.SaveTask(project.Root, task); saveErr != nil {
						return saveErr
					}
				}
			}
		}
	}
	if len(removeIDs) > 0 {
		filtered := j.Planned[:0]
		for _, ref := range j.Planned {
			if !containsID(removeIDs, ref.ID) {
				filtered = append(filtered, ref)
			}
		}
		j.Planned = filtered
	}
	if len(addIDs) > 0 || len(removeIDs) > 0 {
		if err := a.Store.SaveJournal(j); err != nil {
			return err
		}
	}
	// Seed the journal with explicitly planned work and overdue/due work so a
	// developer can run `day start` without first hand-editing a journal.
	journalChanged := false
	for _, project := range reg.Projects {
		tasks, listErr := a.Store.ListTasks(project.Root, "")
		if listErr != nil {
			return listErr
		}
		for _, task := range tasks {
			if !containsID(removeIDs, task.ID) && (task.Status == model.StatusPlanned || (task.DueDate != "" && task.DueDate <= date && task.Status != model.StatusDone)) {
				before := len(j.Planned)
				j.EnsureTask(model.TaskRef{ID: task.ID, Project: project.Alias})
				journalChanged = journalChanged || before != len(j.Planned)
			}
		}
	}
	if journalChanged {
		if err := a.Store.SaveJournal(j); err != nil {
			return err
		}
	}
	rows := []taskRow{}
	alerts := []string{}
	total := 0
	for _, project := range reg.Projects {
		tasks, listErr := a.Store.ListTasks(project.Root, "")
		if listErr != nil {
			return listErr
		}
		for _, task := range tasks {
			if task.Draft {
				alerts = append(alerts, fmt.Sprintf("task %d is a draft and needs refinement", task.ID))
			}
			if task.Status == model.StatusBlocked {
				alerts = append(alerts, fmt.Sprintf("task %d is blocked: %s", task.ID, valueOr(task.Blocker, "reason not recorded")))
			}
		}
	}
	for _, ref := range j.Planned {
		p, t, e := a.Store.FindTask(reg, ref.ID)
		if e != nil {
			continue
		}
		rows = append(rows, taskRow{Project: p.Alias, Task: t})
		if t.Status != model.StatusDone {
			total += t.RemainingMinutes
		}
	}
	warnings := []string{}
	if timerWarning != "" {
		warnings = append(warnings, timerWarning)
	}
	if total > j.Capacity {
		warnings = append(warnings, fmt.Sprintf("planned %d minutes exceeds %d-minute capacity", total, j.Capacity))
	}
	for _, r := range rows {
		if r.Task.Carryovers >= 3 {
			warnings = append(warnings, fmt.Sprintf("task %d has carried over %d times", r.Task.ID, r.Task.Carryovers))
		}
	}
	warnings = append(alerts, warnings...)
	return a.output(map[string]any{"date": date, "capacity_minutes": j.Capacity, "planned_minutes": total, "planned": rows, "warnings": warnings}, formatDay(date, j, rows, warnings))
}

// recoverInterruptedTimer gives a developer a safe choice when a persisted
// timer crosses a local-day boundary. Non-TTY callers stay non-interactive and
// receive a warning; they can explicitly use task stop --continue/--adjust/
// --discard in automation.
func (a *App) recoverInterruptedTimer(now time.Time) (string, error) {
	state, err := a.Store.LoadGlobalState()
	if err != nil {
		return "", err
	}
	if state.ActiveTimer == nil {
		return "", nil
	}
	active := state.ActiveTimer.Session
	if active.StartedAt.In(now.Location()).Format("2006-01-02") == now.Format("2006-01-02") {
		return "", nil
	}
	warning := fmt.Sprintf("interrupted timer for task %d started %s; choose continue, adjust, or discard", active.TaskID, active.StartedAt.In(now.Location()).Format(time.RFC3339))
	if !isTTY() {
		return warning, nil
	}
	choice := strings.ToLower(strings.TrimSpace(a.prompt(warning + " [continue/adjust/discard]: ")))
	switch choice {
	case "", "continue":
		return "", nil
	case "discard":
		if err := a.stopTaskState(nil, 0, true, true); err != nil {
			return "", err
		}
		return "", nil
	case "adjust":
		minutes := a.promptInt("Recorded minutes", 1)
		if err := a.stopTaskState(nil, minutes, false, true); err != nil {
			return "", err
		}
		return "", nil
	default:
		return "", Usage(fmt.Errorf("timer recovery must be continue, adjust, or discard"))
	}
}

func (a *App) dayStatus() error {
	if err := a.ensure(); err != nil {
		return err
	}
	return store.WithLock(filepath.Join(a.Store.GlobalRoot, "locks", "days.lock"), func() error {
		return a.dayStatusLocked()
	})
}

func (a *App) dayStatusLocked() error {
	now, err := a.configuredNow()
	if err != nil {
		return err
	}
	date := now.Format("2006-01-02")
	j, err := a.Store.LoadJournal(date)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no plan for %s; run dt-task day start", date)
	}
	if err != nil {
		return err
	}
	reg, err := a.Store.LoadRegistry()
	if err != nil {
		return err
	}
	rows := []taskRow{}
	for _, ref := range j.Planned {
		p, t, e := a.Store.FindTask(reg, ref.ID)
		if e == nil {
			rows = append(rows, taskRow{Project: p.Alias, Task: t})
		}
	}
	return a.output(map[string]any{"date": date, "journal": j, "planned": rows}, formatDay(date, j, rows, nil))
}

func (a *App) dayEnd(noteArgs, blockerArgs, tomorrowArgs []string) error {
	if err := a.ensure(); err != nil {
		return err
	}
	return store.WithLock(filepath.Join(a.Store.GlobalRoot, "locks", "days.lock"), func() error {
		return a.dayEndLocked(noteArgs, blockerArgs, tomorrowArgs)
	})
}

func (a *App) dayEndLocked(noteArgs, blockerArgs, tomorrowArgs []string) error {
	now, err := a.configuredNow()
	if err != nil {
		return err
	}
	date := now.Format("2006-01-02")
	j, err := a.Store.LoadJournal(date)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no plan for %s; run dt-task day start", date)
	}
	if err != nil {
		return err
	}
	if !j.ClosedAt.IsZero() {
		return Usage(fmt.Errorf("day %s is already closed", date))
	}
	state, err := a.Store.LoadGlobalState()
	if err != nil {
		return err
	}
	if state.ActiveTimer != nil {
		if err := a.stopTaskState(nil, 0, false, true); err != nil {
			return err
		}
		state, err = a.Store.LoadGlobalState()
		if err != nil {
			return err
		}
		j, err = a.Store.LoadJournal(date)
		if err != nil {
			return err
		}
	}
	reg, err := a.Store.LoadRegistry()
	if err != nil {
		return err
	}
	tomorrow := []model.TaskRef{}
	completed := []uint64{}
	completedEstimate := 0
	for _, ref := range j.Planned {
		p, t, e := a.Store.FindTask(reg, ref.ID)
		if e != nil {
			continue
		}
		if t.Status == model.StatusDone {
			completed = append(completed, t.ID)
			completedEstimate += t.EstimateMinutes
			continue
		}
		t.Carryovers++
		if t.Status != model.StatusBlocked {
			t.Status = model.StatusPlanned
		}
		if _, e = a.Store.SaveTask(p.Root, t); e != nil {
			return e
		}
		tomorrow = appendRefUnique(tomorrow, ref)
	}
	completedSet := map[uint64]bool{}
	for _, id := range completed {
		completedSet[id] = true
	}
	for _, session := range j.Sessions {
		if completedSet[session.TaskID] {
			continue
		}
		if _, task, findErr := a.findAnyTaskByID(session.TaskID); findErr == nil && task.Status == model.StatusDone {
			completed = append(completed, task.ID)
			completedSet[task.ID] = true
			completedEstimate += task.EstimateMinutes
		}
	}
	for _, raw := range noteArgs {
		if strings.TrimSpace(raw) != "" {
			j.Notes = append(j.Notes, strings.TrimSpace(raw))
		}
	}
	for _, raw := range blockerArgs {
		if strings.TrimSpace(raw) != "" {
			j.Blockers = append(j.Blockers, strings.TrimSpace(raw))
		}
	}
	ids, err := parseIDs(tomorrowArgs)
	if err != nil {
		return Usage(err)
	}
	for _, id := range ids {
		project, task, findErr := a.Store.FindTask(reg, id)
		if findErr != nil {
			return findErr
		}
		if task.Status == model.StatusDone {
			continue
		}
		if task.Status == model.StatusBacklog {
			task.Status = model.StatusPlanned
			if _, saveErr := a.Store.SaveTask(project.Root, task); saveErr != nil {
				return saveErr
			}
		}
		tomorrow = appendRefUnique(tomorrow, model.TaskRef{ID: id, Project: project.Alias})
	}
	j.Completed = append(j.Completed, completed...)
	j.Tomorrow = tomorrow
	actual := 0
	for _, session := range j.Sessions {
		actual += session.Minutes
	}
	j.EstimateVarianceMinutes = actual - completedEstimate
	j.ClosedAt = now
	if err := a.Store.SaveJournal(j); err != nil {
		return err
	}
	return a.output(map[string]any{"date": date, "completed": completed, "tomorrow": tomorrow, "focus_minutes": actual, "estimate_variance_minutes": j.EstimateVarianceMinutes, "notes": j.Notes, "blockers": j.Blockers}, fmt.Sprintf("closed %s; carried %d task(s) to tomorrow (%d focus minutes)", date, len(tomorrow), actual))
}

func (a *App) analyticsCommand() *cobra.Command {
	var days int
	var allTime bool
	var fromDate, toDate string
	cmd := &cobra.Command{Use: "analytics", Short: "Show local productivity analytics", RunE: func(cmd *cobra.Command, args []string) error {
		if err := a.ensure(); err != nil {
			return err
		}
		if days <= 0 {
			return Usage(fmt.Errorf("--days must be positive"))
		}
		if allTime && (fromDate != "" || toDate != "") {
			return Usage(fmt.Errorf("--all-time cannot be combined with --from or --to"))
		}
		now, err := a.configuredNow()
		if err != nil {
			return err
		}
		end := now
		start := now.AddDate(0, 0, -6)
		if fromDate != "" {
			start, err = time.ParseInLocation("2006-01-02", fromDate, now.Location())
			if err != nil {
				return Usage(fmt.Errorf("--from must be YYYY-MM-DD"))
			}
		}
		if toDate != "" {
			end, err = time.ParseInLocation("2006-01-02", toDate, now.Location())
			if err != nil {
				return Usage(fmt.Errorf("--to must be YYYY-MM-DD"))
			}
		}
		if days > 0 && fromDate == "" && toDate == "" && !allTime {
			start = end.AddDate(0, 0, -(days - 1))
		}
		if allTime {
			start = time.Time{}
		}
		if end.Before(start) {
			return Usage(fmt.Errorf("analytics range end must not precede start"))
		}
		journals, err := a.Store.ListJournals()
		if err != nil {
			return err
		}
		selected := []model.DayJournal{}
		for _, journal := range journals {
			date, parseErr := time.ParseInLocation("2006-01-02", journal.Date, now.Location())
			if parseErr == nil && !date.Before(start) && !date.After(end) {
				selected = append(selected, journal)
			}
		}
		reg, err := a.Store.LoadRegistry()
		if err != nil {
			return err
		}
		taskByID := map[uint64]taskRow{}
		statusCounts := map[string]int{}
		priorityCounts := map[string]int{}
		projectCounts := map[string]int{}
		staleCount, blockedMinutes := 0, 0
		for _, project := range reg.Projects {
			var tasks []model.Task
			for _, area := range []string{"", store.ArchiveDir} {
				listed, listErr := a.Store.ListTasks(project.Root, area)
				if listErr != nil {
					return listErr
				}
				tasks = append(tasks, listed...)
			}
			for _, task := range tasks {
				taskByID[task.ID] = taskRow{Project: project.Alias, Task: task}
				statusCounts[task.Status]++
				priorityCounts[task.Priority]++
				projectCounts[project.Alias]++
				if task.Carryovers >= 3 && task.Status != model.StatusDone {
					staleCount++
				}
				blockedMinutes += task.BlockedMinutes
				if task.Status == model.StatusBlocked && task.BlockedAt != "" {
					if blockedAt, parseErr := time.Parse(time.RFC3339, task.BlockedAt); parseErr == nil {
						if minutes := int(now.Sub(blockedAt).Minutes()); minutes > 0 {
							blockedMinutes += minutes
						}
					}
				}
			}
		}
		completedSet := map[uint64]bool{}
		actualByTask := map[uint64]int{}
		completed, carryovers, sessions, focus := 0, 0, 0, 0
		for _, journal := range selected {
			for _, id := range journal.Completed {
				if !completedSet[id] {
					completedSet[id] = true
					completed++
				}
			}
			for _, session := range journal.Sessions {
				sessions++
				focus += session.Minutes
				actualByTask[session.TaskID] += session.Minutes
			}
			carryovers += len(journal.Tomorrow)
		}
		estimateTotal, actualTotal := 0, 0
		for id := range completedSet {
			if row, ok := taskByID[id]; ok {
				estimateTotal += row.Task.EstimateMinutes
				actualTotal += actualByTask[id]
			}
		}
		planned := completed + carryovers
		completionRate := 0.0
		if planned > 0 {
			completionRate = float64(completed) / float64(planned) * 100
		}
		data := map[string]any{"start": formatDateOrEmpty(start), "end": end.Format("2006-01-02"), "all_time": allTime, "journals": len(selected), "completed": completed, "completion_rate": completionRate, "sessions": sessions, "focus_minutes": focus, "carryovers": carryovers, "stale_tasks": staleCount, "blocked_minutes": blockedMinutes, "estimate_minutes": estimateTotal, "actual_minutes": actualTotal, "estimate_variance_minutes": actualTotal - estimateTotal, "status_breakdown": statusCounts, "priority_breakdown": priorityCounts, "project_breakdown": projectCounts}
		return a.output(data, fmt.Sprintf("analytics %s–%s: %.1f%% complete, %d focus minutes, %d carryovers, %d stale", formatDateOrEmpty(start), end.Format("2006-01-02"), completionRate, focus, carryovers, staleCount))
	}}
	cmd.Flags().IntVar(&days, "days", 7, "number of days")
	cmd.Flags().BoolVar(&allTime, "all-time", false, "include all stored journals")
	cmd.Flags().StringVar(&fromDate, "from", "", "range start YYYY-MM-DD")
	cmd.Flags().StringVar(&toDate, "to", "", "range end YYYY-MM-DD")
	return cmd
}

func (a *App) doctorCommand() *cobra.Command {
	var fix bool
	cmd := &cobra.Command{Use: "doctor", Short: "Check task storage health", RunE: func(cmd *cobra.Command, args []string) error {
		if err := a.Store.EnsureGlobal(); err != nil {
			return err
		}
		reg, err := a.Store.LoadRegistry()
		if err != nil {
			return err
		}
		issues := []string{}
		checked := 0
		fixed := 0
		seenIDs := map[uint64]string{}
		maxTaskID := uint64(0)
		seenAliases := map[string]bool{}
		seenRoots := map[string]bool{}
		projectFixes := map[string]model.Project{}
		orphanTimer := false
		type taskFix struct {
			root string
			task model.Task
		}
		fixes := []taskFix{}
		fixableIssues := map[string]bool{}
		if _, configErr := a.Store.LoadGlobalConfig(); configErr != nil {
			issues = append(issues, "global config: "+configErr.Error())
		}
		for _, p := range reg.Projects {
			if seenAliases[p.Alias] {
				issues = append(issues, fmt.Sprintf("duplicate project alias: %s", p.Alias))
			}
			seenAliases[p.Alias] = true
			rootKey, _ := filepath.Abs(p.Root)
			if seenRoots[rootKey] {
				issues = append(issues, fmt.Sprintf("duplicate project root: %s", p.Root))
			}
			seenRoots[rootKey] = true
			if _, e := os.Stat(p.Root); e != nil {
				issues = append(issues, fmt.Sprintf("project %s root missing: %s", p.Alias, p.Root))
				continue
			}
			if info, e := os.Stat(filepath.Join(p.Root, store.ProjectDir)); e != nil || !info.IsDir() {
				issue := fmt.Sprintf("project %s missing .task", p.Alias)
				issues = append(issues, issue)
				fixableIssues[issue] = true
				projectFixes[p.Root] = p
				continue
			}
			for _, dir := range []string{store.ProjectTaskRoot(p.Root), store.ProjectArchiveRoot(p.Root), store.ProjectTrashRoot(p.Root)} {
				if info, dirErr := os.Stat(dir); dirErr != nil || !info.IsDir() {
					issue := fmt.Sprintf("project %s missing task directory: %s", p.Alias, dir)
					issues = append(issues, issue)
					fixableIssues[issue] = true
					projectFixes[p.Root] = p
				}
			}
			if config, configErr := a.Store.LoadProjectConfig(p.Root); configErr != nil {
				issue := fmt.Sprintf("project %s config: %v", p.Alias, configErr)
				issues = append(issues, issue)
				if errors.Is(configErr, os.ErrNotExist) {
					fixableIssues[issue] = true
					projectFixes[p.Root] = p
				}
			} else if config.Alias != p.Alias {
				issue := fmt.Sprintf("project %s config alias mismatch: %s", p.Alias, config.Alias)
				issues = append(issues, issue)
				fixableIssues[issue] = true
				projectFixes[p.Root] = p
			}
			for _, area := range []string{"", store.ArchiveDir, store.TrashDir} {
				dir := store.ProjectTaskRoot(p.Root)
				if area == store.ArchiveDir {
					dir = store.ProjectArchiveRoot(p.Root)
				}
				if area == store.TrashDir {
					dir = store.ProjectTrashRoot(p.Root)
				}
				entries, readErr := os.ReadDir(dir)
				if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
					issues = append(issues, fmt.Sprintf("read %s: %v", dir, readErr))
					continue
				}
				for _, entry := range entries {
					if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
						continue
					}
					path := filepath.Join(dir, entry.Name())
					t, loadErr := a.Store.LoadTask(path)
					checked++
					if loadErr != nil {
						issues = append(issues, loadErr.Error())
						continue
					}
					if t.ID > maxTaskID {
						maxTaskID = t.ID
					}
					if previous, duplicate := seenIDs[t.ID]; duplicate {
						issues = append(issues, fmt.Sprintf("duplicate task ID %d: %s and %s", t.ID, previous, path))
					} else {
						seenIDs[t.ID] = path
					}
					if filepath.Base(t.Path) != t.Filename() {
						issue := fmt.Sprintf("task %d filename mismatch: %s", t.ID, filepath.Base(t.Path))
						issues = append(issues, issue)
						fixableIssues[issue] = true
						fixes = append(fixes, taskFix{root: p.Root, task: t})
					}
					if depErr := a.validateDependencies(t); depErr != nil {
						issues = append(issues, fmt.Sprintf("task %d dependency issue: %v", t.ID, depErr))
					}
				}
			}
		}
		registryCounterFix := false
		if maxTaskID > 0 && reg.NextTaskID <= maxTaskID {
			issue := fmt.Sprintf("registry next_task_id %d is not above max task ID %d", reg.NextTaskID, maxTaskID)
			issues = append(issues, issue)
			fixableIssues[issue] = true
			registryCounterFix = true
		}
		if state, stateErr := a.Store.LoadGlobalState(); stateErr == nil && state.ActiveTimer != nil {
			if _, _, findErr := a.findAnyTaskByID(state.ActiveTimer.Session.TaskID); findErr != nil {
				issue := fmt.Sprintf("active timer references missing task %d", state.ActiveTimer.Session.TaskID)
				issues = append(issues, issue)
				fixableIssues[issue] = true
				orphanTimer = true
			}
		} else if stateErr != nil {
			issues = append(issues, "global state: "+stateErr.Error())
		}
		if journals, journalErr := a.Store.ListJournals(); journalErr != nil {
			issues = append(issues, "journals: "+journalErr.Error())
		} else {
			for _, journal := range journals {
				refs := append(append([]model.TaskRef{}, journal.Planned...), journal.Tomorrow...)
				for _, session := range journal.Sessions {
					refs = append(refs, model.TaskRef{ID: session.TaskID, Project: session.Project})
				}
				for _, id := range journal.Completed {
					refs = append(refs, model.TaskRef{ID: id})
				}
				for _, ref := range refs {
					if _, _, findErr := a.findAnyTaskByID(ref.ID); findErr != nil {
						issues = append(issues, fmt.Sprintf("journal %s references missing task %d", journal.Date, ref.ID))
					}
				}
			}
		}
		backups := []string{}
		if fix && (len(fixes) > 0 || len(projectFixes) > 0) {
			roots := map[string]bool{}
			for _, item := range fixes {
				if !roots[item.root] {
					backup, backupErr := a.Store.BackupProject(item.root, "doctor")
					if backupErr != nil {
						return backupErr
					}
					backups = append(backups, backup)
					roots[item.root] = true
				}
			}
			for root, project := range projectFixes {
				if !roots[root] {
					backup, backupErr := a.Store.BackupProject(root, "doctor")
					if backupErr != nil {
						return backupErr
					}
					backups = append(backups, backup)
					roots[root] = true
				}
				if err := a.Store.EnsureProject(root, project.Alias); err != nil {
					return err
				}
				config := model.NewProjectConfig(project.Alias)
				if existing, configErr := a.Store.LoadProjectConfig(root); configErr == nil {
					config = existing
				} else if !errors.Is(configErr, os.ErrNotExist) {
					return configErr
				}
				config.Alias = project.Alias
				if err := a.Store.SaveProjectConfig(root, config); err != nil {
					return err
				}
				fixed++
			}
			for _, item := range fixes {
				if _, saveErr := a.Store.SaveTask(item.root, item.task); saveErr != nil {
					return saveErr
				}
				fixed++
			}
		}
		if fix && registryCounterFix {
			backupDir, backupErr := a.Store.BackupPath("doctor")
			if backupErr != nil {
				return backupErr
			}
			if err := copyFile(a.Store.RegistryPath(), filepath.Join(backupDir, "registry.yaml")); err != nil {
				return err
			}
			backups = append(backups, filepath.Join(backupDir, "registry.yaml"))
			reg.NextTaskID = maxTaskID + 1
			if err := a.Store.SaveRegistry(reg); err != nil {
				return err
			}
			fixed++
		}
		if fix && orphanTimer {
			backupDir, backupErr := a.Store.BackupPath("doctor")
			if backupErr != nil {
				return backupErr
			}
			if err := copyFile(a.Store.GlobalStatePath(), filepath.Join(backupDir, "state.yaml")); err != nil {
				return err
			}
			backups = append(backups, filepath.Join(backupDir, "state.yaml"))
			state, stateErr := a.Store.LoadGlobalState()
			if stateErr != nil {
				return stateErr
			}
			state.ActiveTimer = nil
			if err := a.Store.SaveGlobalState(state); err != nil {
				return err
			}
			fixed++
		}
		remainingIssues := issues
		if fix && (len(fixes) > 0 || len(projectFixes) > 0 || registryCounterFix || orphanTimer) {
			remainingIssues = remainingIssues[:0]
			for _, issue := range issues {
				if !fixableIssues[issue] {
					remainingIssues = append(remainingIssues, issue)
				}
			}
		}
		data := map[string]any{"checked_tasks": checked, "issues": remainingIssues, "fixed": fixed, "backups": backups}
		human := fmt.Sprintf("doctor: %d task(s) checked, %d issue(s), %d fixed", checked, len(remainingIssues), fixed)
		if len(remainingIssues) > 0 {
			human += "\n- " + strings.Join(remainingIssues, "\n- ")
		}
		if err := a.output(data, human); err != nil {
			return err
		}
		if len(remainingIssues) > 0 {
			return fmt.Errorf("doctor found %d issue(s)", len(remainingIssues))
		}
		return nil
	}}
	cmd.Flags().BoolVar(&fix, "fix", false, "repair deterministic issues")
	return cmd
}

func (a *App) configCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Manage configuration"}
	cmd.AddCommand(
		&cobra.Command{Use: "get", Short: "Show global configuration", RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			c, err := a.Store.LoadGlobalConfig()
			if err != nil {
				return err
			}
			return a.output(c, fmt.Sprintf("capacity: %d minutes\ntimezone: %s\neditor: %s\nno_color: %t", c.DailyCapacityMinutes, c.Timezone, c.Editor, c.NoColor))
		}},
		&cobra.Command{Use: "set <key> <value>", Short: "Set a global configuration value", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			c, err := a.Store.LoadGlobalConfig()
			if err != nil {
				return err
			}
			switch args[0] {
			case "capacity", "daily_capacity_minutes":
				n, e := strconv.Atoi(args[1])
				if e != nil || n <= 0 {
					return fmt.Errorf("capacity must be positive minutes")
				}
				c.DailyCapacityMinutes = n
			case "editor":
				c.Editor = args[1]
			case "timezone":
				if args[1] != "" && !strings.EqualFold(args[1], "Local") {
					if _, locationErr := time.LoadLocation(args[1]); locationErr != nil {
						return Usage(fmt.Errorf("timezone must be Local or a valid IANA timezone"))
					}
				}
				c.Timezone = args[1]
			case "no_color", "no-color":
				value, parseErr := strconv.ParseBool(args[1])
				if parseErr != nil {
					return Usage(fmt.Errorf("no_color must be true or false"))
				}
				c.NoColor = value
			default:
				return Usage(fmt.Errorf("unknown config key %q", args[0]))
			}
			if err := a.Store.SaveGlobalConfig(c); err != nil {
				return err
			}
			return a.output(c, "configuration updated")
		}},
		&cobra.Command{Use: "edit", Short: "Edit global configuration in $EDITOR", RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			current, err := a.Store.LoadGlobalConfig()
			if err != nil {
				return err
			}
			data, err := yaml.Marshal(current)
			if err != nil {
				return err
			}
			tmp, err := os.CreateTemp(a.Store.GlobalRoot, ".dt-task-config-*.yaml")
			if err != nil {
				return err
			}
			tmpPath := tmp.Name()
			defer os.Remove(tmpPath)
			if err := tmp.Chmod(0o600); err != nil {
				_ = tmp.Close()
				return err
			}
			if _, err := tmp.Write(data); err != nil {
				_ = tmp.Close()
				return err
			}
			if err := tmp.Close(); err != nil {
				return err
			}
			command, err := a.editorCommand(tmpPath)
			if err != nil {
				return err
			}
			command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := command.Run(); err != nil {
				return err
			}
			edited, err := os.ReadFile(tmpPath)
			if err != nil {
				return err
			}
			var next model.GlobalConfig
			if err := yaml.Unmarshal(edited, &next); err != nil {
				return Usage(fmt.Errorf("invalid config; original was preserved: %w", err))
			}
			if next.DailyCapacityMinutes <= 0 {
				return Usage(fmt.Errorf("daily_capacity_minutes must be positive; original was preserved"))
			}
			if next.Timezone != "" && !strings.EqualFold(next.Timezone, "Local") {
				if _, locationErr := time.LoadLocation(next.Timezone); locationErr != nil {
					return Usage(fmt.Errorf("timezone must be Local or a valid IANA timezone; original was preserved"))
				}
			}
			next.SchemaVersion = model.SchemaVersion
			if err := a.Store.SaveGlobalConfig(next); err != nil {
				return err
			}
			a.Log.Info("global config edited")
			return a.output(next, "configuration updated")
		}},
	)
	return cmd
}

func (a *App) skillCommand() *cobra.Command {
	var status bool
	cmd := &cobra.Command{Use: "skill", Short: "Install or inspect the global dt-task agent skill", RunE: func(cmd *cobra.Command, args []string) error {
		if err := skillbundle.Validate(); err != nil {
			return err
		}
		targets, err := skillTargets()
		if err != nil {
			return err
		}
		if status {
			rows := []map[string]any{}
			invalid := false
			for _, target := range targets {
				state := skillState(target)
				if state["state"] != "current" {
					invalid = true
				}
				rows = append(rows, map[string]any{"path": target, "state": state})
			}
			data := any(rows)
			human := formatSkillStatus(rows)
			if invalid {
				data = map[string]any{"targets": rows, "valid": false}
				human += "\nerror: one or more dt-task skill installations are missing, stale, or unsafe"
			}
			if err := a.output(data, human); err != nil {
				return err
			}
			if invalid {
				return fmt.Errorf("one or more dt-task skill installations are missing, stale, or unsafe")
			}
			return nil
		}
		if err := a.Store.EnsureGlobal(); err != nil {
			return err
		}
		return a.installSkill(targets)
	}}
	cmd.Flags().BoolVar(&status, "status", false, "show installation state without changing files")
	return cmd
}

func skillTargets() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	if hasPathTraversal(home) {
		return nil, fmt.Errorf("refusing path traversal in home directory")
	}
	codex := os.Getenv("CODEX_HOME")
	if codex == "" {
		codex = filepath.Join(home, ".codex")
	} else if info, statErr := os.Lstat(codex); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing symlink CODEX_HOME %s", codex)
	}
	if hasPathTraversal(codex) {
		return nil, fmt.Errorf("refusing path traversal in CODEX_HOME")
	}
	codex, err = filepath.Abs(codex)
	if err != nil {
		return nil, err
	}
	first, err := resolveSkillTarget(filepath.Join(codex, "skills", "dt-task"))
	if err != nil {
		return nil, err
	}
	second, err := resolveSkillTarget(filepath.Join(home, ".agents", "skills", "dt-task"))
	if err != nil {
		return nil, err
	}
	return []string{first, second}, nil
}

func hasPathTraversal(path string) bool {
	for _, part := range strings.FieldsFunc(filepath.ToSlash(path), func(r rune) bool { return r == '/' }) {
		if part == ".." {
			return true
		}
	}
	return false
}

func resolveSkillTarget(target string) (string, error) {
	target, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	parent, base := filepath.Dir(target), filepath.Base(target)
	suffix := []string{}
	for {
		if _, statErr := os.Lstat(parent); statErr == nil {
			resolved, resolveErr := filepath.EvalSymlinks(parent)
			if resolveErr != nil {
				return "", resolveErr
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Join(resolved, base), nil
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", fmt.Errorf("cannot resolve skill target parent %s", parent)
		}
		suffix = append(suffix, filepath.Base(parent))
		parent = next
	}
}

func skillState(target string) map[string]any {
	files := []string{"SKILL.md", filepath.Join("agents", "openai.yaml")}
	missing := []string{}
	extras := []string{}
	unsafe := false
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		unsafe = true
	}
	installed := map[string]string{}
	bundled := map[string]string{}
	for _, name := range files {
		data, _ := fs.ReadFile(skillbundle.Files, filepath.Join("dt-task", name))
		sum := sha256.Sum256(data)
		bundled[name] = hex.EncodeToString(sum[:])
	}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(target, name))
		if err != nil {
			missing = append(missing, name)
			continue
		}
		sum := sha256.Sum256(data)
		installed[name] = hex.EncodeToString(sum[:])
	}
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		allowed := map[string]bool{"SKILL.md": true, filepath.Join("agents", "openai.yaml"): true}
		_ = filepath.WalkDir(target, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || path == target {
				return nil
			}
			rel, relErr := filepath.Rel(target, path)
			if relErr == nil && !allowed[rel] {
				extras = append(extras, rel)
			}
			return nil
		})
	}
	state := "current"
	if unsafe {
		state = "unsafe"
	} else if len(missing) > 0 {
		state = "missing"
	} else if len(extras) > 0 {
		state = "stale"
	} else {
		for _, name := range files {
			if installed[name] != bundled[name] {
				state = "stale"
			}
		}
	}
	sort.Strings(extras)
	return map[string]any{"state": state, "files": installed, "bundled": bundled, "missing": missing, "extras": extras}
}

func (a *App) installSkill(targets []string) error {
	lock := filepath.Join(a.Store.GlobalRoot, "locks", "skill.lock")
	return store.WithLock(lock, func() error {
		stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
		backupRoot := filepath.Join(a.Store.BackupRoot(), "skills", stamp)
		type installRecord struct {
			target, backup string
			installed      bool
		}
		records := []installRecord{}
		rollback := func() {
			for i := len(records) - 1; i >= 0; i-- {
				record := records[i]
				if record.installed {
					_ = os.RemoveAll(record.target)
				}
				if record.backup != "" {
					_ = os.Rename(record.backup, record.target)
				}
			}
		}
		for i, target := range targets {
			if err := validateSkillTarget(target); err != nil {
				rollback()
				return err
			}
			state := skillState(target)
			if state["state"] == "current" {
				continue
			}
			record := installRecord{target: target}
			records = append(records, record)
			recordIndex := len(records) - 1
			if _, err := os.Stat(target); err == nil {
				backup := filepath.Join(backupRoot, strconv.Itoa(i))
				if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
					rollback()
					return err
				}
				if err := os.Rename(target, backup); err != nil {
					rollback()
					return fmt.Errorf("backup skill %s: %w", target, err)
				}
				record.backup = backup
				records[recordIndex] = record
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				rollback()
				return err
			}
			stage, err := os.MkdirTemp(filepath.Dir(target), ".dt-task-skill-*")
			if err != nil {
				rollback()
				return err
			}
			if err := copyEmbeddedSkill(stage); err != nil {
				_ = os.RemoveAll(stage)
				rollback()
				return err
			}
			if err := os.Rename(stage, target); err != nil {
				_ = os.RemoveAll(stage)
				rollback()
				return err
			}
			record.installed = true
			records[recordIndex] = record
		}
		if err := a.output(map[string]any{"targets": targets, "installed": true}, "dt-task skill installed globally"); err != nil {
			return err
		}
		return nil
	})
}

func validateSkillTarget(target string) error {
	if target == "" || filepath.IsAbs(target) == false {
		return fmt.Errorf("skill target must be an absolute path")
	}
	if hasPathTraversal(target) {
		return fmt.Errorf("refusing path traversal in skill target")
	}
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink skill target %s", target)
	}
	return nil
}

func copyEmbeddedSkill(dest string) error {
	return fs.WalkDir(skillbundle.Files, "dt-task", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "dt-task" {
			return nil
		}
		rel := strings.TrimPrefix(path, "dt-task/")
		target := filepath.Join(dest, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(skillbundle.Files, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func (a *App) completionCommand() *cobra.Command {
	return &cobra.Command{Use: "completion [bash|zsh]", Short: "Generate shell completion", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		root := cmd.Root()
		switch args[0] {
		case "bash":
			return root.GenBashCompletion(a.Out)
		case "zsh":
			return root.GenZshCompletion(a.Out)
		default:
			return fmt.Errorf("completion supports bash or zsh")
		}
	}}
}
func (a *App) versionCommand() *cobra.Command {
	return &cobra.Command{Use: "version", Short: "Print version", RunE: func(cmd *cobra.Command, args []string) error {
		return a.output(map[string]string{"version": version}, "dt-task "+version)
	}}
}

// findAnyTaskByID deliberately ignores --project. Global timers, journals,
// analytics, and doctor must be able to resolve work from every registered
// project regardless of the caller's current directory or project filter.
func (a *App) findAnyTaskByID(id uint64) (model.Project, model.Task, error) {
	reg, err := a.Store.LoadRegistry()
	if err != nil {
		return model.Project{}, model.Task{}, err
	}
	return a.Store.FindTask(reg, id)
}

func (a *App) prompt(label string) string {
	fmt.Fprint(a.Out, label)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line)
}
func (a *App) promptInt(label string, fallback int) int {
	raw := a.prompt(fmt.Sprintf("%s [%d]: ", label, fallback))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
func parseCSV(raw string) []string {
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" && !contains(out, item) {
			out = append(out, item)
		}
	}
	return out
}

func appendUnique(values []string, extras ...string) []string {
	result := append([]string(nil), values...)
	for _, value := range extras {
		value = strings.TrimSpace(value)
		if value != "" && !contains(result, value) {
			result = append(result, value)
		}
	}
	return result
}

func parseIDs(values []string) ([]uint64, error) {
	ids := make([]uint64, 0, len(values))
	for _, raw := range values {
		for _, item := range strings.Split(raw, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			id, err := strconv.ParseUint(item, 10, 64)
			if err != nil || id == 0 {
				return nil, fmt.Errorf("invalid task id %q", item)
			}
			if !containsID(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	return ids, nil
}
func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func containsID(values []uint64, wanted uint64) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func appendRefUnique(values []model.TaskRef, wanted model.TaskRef) []model.TaskRef {
	for _, value := range values {
		if value.ID == wanted.ID && value.Project == wanted.Project {
			return values
		}
	}
	return append(values, wanted)
}

type fileSnapshot struct {
	path   string
	data   []byte
	mode   os.FileMode
	exists bool
}

func snapshotPaths(paths []string) (map[string]fileSnapshot, error) {
	snapshots := make(map[string]fileSnapshot, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			snapshots[path] = fileSnapshot{path: path}
			continue
		}
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		snapshots[path] = fileSnapshot{path: path, data: data, mode: info.Mode().Perm(), exists: true}
	}
	return snapshots, nil
}

func restoreSnapshots(snapshots map[string]fileSnapshot) error {
	var first error
	for _, snapshot := range snapshots {
		var err error
		if !snapshot.exists {
			err = os.Remove(snapshot.path)
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		} else {
			if err = os.MkdirAll(filepath.Dir(snapshot.path), 0o700); err == nil {
				var tmp *os.File
				tmp, err = os.CreateTemp(filepath.Dir(snapshot.path), ".dt-task-restore-*")
				if err == nil {
					name := tmp.Name()
					if chmodErr := tmp.Chmod(snapshot.mode); chmodErr != nil {
						err = chmodErr
					} else if _, writeErr := tmp.Write(snapshot.data); writeErr != nil {
						err = writeErr
					}
					if closeErr := tmp.Close(); err == nil {
						err = closeErr
					}
					if err == nil {
						_ = os.Remove(snapshot.path)
						err = os.Rename(name, snapshot.path)
					}
					_ = os.Remove(name)
				}
			}
		}
		if err != nil && first == nil {
			first = err
		}
	}
	return first
}

func copyFile(src, dest string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(dest, data, info.Mode().Perm()); err != nil {
		return err
	}
	return nil
}

func journalPaths(s store.Store, journals []model.DayJournal) []string {
	paths := make([]string, 0, len(journals))
	for _, journal := range journals {
		paths = append(paths, s.JournalPath(journal.Date))
	}
	return paths
}

func taskBody(problem, outcome, acceptance, scope, nonGoals, risks, qaNotes string, draft bool) string {
	if draft {
		return "## Problem\n\n_To refine._\n\n## Outcome\n\n_To refine._\n\n## Acceptance criteria\n\n- [ ] _To refine._\n"
	}
	sections := fmt.Sprintf("## Problem\n\n%s\n\n## Outcome\n\n%s\n\n## Acceptance criteria\n\n- [ ] %s\n", strings.TrimSpace(problem), strings.TrimSpace(outcome), strings.TrimSpace(acceptance))
	if strings.TrimSpace(scope) != "" {
		sections += "\n## Scope\n\n" + strings.TrimSpace(scope) + "\n"
	}
	if strings.TrimSpace(nonGoals) != "" {
		sections += "\n## Non-goals\n\n" + strings.TrimSpace(nonGoals) + "\n"
	}
	if strings.TrimSpace(risks) != "" {
		sections += "\n## Risks\n\n" + strings.TrimSpace(risks) + "\n"
	}
	sections += "\n## QA notes\n\n" + strings.TrimSpace(qaNotes) + "\n\n## Work log\n\n"
	return sections
}
func formatProjects(projects []model.Project) string {
	if len(projects) == 0 {
		return "no projects registered"
	}
	var lines []string
	for _, p := range projects {
		lines = append(lines, fmt.Sprintf("%s\t%s", p.Alias, p.Root))
	}
	return strings.Join(lines, "\n")
}
func formatTasks(rows []taskRow) string {
	if len(rows) == 0 {
		return "no tasks found"
	}
	var lines []string
	for _, row := range rows {
		lines = append(lines, fmt.Sprintf("%04d\t%s\t%s\t%s\t%s", row.Task.ID, row.Project, row.Task.Status, row.Task.Priority, row.Task.Title))
	}
	return strings.Join(lines, "\n")
}
func formatTask(alias string, t model.Task) string {
	return fmt.Sprintf("%04d [%s] %s\nproject: %s\npriority: %s\nestimate: %dm\nremaining: %dm\ndue: %s\n\n%s", t.ID, t.Status, t.Title, alias, t.Priority, t.EstimateMinutes, t.RemainingMinutes, valueOr(t.DueDate, "none"), t.Body)
}
func formatDay(date string, j model.DayJournal, rows []taskRow, warnings []string) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("%s — capacity %dm", date, j.Capacity))
	for _, row := range rows {
		lines = append(lines, fmt.Sprintf("%04d\t%s\t%s\t%dm", row.Task.ID, row.Project, row.Task.Title, row.Task.RemainingMinutes))
	}
	for _, warning := range warnings {
		lines = append(lines, "warning: "+warning)
	}
	if len(rows) == 0 {
		lines = append(lines, "no planned tasks")
	}
	return strings.Join(lines, "\n")
}
func formatSkillStatus(rows []map[string]any) string {
	var lines []string
	for _, row := range rows {
		state, _ := row["state"].(map[string]any)
		lines = append(lines, fmt.Sprintf("%s: %s", row["path"], state["state"]))
	}
	return strings.Join(lines, "\n")
}
func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
func formatDateOrEmpty(value time.Time) string {
	if value.IsZero() {
		return "all"
	}
	return value.Format("2006-01-02")
}
