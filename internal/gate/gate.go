// Package gate decides whether the viewer can approve and merge a pull request
// into a repository's default branch without anyone else.
package gate

import (
	"fmt"
	"strings"

	"github.com/hmarr/codeowners"

	"github.com/jackchuka/gh-owned/internal/github"
	"github.com/jackchuka/gh-owned/internal/owner"
)

// Verdict is the solo-merge outcome for a repo.
type Verdict string

const (
	Open      Verdict = "open"      // no review rule
	Bypass    Verdict = "bypass"    // rules exist, the viewer can bypass all of them
	Approve   Verdict = "approve"   // one approval required, the viewer's counts
	Codeowner Verdict = "codeowner" // code owner / required reviewer review, the viewer owns everything
	Partial   Verdict = "partial"   // some paths need someone else
	Blocked   Verdict = "blocked"   // someone else must act
	NoWrite   Verdict = "no-write"
	Empty     Verdict = "empty"
	Error     Verdict = "error"
)

var writePerms = map[string]bool{"ADMIN": true, "MAINTAIN": true, "WRITE": true}

var bypassing = map[string]bool{"always": true, "pull_requests_only": true, "exempt": true}

// Options describe who the viewer is relative to the pull request.
type Options struct {
	AsAuthor bool // the viewer opened the PR, so their approval does not count
	AsPusher bool // the viewer pushed the latest commit
}

// Requirement is one source of merge requirements (classic protection or a ruleset).
type Requirement struct {
	Source            string
	Bypassable        bool
	Approvals         int
	CodeOwner         bool
	RequiredReviewers []github.RequiredReviewer
	LastPush          bool // the latest pusher's approval does not count
	CopilotExtra      bool // PRs Copilot opened alone need one more approval
	PushBlocked       bool // the viewer may not update the branch
	Hidden            bool // read from refUpdateRule: bypass and last-push are not visible
}

func (r Requirement) reviews() bool {
	return r.Approvals > 0 || r.CodeOwner || len(r.RequiredReviewers) > 0
}

// Requirements collects the merge requirements on the repo's default branch.
func Requirements(repo github.Repo, id *github.Identity) ([]Requirement, error) {
	var reqs []Requirement
	switch {
	case repo.Protection != nil:
		bp := repo.Protection
		adminBypass := repo.ViewerPermission == "ADMIN" && !bp.IsAdminEnforced
		approvals := 0
		if bp.RequiresApprovingReviews {
			approvals = bp.RequiredApprovingReviewCount
		}
		reqs = append(reqs,
			Requirement{
				Source:     "branch-protection",
				Bypassable: adminBypass || anyIs(id, bp.BypassPullRequestActors),
				Approvals:  approvals,
				CodeOwner:  bp.RequiresCodeOwnerReviews,
				LastPush:   bp.RequireLastPushApproval,
			},
			Requirement{
				Source:      "branch-protection",
				Bypassable:  adminBypass,
				PushBlocked: bp.LockBranch || (bp.RestrictsPushes && !anyIs(id, bp.PushActors)),
			},
		)
	case repo.RefRule != nil:
		reqs = append(reqs, Requirement{
			Source:      "branch-protection",
			Approvals:   repo.RefRule.RequiredApprovingReviewCount,
			CodeOwner:   repo.RefRule.RequiresCodeOwnerReviews,
			PushBlocked: !repo.RefRule.ViewerCanPush,
			Hidden:      true,
		})
	}

	for _, rule := range repo.Rules {
		src := fmt.Sprintf("ruleset:%d", rule.RulesetID)
		bypassable := bypassing[repo.Bypass[rule.RulesetID]]
		switch rule.Type {
		case "update":
			reqs = append(reqs, Requirement{Source: src, Bypassable: bypassable, PushBlocked: true})
		case "pull_request":
			p, err := rule.PullRequest()
			if err != nil {
				return nil, fmt.Errorf("%s pull_request parameters: %w", src, err)
			}
			reqs = append(reqs, Requirement{
				Source:            src,
				Bypassable:        bypassable,
				Approvals:         p.RequiredApprovingReviewCount,
				CodeOwner:         p.RequireCodeOwnerReview,
				RequiredReviewers: p.RequiredReviewers,
				LastPush:          p.RequireLastPushApproval,
				CopilotExtra:      p.RequireExtraApprovalForUnattributedChgs,
			})
		}
	}
	return reqs, nil
}

// Result is the solo-merge verdict with human-readable notes.
type Result struct {
	Verdict      Verdict
	Notes        []string
	CopilotExtra bool // Copilot-opened PRs need one more approval than the verdict implies
}

// Evaluate decides the verdict. rs is the parsed CODEOWNERS of the default branch.
func Evaluate(repo github.Repo, reqs []Requirement, rs codeowners.Ruleset, id *github.Identity, opts Options) Result {
	res := evaluate(repo, reqs, rs, id, opts)
	for _, r := range reqs {
		if r.CopilotExtra && r.Approvals > 0 && !r.Bypassable {
			res.CopilotExtra = true
		}
	}
	if res.Verdict == Approve || res.Verdict == Codeowner || res.Verdict == Partial || res.Verdict == Blocked {
		for _, r := range reqs {
			if r.Hidden {
				res.Notes = append(res.Notes, "branch protection bypass and last-push rule not visible without admin")
				break
			}
		}
	}
	return res
}

func evaluate(repo github.Repo, reqs []Requirement, rs codeowners.Ruleset, id *github.Identity, opts Options) Result {
	if !writePerms[repo.ViewerPermission] {
		return Result{Verdict: NoWrite, Notes: []string{strings.ToLower(repo.ViewerPermission)}}
	}
	if repo.IsEmpty || repo.DefaultBranch == "" {
		return Result{Verdict: Empty}
	}
	for _, r := range reqs {
		if r.PushBlocked && !r.Bypassable {
			return blocked(fmt.Sprintf("you may not push to %s, so you may not merge into it", repo.DefaultBranch))
		}
	}

	var gating []Requirement
	active := false
	for _, r := range reqs {
		active = active || r.reviews() || r.PushBlocked
		if !r.Bypassable && r.reviews() {
			gating = append(gating, r)
		}
	}
	if len(gating) == 0 {
		if active {
			return Result{Verdict: Bypass, Notes: []string{"you can bypass the review rules"}}
		}
		return Result{Verdict: Open}
	}

	approvals, lastPush := 0, false
	for _, r := range gating {
		approvals = max(approvals, r.Approvals)
		lastPush = lastPush || r.LastPush
	}
	switch {
	case opts.AsAuthor:
		return blocked("you are the author and an approval is required")
	case opts.AsPusher && lastPush:
		return blocked("you pushed last and the latest push must be approved by someone else")
	case approvals > 1:
		return blocked(fmt.Sprintf("%d approvals required", approvals))
	}

	res := Result{Verdict: Approve}
	if lastPush {
		res.Notes = append(res.Notes, "latest push needs another approver: do not push to the PR yourself")
	}

	codeOwner := false
	for _, r := range gating {
		codeOwner = codeOwner || r.CodeOwner
	}
	if codeOwner {
		cov, foreign := owner.CodeownerCoverage(rs, id)
		switch cov {
		case owner.CoverageForeign:
			return blocked("CODEOWNERS default owner is someone else")
		case owner.CoverageFull:
			res.Verdict = Codeowner
		case owner.CoveragePartial:
			res.Verdict = Partial
			res.Notes = append(res.Notes, "CODEOWNERS owned by others: "+Abbreviate(foreign, 3))
		}
	}

	for _, r := range gating {
		for _, rr := range r.RequiredReviewers {
			need := rr.Minimum()
			if need == 0 {
				continue
			}
			name := id.TeamName(rr.Reviewer.ID)
			mine := id.TeamIDs[rr.Reviewer.ID]
			if mine && need == 1 {
				if res.Verdict == Approve {
					res.Verdict = Codeowner
				}
				continue
			}
			why := name
			if mine {
				why = fmt.Sprintf("%d approvals from %s", need, name)
			}
			patterns := rr.FilePatterns
			if len(patterns) == 0 || anyCatchall(patterns) {
				return blocked(fmt.Sprintf("required reviewer %s on all files", why))
			}
			res.Verdict = Partial
			res.Notes = append(res.Notes, fmt.Sprintf("%s required on: %s", why, Abbreviate(patterns, 3)))
		}
	}
	return res
}

func blocked(note string) Result {
	return Result{Verdict: Blocked, Notes: []string{note}}
}

func anyIs(id *github.Identity, handles []string) bool {
	for _, h := range handles {
		if id.Is(h) {
			return true
		}
	}
	return false
}

func anyCatchall(patterns []string) bool {
	for _, p := range patterns {
		if owner.IsCatchall(p) {
			return true
		}
	}
	return false
}

// Abbreviate joins the first keep items and counts the rest. It never hides a
// single item, since "(+1 more)" takes about as much room as the item itself.
func Abbreviate(items []string, keep int) string {
	if len(items) <= keep+1 {
		return strings.Join(items, " ")
	}
	return fmt.Sprintf("%s (+%d more)", strings.Join(items[:keep], " "), len(items)-keep)
}
