package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// ANSI helpers — disabled when noColor is true or stdout is not a tty.

const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiGrey   = "\033[90m"
)

func colorize(s, code string, noColor bool) string {
	if noColor || code == "" {
		return s
	}
	return code + s + ansiReset
}

func gradeColor(grade string, noColor bool) string {
	switch grade {
	case "A":
		return colorize(grade, ansiGreen, noColor)
	case "B":
		return colorize(grade, ansiCyan, noColor)
	case "C":
		return colorize(grade, ansiYellow, noColor)
	case "D":
		return colorize(grade, ansiRed, noColor)
	case "F":
		return colorize(ansiBold+grade, ansiRed, noColor)
	default:
		return grade
	}
}

func ciCell(run *WorkflowRun, noColor bool) string {
	if run == nil {
		return colorize("—", ansiGrey, noColor)
	}
	switch run.Conclusion {
	case "success":
		return colorize("● pass", ansiGreen, noColor)
	case "failure":
		return colorize("● fail", ansiRed, noColor)
	case "cancelled":
		return colorize("● cancel", ansiYellow, noColor)
	case "timed_out":
		return colorize("● timeout", ansiYellow, noColor)
	case "neutral", "skipped":
		return colorize("● "+run.Conclusion, ansiGrey, noColor)
	case "":
		if run.Status == "in_progress" || run.Status == "queued" {
			return colorize("◐ "+run.Status, ansiYellow, noColor)
		}
		return colorize("? "+run.Status, ansiGrey, noColor)
	default:
		return run.Conclusion
	}
}

func releaseCell(rel *Release, noColor bool) string {
	if rel == nil {
		return colorize("—", ansiGrey, noColor)
	}
	age := int(time.Since(rel.PublishedAt).Hours() / 24)
	tag := rel.TagName
	if age > 90 {
		return colorize(tag, ansiYellow, noColor)
	}
	return tag
}

// RenderTable writes an ANSI table to w.
func RenderTable(w io.Writer, owner string, entries []FleetEntry, cutoff time.Time, noColor bool) {
	now := time.Now()
	fmt.Fprintf(w, "%s fleet-pulse — %s %s\n", colorize("▸", ansiCyan, noColor), colorize(owner, ansiBold, noColor), colorize(now.Format("2006-01-02 15:04 UTC"), ansiDim, noColor))
	if len(entries) == 0 {
		fmt.Fprintln(w, colorize("  (no repos found)", ansiGrey, noColor))
		return
	}

	// Summary line.
	var totalDep, failing, stale, noRel int
	for _, e := range entries {
		totalDep += e.DependabotPRs
		if e.LastWorkflowRun != nil && e.LastWorkflowRun.Conclusion == "failure" {
			failing++
		}
		if e.Repo.PushedAt.Before(cutoff) {
			stale++
		}
		if e.LatestRelease == nil {
			noRel++
		}
	}
	avgScore := 0
	for _, e := range entries {
		avgScore += e.Health.Score
	}
	avgScore /= len(entries)
	fmt.Fprintf(w, "  %d repos · avg health %s · %d dependabot · %d CI failing · %d stale · %d no release\n",
		len(entries), gradeColor(gradeForScore(avgScore), noColor), totalDep, failing, stale, noRel)
	fmt.Fprintln(w, strings.Repeat("─", 92))

	// Header.
	hdr := fmt.Sprintf("  %-22s  %6s  %5s  %4s  %4s  %-10s  %-12s  %s",
		"REPO", "PUSHED", "ISS", "PR", "DEPS", "CI", "RELEASE", "HEALTH")
	fmt.Fprintln(w, colorize(hdr, ansiDim, noColor))
	fmt.Fprintln(w, colorize(strings.Repeat("─", 92), ansiDim, noColor))

	// Sort worst health first (actionable top), then stale.
	sorted := make([]FleetEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Health.Score != sorted[j].Health.Score {
			return sorted[i].Health.Score < sorted[j].Health.Score
		}
		return sorted[i].Repo.PushedAt.Before(sorted[j].Repo.PushedAt)
	})

	for _, e := range sorted {
		pushed := e.Repo.PushedAt.Format("2006-01-02")
		pushedCell := pushed
		if e.Repo.PushedAt.Before(cutoff) {
			pushedCell = colorize(pushed, ansiYellow, noColor)
		}
		depsCell := fmt.Sprintf("%d", e.DependabotPRs)
		if e.DependabotPRs > 0 {
			depsCell = colorize(depsCell, ansiYellow, noColor)
		}
		healthCell := gradeColor(e.Health.Grade, noColor) + colorize(fmt.Sprintf(" %d", e.Health.Score), ansiDim, noColor)
		if len(e.Health.Reasons) > 0 {
			healthCell += colorize("  "+strings.Join(e.Health.Reasons, ", "), ansiGrey, noColor)
		}
		name := e.Repo.Name
		if len(name) > 22 {
			name = name[:21] + "…"
		}
		fmt.Fprintf(w, "  %-22s  %6s  %5d  %4d  %4s  %-10s  %-12s  %s\n",
			name, pushedCell, e.OpenIssues, e.OpenPRs, depsCell, ciCell(e.LastWorkflowRun, noColor), releaseCell(e.LatestRelease, noColor), healthCell)
	}
	fmt.Fprintln(w, colorize(strings.Repeat("─", 92), ansiDim, noColor))
	fmt.Fprintf(w, "  %s\n", colorize("health: A 90+  B 75+  C 55+  D 30+  F <30  ·  sort: worst first", ansiGrey, noColor))
}

func gradeForScore(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 75:
		return "B"
	case score >= 55:
		return "C"
	case score >= 30:
		return "D"
	default:
		return "F"
	}
}

// RenderMarkdown writes a markdown report to w.
func RenderMarkdown(w io.Writer, owner string, entries []FleetEntry, cutoff time.Time) {
	fmt.Fprintf(w, "# Fleet pulse — %s\n\n", owner)
	fmt.Fprintf(w, "_Generated %s_\n\n", time.Now().Format("2006-01-02 15:04 UTC"))
	if len(entries) == 0 {
		fmt.Fprintln(w, "No repos found.")
		return
	}
	var totalDep, failing, stale, noRel int
	for _, e := range entries {
		totalDep += e.DependabotPRs
		if e.LastWorkflowRun != nil && e.LastWorkflowRun.Conclusion == "failure" {
			failing++
		}
		if e.Repo.PushedAt.Before(cutoff) {
			stale++
		}
		if e.LatestRelease == nil {
			noRel++
		}
	}
	avgScore := 0
	for _, e := range entries {
		avgScore += e.Health.Score
	}
	avgScore /= len(entries)
	fmt.Fprintf(w, "%d repos · avg health %s (%d) · %d dependabot · %d CI failing · %d stale · %d no release\n\n",
		len(entries), gradeForScore(avgScore), avgScore, totalDep, failing, stale, noRel)

	fmt.Fprintln(w, "| Repo | Pushed | Issues | PRs | Deps | CI | Release | Health | Notes |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|")

	sorted := make([]FleetEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Health.Score != sorted[j].Health.Score {
			return sorted[i].Health.Score < sorted[j].Health.Score
		}
		return sorted[i].Repo.PushedAt.Before(sorted[j].Repo.PushedAt)
	})

	for _, e := range sorted {
		ci := "—"
		if e.LastWorkflowRun != nil {
			if e.LastWorkflowRun.Conclusion != "" {
				ci = e.LastWorkflowRun.Conclusion
			} else {
				ci = e.LastWorkflowRun.Status
			}
		}
		rel := "—"
		if e.LatestRelease != nil {
			rel = e.LatestRelease.TagName
		}
		notes := strings.Join(e.Health.Reasons, ", ")
		if notes == "" {
			notes = "—"
		}
		fmt.Fprintf(w, "| [%s](%s) | %s | %d | %d | %d | %s | %s | %s %d | %s |\n",
			e.Repo.Name, e.Repo.HTMLURL,
			e.Repo.PushedAt.Format("2006-01-02"),
			e.OpenIssues, e.OpenPRs, e.DependabotPRs,
			ci, rel, e.Health.Grade, e.Health.Score, notes)
	}
	fmt.Fprintln(w)
}

// RenderJSON writes the fleet as indented JSON.
func RenderJSON(w io.Writer, owner string, entries []FleetEntry, cutoff time.Time) error {
	type out struct {
		Owner     string       `json:"owner"`
		Generated time.Time    `json:"generated"`
		Entries   []FleetEntry `json:"repos"`
		Summary   struct {
			Total       int `json:"total"`
			AvgHealth   int `json:"avg_health"`
			Dependabot  int `json:"dependabot_prs"`
			CIFailing   int `json:"ci_failing"`
			Stale       int `json:"stale"`
			NoRelease   int `json:"no_release"`
		} `json:"summary"`
	}
	var o out
	o.Owner = owner
	o.Generated = time.Now().UTC()
	o.Entries = entries
	o.Summary.Total = len(entries)
	if len(entries) > 0 {
		sum := 0
		for _, e := range entries {
			sum += e.Health.Score
			o.Summary.Dependabot += e.DependabotPRs
			if e.LastWorkflowRun != nil && e.LastWorkflowRun.Conclusion == "failure" {
				o.Summary.CIFailing++
			}
			if e.Repo.PushedAt.Before(cutoff) {
				o.Summary.Stale++
			}
			if e.LatestRelease == nil {
				o.Summary.NoRelease++
			}
		}
		o.Summary.AvgHealth = sum / len(entries)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(o)
}

// ShouldFail returns true if any --fail-on condition is triggered.
func ShouldFail(entries []FleetEntry, cutoff time.Time, failOn string) (bool, string) {
	if failOn == "" || failOn == "never" {
		return false, ""
	}
	conds := strings.Split(failOn, ",")
	wantAny := false
	want := map[string]bool{}
	for _, c := range conds {
		c = strings.TrimSpace(strings.ToLower(c))
		if c == "any" {
			wantAny = true
		}
		want[c] = true
	}
	check := func(name string) bool {
		return wantAny || want[name] || want["any"]
	}
	for _, e := range entries {
		if check("stale") && e.Repo.PushedAt.Before(cutoff) {
			return true, fmt.Sprintf("stale: %s", e.Repo.Name)
		}
		if check("ci") && e.LastWorkflowRun != nil && e.LastWorkflowRun.Conclusion == "failure" {
			return true, fmt.Sprintf("ci failing: %s", e.Repo.Name)
		}
		if check("dependabot") && e.DependabotPRs > 0 {
			return true, fmt.Sprintf("dependabot: %s (%d)", e.Repo.Name, e.DependabotPRs)
		}
		if check("release-gap") && e.LatestRelease == nil {
			return true, fmt.Sprintf("no release: %s", e.Repo.Name)
		}
		if check("health") && e.Health.Grade == "F" {
			return true, fmt.Sprintf("health F: %s", e.Repo.Name)
		}
	}
	return false, ""
}
