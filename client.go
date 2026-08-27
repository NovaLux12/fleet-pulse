package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Repo is the subset of GitHub's repo object we care about.
type Repo struct {
	Name        string    `json:"name"`
	FullName    string    `json:"full_name"`
	Description string    `json:"description"`
	Private     bool      `json:"private"`
	Archived    bool      `json:"archived"`
	PushedAt    time.Time `json:"pushed_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	HTMLURL     string    `json:"html_url"`
	Stargazers  int       `json:"stargazers_count"`
	OpenIssues  int       `json:"open_issues_count"`
}

// Release is the latest non-draft release for a repo.
type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
}

// WorkflowRun is the subset we need to detect CI health.
type WorkflowRun struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	HeadBranch string    `json:"head_branch"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	HTMLURL    string    `json:"html_url"`
}

// FleetEntry aggregates every signal for one repo.
type FleetEntry struct {
	Repo             Repo         `json:"repo"`
	OpenIssues       int          `json:"open_issues"`
	OpenPRs          int          `json:"open_prs"`
	DependabotPRs    int          `json:"dependabot_prs"`
	LatestRelease    *Release     `json:"latest_release"`
	LatestReleaseErr string       `json:"latest_release_err,omitempty"`
	LastWorkflowRun  *WorkflowRun `json:"last_workflow_run"`
	WorkflowErr      string       `json:"workflow_err,omitempty"`
	Health           HealthScore  `json:"health"`
}

// HealthScore summarises a FleetEntry into a single grade.
type HealthScore struct {
	Score   int      `json:"score"` // 0-100
	Grade   string   `json:"grade"` // A/B/C/D/F
	Reasons []string `json:"reasons,omitempty"`
}

// Client wraps http.Client with auth and base URL.
type Client struct {
	HTTP   *http.Client
	Token  string
	APIURL string
}

// NewClient returns a Client configured for the GitHub REST API.
func NewClient(token string) *Client {
	return &Client{
		HTTP:   &http.Client{Timeout: 30 * time.Second},
		Token:  token,
		APIURL: "https://api.github.com",
	}
}

func (c *Client) get(path string, out any, query url.Values) error {
	u := c.APIURL + path
	if query != nil && len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("User-Agent", "fleet-pulse/0.1")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return &NotFoundError{Path: path}
	}
	if resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0" {
		return fmt.Errorf("rate limited — set GH_TOKEN for higher quota")
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GET %s: %d %s: %s", path, resp.StatusCode, resp.Status, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// NotFoundError is returned by Client.get for 404 responses.
type NotFoundError struct{ Path string }

func (e *NotFoundError) Error() string { return "not found: " + e.Path }

// IsNotFound reports whether err is or wraps a NotFoundError.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(*NotFoundError)
	return ok
}

// ListRepos returns up to maxRepos repos for owner, sorted by pushed.
func (c *Client) ListRepos(owner string, maxRepos int) ([]Repo, error) {
	var all []Repo
	page := 1
	for len(all) < maxRepos {
		var batch []Repo
		q := url.Values{}
		q.Set("per_page", "100")
		q.Set("page", fmt.Sprintf("%d", page))
		q.Set("type", "owner")
		q.Set("sort", "pushed")
		if err := c.get("/users/"+owner+"/repos", &batch, q); err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		all = append(all, batch...)
		page++
		if page > 5 {
			break
		}
	}
	if len(all) > maxRepos {
		all = all[:maxRepos]
	}
	return all, nil
}

// CountOpenIssuesAndPRs returns (issues, prs) and counts dependabot PRs separately.
// Uses /repos/{owner}/{repo}/pulls for PRs and /repos/.../issues for issues.
func (c *Client) CountOpenIssuesAndPRs(owner, repo string) (issues, prs, dependabotPRs int, err error) {
	// Fetch open pulls to get accurate PR count + dependabot filter.
	var pulls []struct {
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		PullRequest *json.RawMessage `json:"pull_request"`
	}
	q := url.Values{}
	q.Set("state", "open")
	q.Set("per_page", "100")
	if err = c.get(fmt.Sprintf("/repos/%s/%s/pulls", owner, repo), &pulls, q); err != nil {
		return 0, 0, 0, err
	}
	prs = len(pulls)
	for _, p := range pulls {
		if p.User.Login == "dependabot[bot]" || p.User.Login == "dependabot-preview[bot]" {
			dependabotPRs++
		}
	}
	// Open issues count from repo object includes PRs; subtract prs.
	// But we already have prs, so fetch issues and filter out PRs for issue count.
	var items []json.RawMessage
	q2 := url.Values{}
	q2.Set("state", "open")
	q2.Set("per_page", "100")
	if err = c.get(fmt.Sprintf("/repos/%s/%s/issues", owner, repo), &items, q2); err != nil {
		return 0, 0, 0, err
	}
	for _, raw := range items {
		var probe struct {
			PullRequest *json.RawMessage `json:"pull_request"`
		}
		if err2 := json.Unmarshal(raw, &probe); err2 != nil {
			continue
		}
		if probe.PullRequest == nil {
			issues++
		}
	}
	return issues, prs, dependabotPRs, nil
}

// LatestRelease returns the most recent non-draft release or nil.
func (c *Client) LatestRelease(owner, repo string) (*Release, error) {
	var r Release
	err := c.get(fmt.Sprintf("/repos/%s/%s/releases/latest", owner, repo), &r, nil)
	if err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// LatestWorkflowRun returns the most recent workflow run across all branches, or nil.
func (c *Client) LatestWorkflowRun(owner, repo string) (*WorkflowRun, error) {
	var resp struct {
		Runs []WorkflowRun `json:"workflow_runs"`
	}
	q := url.Values{}
	q.Set("per_page", "1")
	err := c.get(fmt.Sprintf("/repos/%s/%s/actions/runs", owner, repo), &resp, q)
	if err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(resp.Runs) == 0 {
		return nil, nil
	}
	return &resp.Runs[0], nil
}

// ComputeHealth grades a FleetEntry based on stale, CI, dependabot, release gap.
func ComputeHealth(e FleetEntry, cutoff time.Time, staleDays int) HealthScore {
	score := 100
	var reasons []string

	if e.Repo.PushedAt.Before(cutoff) {
		days := int(time.Since(e.Repo.PushedAt).Hours() / 24)
		penalty := min(30, days-staleDays+10)
		if penalty < 10 {
			penalty = 10
		}
		score -= penalty
		reasons = append(reasons, fmt.Sprintf("stale %dd since push", days))
	}
	if e.LastWorkflowRun != nil && e.LastWorkflowRun.Conclusion == "failure" {
		score -= 25
		reasons = append(reasons, "CI failing")
	} else if e.LastWorkflowRun != nil && e.LastWorkflowRun.Conclusion == "timed_out" {
		score -= 20
		reasons = append(reasons, "CI timed out")
	}
	if e.DependabotPRs > 0 {
		penalty := min(20, e.DependabotPRs*5)
		score -= penalty
		reasons = append(reasons, fmt.Sprintf("%d dependabot PR(s) pending", e.DependabotPRs))
	}
	if e.LatestRelease == nil {
		score -= 10
		reasons = append(reasons, "no releases")
	} else {
		age := int(time.Since(e.LatestRelease.PublishedAt).Hours() / 24)
		if age > 90 {
			penalty := min(20, (age-90)/10+5)
			score -= penalty
			reasons = append(reasons, fmt.Sprintf("release %dd old (%s)", age, e.LatestRelease.TagName))
		}
	}
	if score < 0 {
		score = 0
	}
	grade := "A"
	switch {
	case score >= 90:
		grade = "A"
	case score >= 75:
		grade = "B"
	case score >= 55:
		grade = "C"
	case score >= 30:
		grade = "D"
	default:
		grade = "F"
	}
	return HealthScore{Score: score, Grade: grade, Reasons: reasons}
}
