package scan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jackchuka/gh-owned/internal/gate"
	"github.com/jackchuka/gh-owned/internal/github"
	"github.com/jackchuka/gh-owned/internal/owner"
)

type fake struct {
	repos map[string][]github.Repo
}

func (f fake) EnterpriseOrgs(string) ([]string, error) { return []string{"acme", "labs"}, nil }
func (f fake) LoadIdentity([]string) (*github.Identity, error) {
	return github.NewIdentity("alice", map[int64]string{1: "acme/eng", 2: "acme/everyone"}), nil
}
func (f fake) ListRepos(org string) ([]github.Repo, error)   { return f.repos[org], nil }
func (f fake) LoadRules(*github.Repo) error                  { return nil }
func (f fake) OrgTeamNames(string) (map[int64]string, error) { return nil, nil }

func review() *github.Protection {
	return &github.Protection{RequiresApprovingReviews: true, RequiredApprovingReviewCount: 1, IsAdminEnforced: true}
}

func fixture() fake {
	return fake{repos: map[string][]github.Repo{
		"acme": {
			{NameWithOwner: "acme/api", ViewerPermission: "ADMIN", DefaultBranch: "main", Codeowners: "* @acme/eng\n"},
			{NameWithOwner: "acme/web", ViewerPermission: "ADMIN", DefaultBranch: "main", Codeowners: "* @acme/everyone\n"},
			{NameWithOwner: "acme/ops", ViewerPermission: "ADMIN", DefaultBranch: "main", Codeowners: "* @bob\n", Protection: review()},
			{NameWithOwner: "acme/old", ViewerPermission: "ADMIN", DefaultBranch: "main", Codeowners: "* @alice\n", IsArchived: true},
			{NameWithOwner: "acme/misc", ViewerPermission: "ADMIN", DefaultBranch: "main"},
		},
		"labs": {
			{NameWithOwner: "labs/x", ViewerPermission: "WRITE", DefaultBranch: "main", Codeowners: "/x/ @alice\n"},
		},
	}}
}

func roles(r *Report) map[string]owner.Role {
	out := map[string]owner.Role{}
	for _, rep := range r.Repos {
		out[rep.Repo] = rep.Role
	}
	return out
}

func TestRun(t *testing.T) {
	r, err := Run(fixture(), Options{Enterprises: []string{"acme-corp"}, Jobs: 4})
	require.NoError(t, err)
	assert.Equal(t, "alice", r.Viewer)
	assert.Equal(t, 5, r.Scanned, "archived repo skipped")
	assert.Equal(t, map[string]owner.Role{
		"acme/api": owner.RoleOwner,
		"acme/web": owner.RoleOwner,
		"labs/x":   owner.RolePathOwner,
		"acme/ops": owner.RoleApprover,
	}, roles(r))
	assert.Equal(t, "acme/api", r.Repos[0].Repo, "owners first, then by name")
	for _, rep := range r.Repos {
		if rep.Repo == "acme/ops" {
			assert.Equal(t, gate.Approve, rep.Solo)
			assert.Equal(t, []string{"@bob"}, rep.Unenforced)
		}
	}
}

func TestRunOptions(t *testing.T) {
	r, err := Run(fixture(), Options{Orgs: []string{"acme"}, IgnoreTeams: []string{"acme/everyone"}, NoApprover: true, IncludeArchived: true})
	require.NoError(t, err)
	assert.Equal(t, map[string]owner.Role{"acme/api": owner.RoleOwner, "acme/old": owner.RoleOwner}, roles(r))

	r, err = Run(fixture(), Options{Repos: []string{"labs/x"}, AllRepos: true})
	require.NoError(t, err)
	assert.Equal(t, 1, r.Scanned)

	r, err = Run(fixture(), Options{Enterprises: []string{"e"}, ExcludeOrgs: []string{"acme"}})
	require.NoError(t, err)
	assert.Equal(t, 1, r.Scanned)
}

func TestRunPersonalAccount(t *testing.T) {
	f := fake{repos: map[string][]github.Repo{
		"alice": {
			{NameWithOwner: "alice/tool", ViewerPermission: "ADMIN", DefaultBranch: "main"},
			{NameWithOwner: "alice/dots", ViewerPermission: "ADMIN", DefaultBranch: "main", Codeowners: "/x/ @alice\n"},
			{NameWithOwner: "alice/upstream-fork", ViewerPermission: "ADMIN", DefaultBranch: "main", IsFork: true},
		},
	}}
	r, err := Run(f, Options{Orgs: []string{"alice"}})
	require.NoError(t, err)
	assert.Equal(t, map[string]owner.Role{"alice/tool": owner.RoleOwner, "alice/dots": owner.RoleOwner}, roles(r))
	for _, rep := range r.Repos {
		assert.Equal(t, []string{"@alice"}, rep.Via)
		assert.Empty(t, rep.OwnedPaths)
	}
}
