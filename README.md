<div align="center">

# gh-owned

**Find the repos you are responsible for, and whether you can merge there alone.**

A `gh` extension that reads CODEOWNERS, branch protection, and rulesets across<br>
an enterprise, orgs, or repos, and tells you where you are on the hook.

[![Test](https://github.com/jackchuka/gh-owned/actions/workflows/test.yml/badge.svg)](https://github.com/jackchuka/gh-owned/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/v/release/jackchuka/gh-owned?sort=semver)](https://github.com/jackchuka/gh-owned/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/jackchuka/gh-owned)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

<img src="assets/demo.gif" alt="gh owned listing an org's repos with roles and solo-merge verdicts, ignoring a company-wide team, and writing gh-dash sections" width="800">

[Installation](#installation) · [Quick Start](#quick-start) · [Output](#output) · [gh-dash](#gh-dash) · [Agent skill](#agent-skill) · [Limits](#limits)

</div>

---

Admin access says what you _can_ touch, not what you _own_. An org owner is
admin on every repo. The answer to "what am I responsible for?" lives in
CODEOWNERS files and ruleset required-reviewer rules, spread across every
repository, and whether your approval alone can merge a PR depends on branch
protection, rulesets, bypass lists, and push restrictions on top of that.

**gh-owned** resolves all of it from your point of view, ancestor teams
included, and prints one line per repo: your role and your solo-merge verdict.
It can also write [gh-dash](https://github.com/dlvhdr/gh-dash) sections, so the
PR inbox for those repos is one keypress away.

## Features

- **Responsibility, not access** — roles come from CODEOWNERS and ruleset required reviewers, never from admin rights
- **Team-aware** — your teams and their ancestor teams count; drop broad teams with `--ignore-team`
- **Solo-merge verdict** — classic branch protection and rulesets (org and enterprise level included), your bypass status, code owner review, required-reviewer teams, last-push approval, push restrictions, locked branches, and Copilot's extra-approval rule
- **Unenforced owners** — flags repos where CODEOWNERS names someone else but any writer's approval merges
- **Works without admin** — reads what a member can see and says what it could not
- **gh-dash output** — PR sections for the repos you own, chunked per org
- **Scriptable** — `--format json`

## Installation

```bash
gh extension install jackchuka/gh-owned
```

Requires the [GitHub CLI](https://cli.github.com/) authenticated with the `repo` and `read:org` scopes:

```bash
gh auth refresh -s read:org
```

### From source

```bash
git clone https://github.com/jackchuka/gh-owned.git
cd gh-owned
go build -o gh-owned .
gh extension install .
```

## Quick Start

```bash
gh owned --org acme
```

```text
alice  8 repos scanned · 5 owner · 1 path owner · 1 approver
via  @acme/everyone 2 · @acme/platform 2 · @acme/engineering 1 · @alice 1

Owner (5) you own the default
  acme/api       ✓ codeowner  @acme/platform
  acme/cli       ✓ bypass     @acme/platform     you can bypass the review rules
  acme/handbook  ✓ approve    @acme/everyone
  acme/sdk       ✓ codeowner  @acme/engineering  latest push needs another approver: do not push to the PR yourself
  acme/web       ~ partial    @acme/everyone     CODEOWNERS owned by others: /billing/

Path owner (1) you own some paths
  acme/infra     ✗ blocked    @alice             paths: /modules/dns/ · CODEOWNERS default owner is someone else

Approver (1) nobody named you; any writer's approval merges
  acme/tools     ✓ approve                       CODEOWNERS not enforced: @bob · Copilot-opened PRs need one more approval
```

The `via` line counts repos per handle. A company-wide team near the top is
usually noise; ignore it and run again:

```bash
gh owned --org acme --ignore-team @acme/everyone
```

More examples:

```bash
gh owned --enterprise acme-corp --exclude-org acme-sandbox   # every org you belong to
gh owned --repo acme/api --repo acme/web                     # specific repos
gh owned --org alice                                         # your personal account
gh owned --org acme --as-pusher                              # you pushed a fix to the PR
gh owned --org acme --format json | jq '.repos[] | select(.solo == "blocked")'
```

## Output

### Role

| Role | Meaning |
|---|---|
| `owner` | You or one of your teams owns the default (`*` in CODEOWNERS), or is a required reviewer on all files |
| `path-owner` | You own some paths; they are listed |
| `approver` | Nobody named you, but a review is required and any writer's approval satisfies it |

Repos under your own user account count as `owner` even without CODEOWNERS (forks excluded).
Repos with no role are hidden unless you pass `--all-repos`.

### Solo-merge verdict

Whether you can approve and merge a PR into the default branch without anyone
else. By default the PR is assumed to be opened by someone else (a person or a
dependency bot), so your approval counts.

| `solo` | Alone? | Meaning |
|---|---|---|
| `open` | Yes | No review rule |
| `bypass` | Yes | Rules exist, but you can bypass every one |
| `approve` | Yes | One approval is required and yours counts |
| `codeowner` | Yes | Code owner or required-reviewer review is required and you own everything |
| `partial` | Depends | Some paths belong to others; the note lists them |
| `blocked` | No | Push restriction, locked branch, "Restrict updates", 2+ approvals, someone else's code owner or required reviewer, `--as-author`, or last-push approval with `--as-pusher` |
| `no-write` | No | Read or triage permission only |

### Piped output

When stdout is not a terminal, `table` prints tab-separated rows for scripts,
one per repo: role, repo, solo, via (comma-separated), notes. Use
`--format json` for the full report.

### Flags

| Flag | Description |
|---|---|
| `--enterprise SLUG` | Scan every org you belong to in the enterprise |
| `--org ORG` | Scan an org or user account (repeatable) |
| `--repo OWNER/NAME` | Scan a repo (repeatable) |
| `--exclude-org ORG` | Skip an org |
| `--ignore-team @org/team` | Ownership through this team does not count as yours (repeatable) |
| `--as-author` | Judge as the PR author: your approval does not count |
| `--as-pusher` | Judge as the latest pusher: last-push approval needs someone else |
| `--include-archived` | Include archived repos |
| `--no-approver` | Do not report `approver` repos |
| `--all-repos` | Also list repos with no role |
| `--format table\|json\|gh-dash` | Output format (default `table`) |
| `--jobs N` | Concurrent repo lookups (default 16) |

## gh-dash

```bash
gh owned --org acme --ignore-team @acme/everyone --format gh-dash > owned.yml
```

This writes `prSections` for [gh-dash](https://github.com/dlvhdr/gh-dash):

- **Review requested** — `review-requested:@me`, including team requests
- **Owned: needs review** — PRs waiting for review in repos you own
- **Owned: approved** — approved PRs in repos you own, ready to merge
- **Supplementary: any writer can approve** — PRs waiting for review in `approver` repos

Sections are split per org and chunked at 25 repos to keep search queries
short. Merge them into `~/.config/gh-dash/config.yml`.

## Agent skill

A skill in [`skills/gh-owned`](skills/gh-owned/SKILL.md) teaches coding agents
(Claude Code, Codex, Gemini CLI, and others that follow the
[Agent Skills](https://agentskills.io) spec) to run the scan, pick teams to
ignore, and list the open PRs that need you.

```bash
gh skill install jackchuka/gh-owned gh-owned
# or
npx skills add jackchuka/gh-owned --skill gh-owned
```

Then ask something like "which repos am I responsible for in acme?" or
"what PRs are waiting on me?".

## Limits

- Only the default branch is evaluated.
- Required status checks, merge queues, and required deployments are not
  checked: they wait for CI, not for a person.
- Without admin access, classic branch protection hides admin enforcement,
  bypass lists, and last-push approval. Affected notes say "not visible without
  admin". Rulesets, CODEOWNERS, and teams are fully visible.
- Copilot's extra-approval rule applies per PR; it is reported as a note.
- If `--enterprise` fails because you cannot read the enterprise, pass `--org`.

## Related

- [postura](https://github.com/jackchuka/postura) audits enterprise, org, and repo settings against a security baseline you own. Use postura to check that protection is set up as the org intends; use gh-owned to see where you personally are responsible and whether your approval alone can merge.

## License

[MIT](LICENSE)
