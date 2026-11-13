package gate

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jackchuka/gh-owned/internal/github"
	"github.com/jackchuka/gh-owned/internal/owner"
)

// alice is in acme/web, whose parent team is acme/eng.
var alice = github.NewIdentity("alice", map[int64]string{10: "acme/web", 1: "acme/eng"})

func repo(perm string) github.Repo {
	return github.Repo{NameWithOwner: "acme/app", ViewerPermission: perm, DefaultBranch: "main"}
}

type bp = github.Protection

func protected(p bp) github.Repo {
	r := repo("ADMIN")
	r.Protection = &p
	return r
}

func review(n int) bp {
	return bp{RequiresApprovingReviews: n > 0, RequiredApprovingReviewCount: n, IsAdminEnforced: true}
}

func prRule(id int64, params map[string]any) github.BranchRule {
	raw, _ := json.Marshal(params)
	return github.BranchRule{Type: "pull_request", RulesetID: id, Parameters: raw}
}

func rr(team int64, min int, patterns ...string) map[string]any {
	return map[string]any{"reviewer": map[string]any{"id": team, "type": "Team"}, "file_patterns": patterns, "minimum_approvals": min}
}

func verdict(t *testing.T, r github.Repo, codeowners string, opts Options) Result {
	t.Helper()
	reqs, err := Requirements(r, alice)
	require.NoError(t, err)
	return Evaluate(r, reqs, owner.Parse(codeowners), alice, opts)
}

func TestVerdicts(t *testing.T) {
	withCO := func(p bp) bp { p.RequiresCodeOwnerReviews = true; return p }
	notEnforced := review(1)
	notEnforced.IsAdminEnforced = false
	teamBypass := review(1)
	teamBypass.BypassPullRequestActors = []string{"@acme/web"}
	lastPush := review(1)
	lastPush.RequireLastPushApproval = true
	restricted := review(1)
	restricted.RestrictsPushes = true
	restrictedMine := restricted
	restrictedMine.PushActors = []string{"@acme/eng"}
	restrictedNotEnforced := restricted
	restrictedNotEnforced.IsAdminEnforced = false
	locked := review(0)
	locked.LockBranch = true

	cases := []struct {
		name       string
		repo       github.Repo
		codeowners string
		opts       Options
		want       Verdict
	}{
		{"no protection", repo("ADMIN"), "", Options{}, Open},
		{"read only", repo("READ"), "", Options{}, NoWrite},
		{"admin not enforced", protected(notEnforced), "", Options{}, Bypass},
		{"bypass via team", protected(teamBypass), "", Options{}, Bypass},
		{"one approval", protected(review(1)), "", Options{}, Approve},
		{"author cannot self approve", protected(review(1)), "", Options{AsAuthor: true}, Blocked},
		{"two approvals", protected(review(2)), "", Options{}, Blocked},
		{"code owner via parent team", protected(withCO(review(1))), "* @acme/eng\n", Options{}, Codeowner},
		{"code owner foreign default", protected(withCO(review(1))), "* @acme/sre\n", Options{}, Blocked},
		{"code owner partial", protected(withCO(review(1))), "* @alice\n/infra/ @acme/sre\n", Options{}, Partial},
		{"code owner without file", protected(withCO(review(1))), "", Options{}, Approve},
		{"last catch-all wins", protected(withCO(review(1))), "* @alice\n* @acme/sre\n", Options{}, Blocked},
		{"last push, not pusher", protected(lastPush), "", Options{}, Approve},
		{"last push, pusher", protected(lastPush), "", Options{AsPusher: true}, Blocked},
		{"pusher without last push rule", protected(review(1)), "", Options{AsPusher: true}, Approve},
		{"push restricted", protected(restricted), "", Options{}, Blocked},
		{"push allowed via team", protected(restrictedMine), "", Options{}, Approve},
		{"push restricted, admin not enforced", protected(restrictedNotEnforced), "", Options{}, Bypass},
		{"locked branch", protected(locked), "", Options{}, Blocked},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, verdict(t, c.repo, c.codeowners, c.opts).Verdict)
		})
	}
}

func TestRulesets(t *testing.T) {
	withRules := func(bypass string, rules ...github.BranchRule) github.Repo {
		r := repo("ADMIN")
		r.Rules = rules
		r.Bypass = map[int64]string{7: bypass, 8: bypass}
		return r
	}
	one := map[string]any{"required_approving_review_count": 1}

	cases := []struct {
		name string
		repo github.Repo
		opts Options
		want Verdict
	}{
		{"bypass always", withRules("always", prRule(7, one)), Options{}, Bypass},
		{"never bypass", withRules("never", prRule(7, one)), Options{}, Approve},
		{"non pull_request rules ignored", withRules("never", github.BranchRule{Type: "copilot_code_review", RulesetID: 9}), Options{}, Open},
		{"zero approvals is open", withRules("never", prRule(7, map[string]any{"required_approving_review_count": 0})), Options{}, Open},
		{"last push via ruleset", withRules("never", prRule(7, map[string]any{"required_approving_review_count": 1, "require_last_push_approval": true})), Options{AsPusher: true}, Blocked},
		{"restrict updates", withRules("never", github.BranchRule{Type: "update", RulesetID: 8}), Options{}, Blocked},
		{"restrict updates bypassable", withRules("always", github.BranchRule{Type: "update", RulesetID: 8}), Options{}, Bypass},
		{"required reviewer my team", withRules("never", prRule(7, map[string]any{"required_reviewers": []any{rr(10, 1, "*")}})), Options{}, Codeowner},
		{"required reviewer other team, all files", withRules("never", prRule(7, map[string]any{"required_reviewers": []any{rr(99, 1, "**")}})), Options{}, Blocked},
		{"required reviewer other team, some paths", withRules("never", prRule(7, map[string]any{"required_reviewers": []any{rr(99, 1, "db/**")}})), Options{}, Partial},
		{"required reviewer optional", withRules("never", prRule(7, map[string]any{"required_approving_review_count": 1, "required_reviewers": []any{rr(99, 0, "*")}})), Options{}, Approve},
		{"required reviewer my team needs two", withRules("never", prRule(7, map[string]any{"required_reviewers": []any{rr(10, 2, "*")}})), Options{}, Blocked},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, verdict(t, c.repo, "", c.opts).Verdict)
		})
	}
}

func TestNonAdminRefRule(t *testing.T) {
	r := repo("WRITE")
	r.RefRule = &github.RefRule{RequiredApprovingReviewCount: 1, RequiresCodeOwnerReviews: true, ViewerCanPush: true}
	res := verdict(t, r, "* @acme/sre\n", Options{})
	assert.Equal(t, Blocked, res.Verdict)
	assert.Contains(t, res.Notes[len(res.Notes)-1], "not visible without admin")

	r.RefRule = &github.RefRule{ViewerCanPush: true}
	assert.Equal(t, Open, verdict(t, r, "", Options{}).Verdict)

	r.RefRule = &github.RefRule{RequiredApprovingReviewCount: 1, ViewerCanPush: false}
	assert.Equal(t, Blocked, verdict(t, r, "", Options{}).Verdict)
}

func TestNotes(t *testing.T) {
	r := repo("ADMIN")
	r.Rules = []github.BranchRule{prRule(7, map[string]any{"required_reviewers": []any{rr(99, 1, "db/**")}})}
	alice.AddTeamNames(map[int64]string{99: "@acme/dba"})
	res := verdict(t, r, "", Options{})
	assert.Equal(t, []string{"@acme/dba required on: db/**"}, res.Notes)

	r.Rules = []github.BranchRule{prRule(7, map[string]any{"required_reviewers": []any{rr(42, 1, "*")}})}
	assert.Equal(t, []string{"required reviewer team 42 on all files"}, verdict(t, r, "", Options{}).Notes)

	lp := review(1)
	lp.RequireLastPushApproval = true
	assert.Contains(t, verdict(t, protected(lp), "", Options{}).Notes[0], "do not push to the PR yourself")
}

func TestCopilotExtra(t *testing.T) {
	r := repo("ADMIN")
	r.Rules = []github.BranchRule{prRule(7, map[string]any{"required_approving_review_count": 1, "require_extra_approval_for_unattributed_changes": true})}
	r.Bypass = map[int64]string{7: "never"}
	res := verdict(t, r, "", Options{})
	assert.Equal(t, Approve, res.Verdict)
	assert.True(t, res.CopilotExtra)

	r.Bypass[7] = "always"
	assert.False(t, verdict(t, r, "", Options{}).CopilotExtra)
}

func TestEmpty(t *testing.T) {
	r := repo("ADMIN")
	r.IsEmpty = true
	assert.Equal(t, Empty, verdict(t, r, "", Options{}).Verdict)
}
