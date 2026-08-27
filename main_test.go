package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestHealthGrade(t *testing.T) {
	cases := []struct {
		score int
		grade string
	}{
		{100, "A"}, {90, "A"}, {89, "B"}, {75, "B"}, {74, "C"}, {55, "C"}, {54, "D"}, {30, "D"}, {29, "F"}, {0, "F"},
	}
	for _, c := range cases {
		if got := gradeForScore(c.score); got != c.grade {
			t.Errorf("gradeForScore(%d)=%q want %q", c.score, got, c.grade)
		}
	}
}

func TestComputeHealthClean(t *testing.T) {
	now := time.Now()
	e := FleetEntry{
		Repo:            Repo{PushedAt: now, Name: "fresh"},
		LatestRelease:   &Release{TagName: "v0.1.0", PublishedAt: now},
		LastWorkflowRun: &WorkflowRun{Conclusion: "success"},
	}
	h := ComputeHealth(e, now.AddDate(0, 0, -30), 30)
	if h.Grade != "A" || h.Score < 90 {
		t.Fatalf("clean repo should be A, got %s %d %v", h.Grade, h.Score, h.Reasons)
	}
	if len(h.Reasons) != 0 {
		t.Errorf("clean repo should have no reasons, got %v", h.Reasons)
	}
}

func TestComputeHealthStaleAndCIFailing(t *testing.T) {
	now := time.Now()
	e := FleetEntry{
		Repo:            Repo{PushedAt: now.AddDate(0, 0, -60), Name: "stale"},
		LatestRelease:   &Release{TagName: "v0.1.0", PublishedAt: now},
		LastWorkflowRun: &WorkflowRun{Conclusion: "failure"},
		DependabotPRs:   3,
	}
	h := ComputeHealth(e, now.AddDate(0, 0, -30), 30)
	if h.Score >= 70 {
		t.Fatalf("stale+CI+deps should drag score low, got %d %v", h.Score, h.Reasons)
	}
	// Should mention all three.
	joined := strings.Join(h.Reasons, " ")
	for _, want := range []string{"stale", "CI failing", "dependabot"} {
		if !strings.Contains(strings.ToLower(joined), strings.ToLower(want)) {
			t.Errorf("reasons %v should contain %q", h.Reasons, want)
		}
	}
}

func TestComputeHealthNoRelease(t *testing.T) {
	now := time.Now()
	e := FleetEntry{
		Repo: Repo{PushedAt: now, Name: "norelease"},
	}
	h := ComputeHealth(e, now.AddDate(0, 0, -30), 30)
	found := false
	for _, r := range h.Reasons {
		if strings.Contains(r, "no releases") {
			found = true
		}
	}
	if !found {
		t.Errorf("should flag no releases, got %v", h.Reasons)
	}
}

func TestShouldFail(t *testing.T) {
	now := time.Now()
	cutoff := now.AddDate(0, 0, -30)
	entries := []FleetEntry{
		{Repo: Repo{Name: "a", PushedAt: now.AddDate(0, 0, -60)}},
		{Repo: Repo{Name: "b", PushedAt: now}, LastWorkflowRun: &WorkflowRun{Conclusion: "failure"}},
		{Repo: Repo{Name: "c", PushedAt: now}, DependabotPRs: 2},
	}
	if ok, _ := ShouldFail(entries, cutoff, ""); ok {
		t.Error("empty fail-on should not trigger")
	}
	if ok, _ := ShouldFail(entries, cutoff, "never"); ok {
		t.Error("never should not trigger")
	}
	if ok, reason := ShouldFail(entries, cutoff, "stale"); !ok || !strings.Contains(reason, "a") {
		t.Errorf("stale should trigger on a, got %v %q", ok, reason)
	}
	if ok, _ := ShouldFail(entries, cutoff, "ci"); !ok {
		t.Error("ci should trigger on b")
	}
	if ok, _ := ShouldFail(entries, cutoff, "dependabot"); !ok {
		t.Error("dependabot should trigger on c")
	}
	if ok, _ := ShouldFail(entries, cutoff, "any"); !ok {
		t.Error("any should trigger")
	}
	// health F: make one entry F
	fEntries := []FleetEntry{
		{Repo: Repo{Name: "f", PushedAt: now.AddDate(0, 0, -200)}, Health: HealthScore{Score: 10, Grade: "F"}},
	}
	if ok, _ := ShouldFail(fEntries, cutoff, "health"); !ok {
		t.Error("health should trigger on F grade")
	}
	// No match
	clean := []FleetEntry{{Repo: Repo{Name: "ok", PushedAt: now}, Health: HealthScore{Score: 95, Grade: "A"}}}
	if ok, _ := ShouldFail(clean, cutoff, "ci"); ok {
		t.Error("ci should not trigger on clean entry with no workflow run")
	}
}

func TestRenderJSONValid(t *testing.T) {
	now := time.Now()
	entries := []FleetEntry{
		{Repo: Repo{Name: "x", PushedAt: now, HTMLURL: "https://github.com/o/x"}, Health: HealthScore{Score: 90, Grade: "A"}},
	}
	var buf bytes.Buffer
	if err := RenderJSON(&buf, "o", entries, now.AddDate(0, 0, -30)); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if out["owner"] != "o" {
		t.Errorf("owner %v", out["owner"])
	}
}

func TestRenderTableNoPanic(t *testing.T) {
	now := time.Now()
	entries := []FleetEntry{
		{Repo: Repo{Name: "alpha", PushedAt: now.AddDate(0, 0, -40), HTMLURL: "https://github.com/o/alpha"}, OpenIssues: 1, OpenPRs: 2, DependabotPRs: 1, Health: HealthScore{Score: 40, Grade: "D", Reasons: []string{"stale 40d since push"}}, LastWorkflowRun: &WorkflowRun{Conclusion: "failure"}, LatestRelease: &Release{TagName: "v0.2.0", PublishedAt: now.AddDate(0, 0, -100)}},
		{Repo: Repo{Name: "beta", PushedAt: now, HTMLURL: "https://github.com/o/beta"}, Health: HealthScore{Score: 95, Grade: "A"}, LastWorkflowRun: &WorkflowRun{Conclusion: "success"}, LatestRelease: &Release{TagName: "v0.3.0", PublishedAt: now}},
	}
	var buf bytes.Buffer
	RenderTable(&buf, "o", entries, now.AddDate(0, 0, -30), true)
	s := buf.String()
	if !strings.Contains(s, "alpha") || !strings.Contains(s, "beta") {
		t.Errorf("table should contain both repos, got:\n%s", s)
	}
}

func TestRenderMarkdownContainsHeader(t *testing.T) {
	now := time.Now()
	entries := []FleetEntry{
		{Repo: Repo{Name: "m", PushedAt: now, HTMLURL: "https://github.com/o/m"}, Health: HealthScore{Score: 80, Grade: "B"}},
	}
	var buf bytes.Buffer
	RenderMarkdown(&buf, "myorg", entries, now.AddDate(0, 0, -30))
	s := buf.String()
	if !strings.Contains(s, "# Fleet pulse") || !strings.Contains(s, "myorg") {
		t.Errorf("markdown header missing, got:\n%s", s)
	}
	if !strings.Contains(s, "| Repo |") {
		t.Errorf("markdown table header missing, got:\n%s", s)
	}
}

func TestNotFoundHelper(t *testing.T) {
	if IsNotFound(nil) {
		t.Error("nil should not be not-found")
	}
	if !IsNotFound(&NotFoundError{Path: "/x"}) {
		t.Error("NotFoundError should be not-found")
	}
}

func TestColorizeNoColor(t *testing.T) {
	if got := colorize("hi", ansiRed, true); got != "hi" {
		t.Errorf("noColor should suppress ANSI, got %q", got)
	}
	if got := colorize("hi", ansiRed, false); !strings.Contains(got, "hi") || !strings.Contains(got, "\033[") {
		t.Errorf("color should wrap, got %q", got)
	}
}

func TestJSONSchemaValid(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal([]byte(JSONSchema), &m); err != nil {
		t.Fatalf("JSONSchema invalid JSON: %v", err)
	}
	if m["title"] != "fleet-pulse" {
		t.Errorf("title %v", m["title"])
	}
}
