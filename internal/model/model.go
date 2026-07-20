package model

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	SchemaVersion    = 1
	StatusBacklog    = "backlog"
	StatusPlanned    = "planned"
	StatusInProgress = "in-progress"
	StatusBlocked    = "blocked"
	StatusDone       = "done"
)

var validStatuses = map[string]bool{
	StatusBacklog: true, StatusPlanned: true, StatusInProgress: true,
	StatusBlocked: true, StatusDone: true,
}

var validPriorities = map[string]bool{"P0": true, "P1": true, "P2": true, "P3": true}

var aliasPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

type Task struct {
	SchemaVersion    int       `yaml:"schema_version" json:"schema_version"`
	ID               uint64    `yaml:"id" json:"id"`
	Title            string    `yaml:"title" json:"title"`
	Slug             string    `yaml:"slug" json:"slug"`
	Problem          string    `yaml:"problem,omitempty" json:"problem,omitempty"`
	Outcome          string    `yaml:"outcome,omitempty" json:"outcome,omitempty"`
	Acceptance       string    `yaml:"acceptance,omitempty" json:"acceptance,omitempty"`
	Scope            string    `yaml:"scope,omitempty" json:"scope,omitempty"`
	NonGoals         string    `yaml:"non_goals,omitempty" json:"non_goals,omitempty"`
	Risks            string    `yaml:"risks,omitempty" json:"risks,omitempty"`
	QANotes          string    `yaml:"qa_notes,omitempty" json:"qa_notes,omitempty"`
	Status           string    `yaml:"status" json:"status"`
	PreviousStatus   string    `yaml:"previous_status,omitempty" json:"previous_status,omitempty"`
	Priority         string    `yaml:"priority" json:"priority"`
	EstimateMinutes  int       `yaml:"estimate_minutes" json:"estimate_minutes"`
	RemainingMinutes int       `yaml:"remaining_minutes" json:"remaining_minutes"`
	CreatedAt        time.Time `yaml:"created_at" json:"created_at"`
	UpdatedAt        time.Time `yaml:"updated_at" json:"updated_at"`
	DueDate          string    `yaml:"due_date,omitempty" json:"due_date,omitempty"`
	Tags             []string  `yaml:"tags,omitempty" json:"tags,omitempty"`
	Dependencies     []uint64  `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
	Blocker          string    `yaml:"blocker,omitempty" json:"blocker,omitempty"`
	BlockedAt        string    `yaml:"blocked_at,omitempty" json:"blocked_at,omitempty"`
	BlockedMinutes   int       `yaml:"blocked_minutes,omitempty" json:"blocked_minutes,omitempty"`
	Draft            bool      `yaml:"draft,omitempty" json:"draft,omitempty"`
	Carryovers       int       `yaml:"carryovers,omitempty" json:"carryovers,omitempty"`
	CompletedAt      string    `yaml:"completed_at,omitempty" json:"completed_at,omitempty"`
	ArchivedAt       string    `yaml:"archived_at,omitempty" json:"archived_at,omitempty"`
	DeletedAt        string    `yaml:"deleted_at,omitempty" json:"deleted_at,omitempty"`
	LastSessionID    string    `yaml:"last_session_id,omitempty" json:"last_session_id,omitempty"`
	Body             string    `yaml:"-" json:"body,omitempty"`
	Path             string    `yaml:"-" json:"-"`
}

type Project struct {
	SchemaVersion int       `yaml:"schema_version" json:"schema_version"`
	Alias         string    `yaml:"alias" json:"alias"`
	Root          string    `yaml:"root" json:"root"`
	RegisteredAt  time.Time `yaml:"registered_at" json:"registered_at"`
}

type Registry struct {
	SchemaVersion int       `yaml:"schema_version" json:"schema_version"`
	NextTaskID    uint64    `yaml:"next_task_id" json:"next_task_id"`
	Projects      []Project `yaml:"projects,omitempty" json:"projects,omitempty"`
}

type GlobalConfig struct {
	SchemaVersion        int    `yaml:"schema_version" json:"schema_version"`
	DailyCapacityMinutes int    `yaml:"daily_capacity_minutes" json:"daily_capacity_minutes"`
	Editor               string `yaml:"editor,omitempty" json:"editor,omitempty"`
	Timezone             string `yaml:"timezone,omitempty" json:"timezone,omitempty"`
	NoColor              bool   `yaml:"no_color,omitempty" json:"no_color,omitempty"`
}

type ProjectConfig struct {
	SchemaVersion   int      `yaml:"schema_version" json:"schema_version"`
	Alias           string   `yaml:"alias" json:"alias"`
	DefaultPriority string   `yaml:"default_priority" json:"default_priority"`
	DefaultTags     []string `yaml:"default_tags,omitempty" json:"default_tags,omitempty"`
}

type WorkSession struct {
	ID        string    `yaml:"id" json:"id"`
	TaskID    uint64    `yaml:"task_id" json:"task_id"`
	Project   string    `yaml:"project" json:"project"`
	StartedAt time.Time `yaml:"started_at" json:"started_at"`
	StoppedAt time.Time `yaml:"stopped_at,omitempty" json:"stopped_at,omitempty"`
	Minutes   int       `yaml:"minutes,omitempty" json:"minutes,omitempty"`
}

type ActiveTimer struct {
	Session         WorkSession `yaml:"session" json:"session"`
	TaskAdjusted    bool        `yaml:"task_adjusted,omitempty" json:"task_adjusted,omitempty"`
	JournalRecorded bool        `yaml:"journal_recorded,omitempty" json:"journal_recorded,omitempty"`
}

type GlobalState struct {
	SchemaVersion int          `yaml:"schema_version" json:"schema_version"`
	ActiveTimer   *ActiveTimer `yaml:"active_timer,omitempty" json:"active_timer,omitempty"`
}

type DayJournal struct {
	SchemaVersion           int           `yaml:"schema_version" json:"schema_version"`
	Date                    string        `yaml:"date" json:"date"`
	Capacity                int           `yaml:"capacity_minutes" json:"capacity_minutes"`
	Planned                 []TaskRef     `yaml:"planned,omitempty" json:"planned,omitempty"`
	Tomorrow                []TaskRef     `yaml:"tomorrow,omitempty" json:"tomorrow,omitempty"`
	Sessions                []WorkSession `yaml:"sessions,omitempty" json:"sessions,omitempty"`
	Completed               []uint64      `yaml:"completed,omitempty" json:"completed,omitempty"`
	Notes                   []string      `yaml:"notes,omitempty" json:"notes,omitempty"`
	Blockers                []string      `yaml:"blockers,omitempty" json:"blockers,omitempty"`
	EstimateVarianceMinutes int           `yaml:"estimate_variance_minutes,omitempty" json:"estimate_variance_minutes,omitempty"`
	OpenedAt                time.Time     `yaml:"opened_at,omitempty" json:"opened_at,omitempty"`
	ClosedAt                time.Time     `yaml:"closed_at,omitempty" json:"closed_at,omitempty"`
	Body                    string        `yaml:"-" json:"body,omitempty"`
}

type TaskRef struct {
	ID      uint64 `yaml:"id" json:"id"`
	Project string `yaml:"project" json:"project"`
}

func NewRegistry() Registry { return Registry{SchemaVersion: SchemaVersion, NextTaskID: 1} }

func NewGlobalConfig() GlobalConfig {
	return GlobalConfig{SchemaVersion: SchemaVersion, DailyCapacityMinutes: 360, Timezone: "Local"}
}

func NewGlobalState() GlobalState { return GlobalState{SchemaVersion: SchemaVersion} }

func NewProjectConfig(alias string) ProjectConfig {
	return ProjectConfig{SchemaVersion: SchemaVersion, Alias: alias, DefaultPriority: "P2"}
}

func NewDayJournal(date string, capacity int) DayJournal {
	return DayJournal{SchemaVersion: SchemaVersion, Date: date, Capacity: capacity}
}

func ValidateStatus(s string) error {
	if !validStatuses[s] {
		return fmt.Errorf("invalid status %q (use backlog, planned, in-progress, blocked, or done)", s)
	}
	return nil
}

func ValidateTransition(from, to string) error {
	if err := ValidateStatus(from); err != nil {
		return err
	}
	if err := ValidateStatus(to); err != nil {
		return err
	}
	if from == to {
		return nil
	}
	allowed := map[string]map[string]bool{
		StatusBacklog:    {StatusPlanned: true, StatusInProgress: true, StatusBlocked: true},
		StatusPlanned:    {StatusInProgress: true, StatusBlocked: true},
		StatusInProgress: {StatusDone: true, StatusBlocked: true},
		StatusBlocked:    {StatusBacklog: true, StatusPlanned: true, StatusInProgress: true},
		StatusDone:       {StatusInProgress: true},
	}
	if !allowed[from][to] {
		return fmt.Errorf("invalid status transition %s -> %s", from, to)
	}
	return nil
}

func ValidatePriority(p string) error {
	if !validPriorities[p] {
		return fmt.Errorf("invalid priority %q (use P0, P1, P2, or P3)", p)
	}
	return nil
}

func ValidateAlias(alias string) error {
	if !aliasPattern.MatchString(alias) {
		return fmt.Errorf("project alias must start with a letter or number and contain only letters, numbers, _ or -")
	}
	return nil
}

func (t *Task) Validate() error {
	if t.ID == 0 {
		return fmt.Errorf("task id must be positive")
	}
	if t.SchemaVersion > SchemaVersion {
		return fmt.Errorf("unsupported task schema_version %d", t.SchemaVersion)
	}
	if strings.TrimSpace(t.Title) == "" {
		return fmt.Errorf("task title is required")
	}
	if err := ValidateStatus(t.Status); err != nil {
		return err
	}
	if t.Status == StatusBlocked && strings.TrimSpace(t.Blocker) == "" {
		return fmt.Errorf("blocked tasks require blocker")
	}
	if err := ValidatePriority(t.Priority); err != nil {
		return err
	}
	if t.EstimateMinutes <= 0 {
		return fmt.Errorf("estimate_minutes must be positive")
	}
	if t.RemainingMinutes < 0 {
		return fmt.Errorf("remaining_minutes cannot be negative")
	}
	if t.BlockedMinutes < 0 || t.Carryovers < 0 {
		return fmt.Errorf("task counters cannot be negative")
	}
	if t.DueDate != "" {
		if _, err := time.Parse("2006-01-02", t.DueDate); err != nil {
			return fmt.Errorf("due_date must be YYYY-MM-DD: %w", err)
		}
	}
	seen := map[uint64]bool{}
	for _, dep := range t.Dependencies {
		if dep == 0 || dep == t.ID {
			return fmt.Errorf("invalid dependency %d", dep)
		}
		if seen[dep] {
			return fmt.Errorf("duplicate dependency %d", dep)
		}
		seen[dep] = true
	}
	return nil
}

func (t Task) Filename() string {
	return fmt.Sprintf("%04d.[%s].%s.md", t.ID, t.Status, SafeSlug(t.Title))
}

func SafeSlug(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "task"
	}
	if len(s) > seventyTwo {
		s = strings.Trim(s[:seventyTwo], "-")
	}
	return s
}

const seventyTwo = 72

func (r Registry) FindProject(alias string) (Project, bool) {
	for _, p := range r.Projects {
		if p.Alias == alias {
			return p, true
		}
	}
	return Project{}, false
}

func (r Registry) ProjectForRoot(root string) (Project, bool) {
	root, _ = filepath.Abs(root)
	for _, p := range r.Projects {
		candidate, _ := filepath.Abs(p.Root)
		if candidate == root {
			return p, true
		}
	}
	return Project{}, false
}

func (r *Registry) AddProject(p Project) error {
	if err := ValidateAlias(p.Alias); err != nil {
		return err
	}
	if _, ok := r.FindProject(p.Alias); ok {
		return fmt.Errorf("project alias %q already exists", p.Alias)
	}
	if _, ok := r.ProjectForRoot(p.Root); ok {
		return fmt.Errorf("project is already registered")
	}
	r.Projects = append(r.Projects, p)
	sort.Slice(r.Projects, func(i, j int) bool { return r.Projects[i].Alias < r.Projects[j].Alias })
	return nil
}

func (r *Registry) AllocateID() uint64 {
	if r.NextTaskID == 0 {
		r.NextTaskID = 1
	}
	id := r.NextTaskID
	r.NextTaskID++
	return id
}

func (d *DayJournal) EnsureTask(ref TaskRef) {
	for _, item := range d.Planned {
		if item.ID == ref.ID && item.Project == ref.Project {
			return
		}
	}
	d.Planned = append(d.Planned, ref)
}

func (d *DayJournal) EnsureTomorrow(ref TaskRef) {
	for _, item := range d.Tomorrow {
		if item.ID == ref.ID && item.Project == ref.Project {
			return
		}
	}
	d.Tomorrow = append(d.Tomorrow, ref)
}
