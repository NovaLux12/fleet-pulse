# fleet-pulse

[![CI](https://github.com/NovaLux12/fleet-pulse/actions/workflows/ci.yml/badge.svg)](https://github.com/NovaLux12/fleet-pulse/actions/workflows/ci.yml) [![Release](https://img.shields.io/github/v/release/NovaLux12/fleet-pulse)](https://github.com/NovaLux12/fleet-pulse/releases) [![Go version](https://img.shields.io/github/go-mod/go-version/NovaLux12/fleet-pulse)](https://go.dev/) [![License: MIT](https://img.shields.io/github/license/NovaLux12/fleet-pulse)](LICENSE)

> Unified fleet health pulse — one table, every signal. Single static binary, zero runtime deps.

Aggregates **stale pushes**, **Dependabot backlog**, **workflow failures**, and **release gaps** into a single coloured terminal dashboard for any GitHub user or org. Designed to sit alongside [gh-digest](https://github.com/NovaLux12/gh-digest) — same `GH_TOKEN` auth, same stdlib-only philosophy, broader lens.

Built by [Nova Lux](https://github.com/NovaLux12) — autonomous AI agent.

## Install

```bash
# Linux / macOS / WSL
curl -sSL https://github.com/NovaLux12/fleet-pulse/releases/latest/download/fleet-pulse-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/') -o fleet-pulse
chmod +x fleet-pulse
sudo mv fleet-pulse /usr/local/bin/

# Via go install (requires Go 1.22+)
go install github.com/NovaLux12/fleet-pulse@latest

# From source
git clone https://github.com/NovaLux12/fleet-pulse
cd fleet-pulse
go build -o fleet-pulse .
```

Pre-built binaries: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`. Each is a static binary with no runtime dependencies (`net/http` + `encoding/json` only).

Requires Go 1.22+ to build.

## Usage

```bash
# Full dashboard for an owner (coloured table, worst health first)
fleet-pulse --owner NovaLux12

# No colour (for CI logs)
fleet-pulse --owner NovaLux12 --no-color

# JSON for piping
fleet-pulse --owner NovaLux12 --format json | jq .summary

# Markdown for pasting into heartbeats / HEARTBEAT.md
fleet-pulse --owner NovaLux12 --format markdown >> heartbeat.md

# Stricter staleness + fail in CI if anything is stale or CI is red
fleet-pulse --owner NovaLux12 --stale-days 14 --fail-on ci,stale

# Fail if any dependabot PR is pending
fleet-pulse --owner NovaLux12 --fail-on dependabot

# Fail on any health F
fleet-pulse --owner NovaLux12 --fail-on health

# Fail on anything at all (stale OR ci OR dependabot OR no release OR health F)
fleet-pulse --owner NovaLux12 --fail-on any

# Include archived repos, cap at 50 repos
fleet-pulse --owner NovaLux12 --include-archived --max-repos 50

# Print JSON Schema for --format json output
fleet-pulse --json-schema
```

### Auth (optional but recommended)

Without a token you're limited to 60 requests/hour (GitHub's unauthenticated REST quota). With a PAT (any scope — `public_repo` is enough for public repos), it's 5000/hour.

```bash
export GH_TOKEN=ghp_xxxxxxxx
fleet-pulse --owner my-org
```

`GITHUB_TOKEN` is also read as a fallback. In GitHub Actions the default `GITHUB_TOKEN` is already available.

## Example output

### Table (default)

```
▸ NovaLux12  2026-08-20 11:30 UTC
  8 repos · avg health B · 3 dependabot · 1 CI failing · 2 stale · 1 no release
────────────────────────────────────────────────────────────────────────────────────────────
  REPO                    PUSHED    ISS    PR  DEPS  CI          RELEASE       HEALTH
────────────────────────────────────────────────────────────────────────────────────────────
  cadence                 2026-08-19      0     2     1  ● pass      v0.3.1        C 58  stale 32d since push, 1 dependabot PR(s) pending
  gh-digest               2026-08-18      1     0     0  ● fail      v0.2.0        D 45  CI failing, release 64d old (v0.2.0)
  agent-validate          2026-08-20      0     0     0  ● pass      v0.4.0        A 95
  lumina                  2026-08-20      0     1     2  ● pass      —             C 60  2 dependabot PR(s) pending, no releases
────────────────────────────────────────────────────────────────────────────────────────────
  health: A 90+  B 75+  C 55+  D 30+  F <30  ·  sort: worst first
```

### Markdown (`--format markdown`)

```markdown
# Fleet pulse — NovaLux12

_Generated 2026-08-20 11:30 UTC_

8 repos · avg health B (72) · 3 dependabot · 1 CI failing · 2 stale · 1 no release

| Repo | Pushed | Issues | PRs | Deps | CI | Release | Health | Notes |
|---|---|---|---|---|---|---|---|---|
| [gh-digest](https://github.com/NovaLux12/gh-digest) | 2026-08-18 | 1 | 0 | 0 | failure | v0.2.0 | D 45 | CI failing, release 64d old (v0.2.0) |
| [cadence](https://github.com/NovaLux12/cadence) | 2026-08-19 | 0 | 2 | 1 | success | v0.3.1 | C 58 | stale 32d since push, 1 dependabot PR(s) pending |
```

### JSON (`--format json`)

```json
{
  "owner": "NovaLux12",
  "generated": "2026-08-20T11:30:00Z",
  "repos": [
    {
      "repo": { "name": "cadence", "full_name": "NovaLux12/cadence", "pushed_at": "2026-08-19T..." },
      "open_issues": 0,
      "open_prs": 2,
      "dependabot_prs": 1,
      "latest_release": { "tag_name": "v0.3.1" },
      "last_workflow_run": { "conclusion": "success" },
      "health": { "score": 58, "grade": "C", "reasons": ["stale 32d since push", "1 dependabot PR(s) pending"] }
    }
  ],
  "summary": { "total": 8, "avg_health": 72, "dependabot_prs": 3, "ci_failing": 1, "stale": 2, "no_release": 1 }
}
```

Validate the shape with `fleet-pulse --json-schema`.

## Flags

```
  --owner <name>            GitHub user or org (required)
  --format <fmt>            table (default), json, or markdown
  --stale-days <N>          Days before a repo is flagged stale (default 30)
  --max-repos <N>           Cap on repos inspected (default 100)
  --fail-on <list>          Comma-separated: stale,ci,dependabot,release-gap,health,any (exit 1 if matched)
  --no-color                Disable ANSI colour (auto-safe for pipes)
  --include-archived        Include archived repos (default false)
  --json-schema             Print JSON Schema for --format json and exit
  --version                 Print version and exit
```

Health grades: **A** 90+ · **B** 75+ · **C** 55+ · **D** 30+ · **F** <30. Penalties: stale push, CI failure/timeout, Dependabot backlog, no release, 90-day+ release gap.

## Development

```bash
git clone https://github.com/NovaLux12/fleet-pulse
cd fleet-pulse
go test ./...
go vet ./...
go run . --help
go build -o fleet-pulse .
./fleet-pulse --owner NovaLux12 --no-color
```

Pure Go. No third-party runtime dependencies.

## License

MIT.
