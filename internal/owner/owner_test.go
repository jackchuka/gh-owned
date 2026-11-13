package owner

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jackchuka/gh-owned/internal/github"
)

var alice = github.NewIdentity("alice", map[int64]string{10: "acme/web", 1: "acme/eng"})

func rr(team int64, patterns ...string) github.RequiredReviewer {
	r := github.RequiredReviewer{FilePatterns: patterns}
	r.Reviewer.ID = team
	return r
}

func TestEvaluate(t *testing.T) {
	m := Matcher{Identity: alice}
	cases := []struct {
		name       string
		codeowners string
		required   []github.RequiredReviewer
		matcher    Matcher
		want       Result
	}{
		{"catch-all via parent team", "* @acme/eng\n", nil, m, Result{Role: RoleOwner, Via: []string{"@acme/eng"}}},
		{"some paths", "* @acme/sre\n/web/ @acme/web\n", nil, m, Result{Role: RolePathOwner, Paths: []string{"/web/"}, Via: []string{"@acme/web"}}},
		{"not named", "* @acme/sre\n", nil, m, Result{Role: RoleNone}},
		{"no codeowners", "", nil, m, Result{Role: RoleNone}},
		{"required reviewer on all files", "", []github.RequiredReviewer{rr(10, "*")}, m, Result{Role: RoleOwner, Via: []string{"@acme/web"}}},
		{"required reviewer on some paths", "", []github.RequiredReviewer{rr(10, "db/**")}, m, Result{Role: RolePathOwner, Paths: []string{"db/**"}, Via: []string{"@acme/web"}}},
		{"required reviewer other team", "", []github.RequiredReviewer{rr(99, "*")}, m, Result{Role: RoleNone}},
		{
			"ignored team is not ownership",
			"* @acme/eng\n/web/ @alice\n", nil,
			Matcher{Identity: alice, Ignored: map[string]bool{"@acme/eng": true}},
			Result{Role: RolePathOwner, Paths: []string{"/web/"}, Via: []string{"@alice"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, Evaluate(Parse(c.codeowners), c.required, c.matcher))
		})
	}
}

func TestCodeownerCoverage(t *testing.T) {
	cov, foreign := CodeownerCoverage(Parse("* @alice\n/infra/ @acme/sre # ops\n"), alice)
	assert.Equal(t, CoveragePartial, cov)
	assert.Equal(t, []string{"/infra/"}, foreign)

	cov, _ = CodeownerCoverage(Parse("* @acme/eng\n"), alice)
	assert.Equal(t, CoverageFull, cov)

	cov, _ = CodeownerCoverage(Parse("* @acme/sre\n"), alice)
	assert.Equal(t, CoverageForeign, cov)

	cov, _ = CodeownerCoverage(Parse("/docs/ @acme/sre\n"), alice)
	assert.Equal(t, CoveragePartial, cov, "no catch-all: unmatched files have no owner")

	cov, _ = CodeownerCoverage(nil, alice)
	assert.Equal(t, CoverageNone, cov)
}
