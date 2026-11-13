---
name: gh-owned
license: MIT
description: >
  Find the GitHub repositories the user is responsible for (CODEOWNERS and
  ruleset required reviewers, not admin access), whether they can approve and
  merge there alone, and the open pull requests there that need them.
  Use when the user asks which repos they own or maintain, what PRs they are on
  the hook for beyond their review requests, whether their approval alone can
  merge somewhere, or wants to scope bulk work such as dependency PRs to repos
  they are responsible for. Triggers: "my repos", "repos I own",
  "PRs waiting on me", "can I merge this alone", "自分が管理するリポジトリ",
  "見なきゃいけない PR", "/gh-owned".
argument-hint: "[--enterprise SLUG | --org ORG | --repo OWNER/NAME]..."
compatibility: Requires gh CLI authenticated with repo and read:org scopes, and the gh-owned extension (gh extension install jackchuka/gh-owned)
allowed-tools: Bash(gh owned:*) Bash(gh api:*) Bash(gh auth status:*) Bash(gh extension list:*) Bash(jq:*)
metadata:
  author: jackchuka
  scope: generic
  layer: workflow
  confirms:
    - install the gh-owned extension
    - write gh-dash config
---

# gh-owned

`gh owned` does the deterministic work: roles, `via` teams, and the solo-merge
verdict per repo. This skill adds the judgment: which broad teams to ignore,
which PRs need the user, and what to hand to other skills.

Run `gh owned --help` for flags and the meaning of every role and verdict.

## 0. Setup

- `gh extension list | grep gh-owned`. If missing, ask before running
  `gh extension install jackchuka/gh-owned`.
- `gh auth status` must list `read:org`. If not, tell the user to run
  `gh auth refresh -s read:org` themselves (it is interactive).

## 1. Scan

```bash
gh owned <targets> --format json > /tmp/owned.json
```

Run it with `--format json`: through a pipe, `table` prints bare tab-separated
rows without the summary.

Targets come from the user: `--enterprise`, `--org`, or `--repo`. Do not guess
an org. Ask before scanning a large sandbox or playground org; offer
`--exclude-org`. If `--enterprise` fails, the user likely cannot read the
enterprise; retry with `--org`.

By default the PR is assumed to be opened by someone else. Add `--as-author`
when the user wrote the PR, or `--as-pusher` when they pushed its latest commit.

## 2. Decide which teams to ignore

Count repos per handle in `.repos[] | select(.role == "owner" or .role == "path-owner") | .via[]`.
Company-wide or department-wide teams near the top make the user
"responsible" for repos they never look at. Ask the user which teams
to ignore, recommending the broadest ones, then rerun with `--ignore-team` for
each. Remember the choice for the rest of the session.

## 3. Find the PRs that need the user

```bash
gh owned <targets> --ignore-team ... --format gh-dash > /tmp/owned.yml
```

Each `filters:` value is a search query in the legacy syntax, where repeated
`repo:` qualifiers are ORed. Run each one with the REST search API, not
`gh search prs` (its advanced syntax ANDs them):

```bash
gh api -X GET search/issues -f q="is:pr <filters>" -f per_page=100 \
  --jq '.items[] | [.repository_url, .number, .title, .user.login, .updated_at] | @tsv'
```

## 4. Report

Lead with counts per role, then:

1. PRs in **Review requested** and **Owned: needs review**, oldest first. Mark
   PRs not updated in 90 days as stale candidates to close.
2. **Owned: approved** PRs: ready to merge if the repo's `solo` allows.
3. Bot PRs (`dependabot`, `renovate`) grouped by repo, offered to a
   dependency-PR triage skill with the repo list if one is installed.
4. **Supplementary** PRs last. Say that nobody named the user there: approving
   overrides the named owners in practice.

Mention repos whose `solo` is `blocked` or `partial` when the user plans to
merge there, with the note explaining why.

Caveats to state when they apply:

- Only the default branch is evaluated; status checks and merge queues are not.
- Notes saying `not visible without admin` mean branch protection was only
  partly readable: a bypass the user has may be missing (verdict too strict),
  and a last-push rule may be missing (verdict too lenient with `--as-pusher`).
  Say so rather than presenting the verdict as certain.

## 5. Optional: install the gh-dash sections

If the user uses gh-dash, offer to merge the generated `prSections` into
`~/.config/gh-dash/config.yml`. Confirm before writing; keep their existing
sections.
