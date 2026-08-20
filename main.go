// Command fleet-pulse — unified fleet health pulse.
//
// Aggregates stale signals, Dependabot backlog, workflow failures and
// release gaps into a single terminal dashboard for a GitHub owner/org.
//
// Usage:
//
//	fleet-pulse --owner NovaLux12 [--format table|json|markdown] [--stale-days 30] [--fail-on ci,stale]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

const version = "0.1.0"

const JSONSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "fleet-pulse",
  "type": "object",
  "properties": {
    "owner":     { "type": "string", "description": "GitHub user or org inspected" },
    "generated": { "type": "string", "format": "date-time" },
    "repos": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "repo": {
            "type": "object",
            "properties": {
              "name":             { "type": "string" },
              "full_name":        { "type": "string" },
              "description":     { "type": "string" },
              "private":         { "type": "boolean" },
              "archived":        { "type": "boolean" },
              "pushed_at":       { "type": "string", "format": "date-time" },
              "html_url":        { "type": "string", "format": "uri" },
              "stargazers_count": { "type": "integer" }
            }
          },
          "open_issues":     { "type": "integer" },
          "open_prs":        { "type": "integer" },
          "dependabot_prs":  { "type": "integer" },
          "latest_release":  { "type": ["object", "null"] },
          "last_workflow_run": { "type": ["object", "null"] },
          "health": {
            "type": "object",
            "properties": {
              "score":   { "type": "integer", "minimum": 0, "maximum": 100 },
              "grade":   { "type": "string", "enum": ["A","B","C","D","F"] },
              "reasons": { "type": "array", "items": { "type": "string" } }
            }
          }
        }
      }
    },
    "summary": {
      "type": "object",
      "properties": {
        "total":         { "type": "integer" },
        "avg_health":    { "type": "integer" },
        "dependabot_prs": { "type": "integer" },
        "ci_failing":    { "type": "integer" },
        "stale":         { "type": "integer" },
        "no_release":    { "type": "integer" }
      }
    }
  },
  "required": ["owner", "generated", "repos"]
}
`

func main() {
	var (
		owner      = flag.String("owner", "", "GitHub user or org (required)")
		format     = flag.String("format", "table", "Output format: table, json, markdown")
		staleDays  = flag.Int("stale-days", 30, "Days before a repo is flagged stale")
		maxRepos   = flag.Int("max-repos", 100, "Max repos to inspect")
		failOn     = flag.String("fail-on", "", "Comma-separated: stale,ci,dependabot,release-gap,health,any (exit 1 if matched)")
		noColor    = flag.Bool("no-color", false, "Disable ANSI colour")
		includeArc = flag.Bool("include-archived", false, "Include archived repos")
		showVer    = flag.Bool("version", false, "Print version and exit")
		showSchema = flag.Bool("json-schema", false, "Print JSON Schema for --format json and exit")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "fleet-pulse v%s — unified fleet health pulse\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage: %s --owner <name> [flags]\n\nFlags:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr, "\nExamples:")
		fmt.Fprintln(os.Stderr, "  fleet-pulse --owner NovaLux12")
		fmt.Fprintln(os.Stderr, "  fleet-pulse --owner NovaLux12 --format json | jq .summary")
		fmt.Fprintln(os.Stderr, "  fleet-pulse --owner NovaLux12 --fail-on ci,stale --stale-days 14")
		fmt.Fprintln(os.Stderr, "  fleet-pulse --owner NovaLux12 --format markdown >> HEARTBEAT.md")
		fmt.Fprintln(os.Stderr, "\nAuth: set GH_TOKEN or GITHUB_TOKEN for higher rate limits (5000/h vs 60/h unauthenticated).")
	}
	flag.Parse()

	if *showVer {
		fmt.Printf("fleet-pulse v%s\n", version)
		return
	}
	if *showSchema {
		fmt.Print(JSONSchema)
		return
	}
	if *owner == "" {
		flag.Usage()
		os.Exit(2)
	}
	*format = strings.ToLower(strings.TrimSpace(*format))
	switch *format {
	case "table", "json", "markdown":
	default:
		fmt.Fprintf(os.Stderr, "error: --format must be table, json, or markdown (got %q)\n", *format)
		os.Exit(2)
	}
	if *staleDays < 1 {
		fmt.Fprintln(os.Stderr, "error: --stale-days must be >= 1")
		os.Exit(2)
	}

	token := tokenFromEnv()
	client := NewClient(token)

	repos, err := client.ListRepos(*owner, *maxRepos)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error listing repos for %s: %v\n", *owner, err)
		os.Exit(1)
	}

	cutoff := time.Now().AddDate(0, 0, -*staleDays)
	var entries []FleetEntry
	for _, r := range repos {
		if r.Archived && !*includeArc {
			continue
		}
		e := FleetEntry{Repo: r}
		issues, prs, deps, err := client.CountOpenIssuesAndPRs(*owner, r.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: %s count: %v\n", r.Name, err)
		} else {
			e.OpenIssues = issues
			e.OpenPRs = prs
			e.DependabotPRs = deps
		}
		rel, err := client.LatestRelease(*owner, r.Name)
		if err != nil && !IsNotFound(err) {
			e.LatestReleaseErr = err.Error()
		} else {
			e.LatestRelease = rel
		}
		run, err := client.LatestWorkflowRun(*owner, r.Name)
		if err != nil && !IsNotFound(err) {
			e.WorkflowErr = err.Error()
		} else {
			e.LastWorkflowRun = run
		}
		e.Health = ComputeHealth(e, cutoff, *staleDays)
		entries = append(entries, e)
	}

	switch *format {
	case "json":
		// Use RenderJSON for consistent envelope.
		if err := RenderJSON(os.Stdout, *owner, entries, cutoff); err != nil {
			fmt.Fprintf(os.Stderr, "error rendering json: %v\n", err)
			os.Exit(1)
		}
		// Also support raw entries dump when piped — but envelope is the contract.
		_ = json.RawMessage{}
	case "markdown":
		RenderMarkdown(os.Stdout, *owner, entries, cutoff)
	default:
		RenderTable(os.Stdout, *owner, entries, cutoff, *noColor)
	}

	if triggered, reason := ShouldFail(entries, cutoff, *failOn); triggered {
		fmt.Fprintf(os.Stderr, "fail-on triggered: %s\n", reason)
		os.Exit(1)
	}
}

func tokenFromEnv() string {
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t
	}
	return os.Getenv("GITHUB_TOKEN")
}
