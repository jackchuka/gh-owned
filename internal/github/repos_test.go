package github

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const adminNode = `{
  "nameWithOwner": "acme/app", "isArchived": false, "isEmpty": false, "viewerPermission": "ADMIN",
  "defaultBranchRef": {
    "name": "main",
    "branchProtectionRule": {
      "requiresApprovingReviews": true, "requiredApprovingReviewCount": 1, "requiresCodeOwnerReviews": true,
      "isAdminEnforced": true, "requireLastPushApproval": false, "restrictsPushes": true, "lockBranch": false,
      "bypassPullRequestAllowances": {"nodes": [{"actor": {"__typename": "User", "login": "alice"}}]},
      "pushAllowances": {"nodes": [{"actor": {"__typename": "Team", "combinedSlug": "acme/web"}}, {"actor": null}]}
    },
    "refUpdateRule": {"requiredApprovingReviewCount": 1, "requiresCodeOwnerReviews": true, "viewerCanPush": true}
  },
  "co1": null, "co2": {"text": "* @acme/web\n"}, "co3": null
}`

const readerNode = `{
  "nameWithOwner": "acme/lib", "viewerPermission": "WRITE",
  "defaultBranchRef": {"name": "trunk", "branchProtectionRule": null,
    "refUpdateRule": {"requiredApprovingReviewCount": null, "requiresCodeOwnerReviews": false, "viewerCanPush": false}}
}`

func decode(t *testing.T, s string) Repo {
	t.Helper()
	var n repoNode
	require.NoError(t, json.Unmarshal([]byte(s), &n))
	return n.toRepo()
}

func TestToRepoAdmin(t *testing.T) {
	r := decode(t, adminNode)
	assert.Equal(t, "main", r.DefaultBranch)
	assert.Equal(t, "acme", r.Owner())
	assert.True(t, r.HasCodeowners)
	assert.Equal(t, "* @acme/web\n", r.Codeowners)
	require.NotNil(t, r.Protection)
	assert.Equal(t, []string{"@alice"}, r.Protection.BypassPullRequestActors)
	assert.Equal(t, []string{"@acme/web"}, r.Protection.PushActors)
	assert.True(t, r.Protection.RestrictsPushes)
}

func TestToRepoReader(t *testing.T) {
	r := decode(t, readerNode)
	assert.Nil(t, r.Protection)
	require.NotNil(t, r.RefRule)
	assert.Equal(t, 0, r.RefRule.RequiredApprovingReviewCount)
	assert.False(t, r.RefRule.ViewerCanPush)
	assert.False(t, r.HasCodeowners)
}

func TestPullRequestParams(t *testing.T) {
	rule := BranchRule{Type: "pull_request", Parameters: json.RawMessage(`{
	  "required_approving_review_count": 1, "require_extra_approval_for_unattributed_changes": true,
	  "required_reviewers": [{"file_patterns": ["db/**"], "minimum_approvals": 0, "reviewer": {"id": 5, "type": "Team"}},
	                         {"file_patterns": ["*"], "reviewer": {"id": 6, "type": "Team"}}]}`)}
	p, err := rule.PullRequest()
	require.NoError(t, err)
	assert.True(t, p.RequireExtraApprovalForUnattributedChgs)
	assert.Equal(t, 0, p.RequiredReviewers[0].Minimum())
	assert.Equal(t, 1, p.RequiredReviewers[1].Minimum())

	empty, err := BranchRule{Type: "deletion", Parameters: json.RawMessage("null")}.PullRequest()
	require.NoError(t, err)
	assert.Zero(t, empty.RequiredApprovingReviewCount)
}

func TestIdentity(t *testing.T) {
	id := NewIdentity("Alice", map[int64]string{1: "acme/Eng"})
	assert.True(t, id.Is("@alice"))
	assert.True(t, id.Is("@ACME/eng"))
	assert.Equal(t, "@acme/Eng", id.TeamName(1))
	assert.Equal(t, "team 2", id.TeamName(2))
	id.AddTeamNames(map[int64]string{2: "@acme/dba", 1: "@ignored"})
	assert.Equal(t, "@acme/dba", id.TeamName(2))
	assert.Equal(t, "@acme/Eng", id.TeamName(1))
}
