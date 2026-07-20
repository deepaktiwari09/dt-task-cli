package model

import (
	"strings"
	"testing"
)

func TestSafeSlugAndFilename(t *testing.T) {
	task := Task{ID: 12, Title: "Implement Login / OAuth", Status: StatusBacklog, Priority: "P2", EstimateMinutes: 20, RemainingMinutes: 20}
	if got := task.Filename(); got != "0012.[backlog].implement-login-oauth.md" {
		t.Fatalf("filename = %q", got)
	}
	if strings.Contains(SafeSlug("A title with punctuation!"), " ") {
		t.Fatal("slug contains whitespace")
	}
}

func TestTaskValidation(t *testing.T) {
	task := Task{ID: 1, Title: "Task", Status: StatusBacklog, Priority: "P2", EstimateMinutes: 30, RemainingMinutes: 30}
	if err := task.Validate(); err != nil {
		t.Fatal(err)
	}
	task.Status = "unknown"
	if err := task.Validate(); err == nil {
		t.Fatal("expected invalid status")
	}
}

func TestRegistryAllocation(t *testing.T) {
	r := NewRegistry()
	if got := r.AllocateID(); got != 1 {
		t.Fatalf("first id = %d", got)
	}
	if got := r.AllocateID(); got != 2 {
		t.Fatalf("second id = %d", got)
	}
}

func TestStatusTransitions(t *testing.T) {
	allowed := [][2]string{{StatusBacklog, StatusPlanned}, {StatusPlanned, StatusInProgress}, {StatusInProgress, StatusBlocked}, {StatusBlocked, StatusPlanned}, {StatusDone, StatusInProgress}}
	for _, pair := range allowed {
		if err := ValidateTransition(pair[0], pair[1]); err != nil {
			t.Fatalf("%s -> %s: %v", pair[0], pair[1], err)
		}
	}
	if err := ValidateTransition(StatusBacklog, StatusDone); err == nil {
		t.Fatal("expected illegal backlog -> done transition")
	}
}

func TestBlockedResumeUsesPriorStatus(t *testing.T) {
	if err := ValidateTransition(StatusBlocked, StatusPlanned); err != nil {
		t.Fatal(err)
	}
}

func TestAliasValidation(t *testing.T) {
	if err := ValidateAlias("web-app_1"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAlias("../unsafe"); err == nil {
		t.Fatal("expected unsafe alias rejection")
	}
}
