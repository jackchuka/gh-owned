package github

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Repo is one repository with everything gh-owned evaluates on its default branch.
type Repo struct {
	NameWithOwner    string
	IsArchived       bool
	IsEmpty          bool
	IsFork           bool
	ViewerPermission string // ADMIN, MAINTAIN, WRITE, TRIAGE, READ
	DefaultBranch    string // "" when the repo has no default branch

	// Protection is the classic branch protection rule, readable by admins only.
	Protection *Protection
	// RefRule is the reduced view of classic protection readable with read access.
	RefRule *RefRule

	Codeowners    string
	HasCodeowners bool

	// Rules are the effective rules on the default branch (rulesets at every level).
	Rules []BranchRule
	// Bypass maps ruleset id -> current_user_can_bypass.
	Bypass map[int64]string
}

// Owner is the org part of NameWithOwner.
func (r Repo) Owner() string {
	return strings.SplitN(r.NameWithOwner, "/", 2)[0]
}

// Protection is a classic branch protection rule.
type Protection struct {
	RequiresApprovingReviews     bool
	RequiredApprovingReviewCount int
	RequiresCodeOwnerReviews     bool
	IsAdminEnforced              bool
	RequireLastPushApproval      bool
	RestrictsPushes              bool
	LockBranch                   bool
	BypassPullRequestActors      []string // "@login" / "@org/team"
	PushActors                   []string
}

// RefRule is the part of classic protection visible without admin access.
type RefRule struct {
	RequiredApprovingReviewCount int
	RequiresCodeOwnerReviews     bool
	ViewerCanPush                bool
}

// BranchRule is one rule from GET /repos/{repo}/rules/branches/{branch}.
type BranchRule struct {
	Type       string          `json:"type"`
	RulesetID  int64           `json:"ruleset_id"`
	Parameters json.RawMessage `json:"parameters"`
}

// PullRequestParams are the parameters of a "pull_request" rule.
type PullRequestParams struct {
	RequiredApprovingReviewCount            int                `json:"required_approving_review_count"`
	RequireCodeOwnerReview                  bool               `json:"require_code_owner_review"`
	RequireLastPushApproval                 bool               `json:"require_last_push_approval"`
	RequireExtraApprovalForUnattributedChgs bool               `json:"require_extra_approval_for_unattributed_changes"`
	RequiredReviewers                       []RequiredReviewer `json:"required_reviewers"`
}

// RequiredReviewer is one team required to review files matching patterns.
type RequiredReviewer struct {
	FilePatterns     []string `json:"file_patterns"`
	MinimumApprovals *int     `json:"minimum_approvals"`
	Reviewer         struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"reviewer"`
}

// Minimum is the number of approvals the team must give (default 1).
func (r RequiredReviewer) Minimum() int {
	if r.MinimumApprovals == nil {
		return 1
	}
	return *r.MinimumApprovals
}

// PullRequest decodes the parameters of a pull_request rule.
func (r BranchRule) PullRequest() (PullRequestParams, error) {
	var p PullRequestParams
	if len(r.Parameters) == 0 || string(r.Parameters) == "null" {
		return p, nil
	}
	err := json.Unmarshal(r.Parameters, &p)
	return p, err
}

const reposQuery = `
query($owner: String!, $cursor: String) {
  repositoryOwner(login: $owner) {
    repositories(first: 50, after: $cursor, ownerAffiliations: [OWNER], orderBy: {field: NAME, direction: ASC}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        nameWithOwner isArchived isEmpty isFork viewerPermission
        defaultBranchRef {
          name
          branchProtectionRule {
            requiresApprovingReviews requiredApprovingReviewCount requiresCodeOwnerReviews
            isAdminEnforced requireLastPushApproval restrictsPushes lockBranch
            bypassPullRequestAllowances(first: 100) { nodes { actor { ...actor } } }
            pushAllowances(first: 100) { nodes { actor { ...actor } } }
          }
          refUpdateRule { requiredApprovingReviewCount requiresCodeOwnerReviews viewerCanPush }
        }
        co1: object(expression: "HEAD:.github/CODEOWNERS") { ... on Blob { text } }
        co2: object(expression: "HEAD:CODEOWNERS") { ... on Blob { text } }
        co3: object(expression: "HEAD:docs/CODEOWNERS") { ... on Blob { text } }
      }
    }
  }
}
fragment actor on BranchActorAllowanceActor {
  __typename
  ... on User { login }
  ... on Team { combinedSlug }
}`

type actorNodes struct {
	Nodes []struct {
		Actor *struct {
			Login        string `json:"login"`
			CombinedSlug string `json:"combinedSlug"`
		} `json:"actor"`
	} `json:"nodes"`
}

func (a actorNodes) handles() []string {
	var out []string
	for _, n := range a.Nodes {
		switch {
		case n.Actor == nil:
		case n.Actor.Login != "":
			out = append(out, "@"+n.Actor.Login)
		case n.Actor.CombinedSlug != "":
			out = append(out, "@"+n.Actor.CombinedSlug)
		}
	}
	return out
}

type blob struct {
	Text string `json:"text"`
}

type repoNode struct {
	NameWithOwner    string `json:"nameWithOwner"`
	IsArchived       bool   `json:"isArchived"`
	IsEmpty          bool   `json:"isEmpty"`
	IsFork           bool   `json:"isFork"`
	ViewerPermission string `json:"viewerPermission"`
	DefaultBranchRef *struct {
		Name                 string `json:"name"`
		BranchProtectionRule *struct {
			RequiresApprovingReviews     bool       `json:"requiresApprovingReviews"`
			RequiredApprovingReviewCount int        `json:"requiredApprovingReviewCount"`
			RequiresCodeOwnerReviews     bool       `json:"requiresCodeOwnerReviews"`
			IsAdminEnforced              bool       `json:"isAdminEnforced"`
			RequireLastPushApproval      bool       `json:"requireLastPushApproval"`
			RestrictsPushes              bool       `json:"restrictsPushes"`
			LockBranch                   bool       `json:"lockBranch"`
			BypassPullRequestAllowances  actorNodes `json:"bypassPullRequestAllowances"`
			PushAllowances               actorNodes `json:"pushAllowances"`
		} `json:"branchProtectionRule"`
		RefUpdateRule *struct {
			RequiredApprovingReviewCount *int  `json:"requiredApprovingReviewCount"`
			RequiresCodeOwnerReviews     bool  `json:"requiresCodeOwnerReviews"`
			ViewerCanPush                *bool `json:"viewerCanPush"`
		} `json:"refUpdateRule"`
	} `json:"defaultBranchRef"`
	Co1 *blob `json:"co1"`
	Co2 *blob `json:"co2"`
	Co3 *blob `json:"co3"`
}

func (n repoNode) toRepo() Repo {
	r := Repo{
		NameWithOwner:    n.NameWithOwner,
		IsArchived:       n.IsArchived,
		IsEmpty:          n.IsEmpty,
		IsFork:           n.IsFork,
		ViewerPermission: n.ViewerPermission,
	}
	for _, b := range []*blob{n.Co1, n.Co2, n.Co3} {
		if b != nil {
			r.Codeowners, r.HasCodeowners = b.Text, true
			break
		}
	}
	ref := n.DefaultBranchRef
	if ref == nil {
		return r
	}
	r.DefaultBranch = ref.Name
	if bp := ref.BranchProtectionRule; bp != nil {
		r.Protection = &Protection{
			RequiresApprovingReviews:     bp.RequiresApprovingReviews,
			RequiredApprovingReviewCount: bp.RequiredApprovingReviewCount,
			RequiresCodeOwnerReviews:     bp.RequiresCodeOwnerReviews,
			IsAdminEnforced:              bp.IsAdminEnforced,
			RequireLastPushApproval:      bp.RequireLastPushApproval,
			RestrictsPushes:              bp.RestrictsPushes,
			LockBranch:                   bp.LockBranch,
			BypassPullRequestActors:      bp.BypassPullRequestAllowances.handles(),
			PushActors:                   bp.PushAllowances.handles(),
		}
	}
	if rr := ref.RefUpdateRule; rr != nil {
		r.RefRule = &RefRule{RequiresCodeOwnerReviews: rr.RequiresCodeOwnerReviews, ViewerCanPush: true}
		if rr.RequiredApprovingReviewCount != nil {
			r.RefRule.RequiredApprovingReviewCount = *rr.RequiredApprovingReviewCount
		}
		if rr.ViewerCanPush != nil {
			r.RefRule.ViewerCanPush = *rr.ViewerCanPush
		}
	}
	return r
}

// ListRepos returns the repositories owned by an org or user account, with
// protection and CODEOWNERS.
func (c *Client) ListRepos(owner string) ([]Repo, error) {
	var repos []Repo
	err := paginate(func(cursor *string) (pageInfo, error) {
		var resp struct {
			RepositoryOwner *struct {
				Repositories struct {
					PageInfo pageInfo   `json:"pageInfo"`
					Nodes    []repoNode `json:"nodes"`
				} `json:"repositories"`
			} `json:"repositoryOwner"`
		}
		if err := c.query(reposQuery, map[string]any{"owner": owner, "cursor": cursor}, &resp); err != nil {
			return pageInfo{}, fmt.Errorf("repositories of %s: %w", owner, err)
		}
		if resp.RepositoryOwner == nil {
			return pageInfo{}, fmt.Errorf("no organization or user named %s", owner)
		}
		for _, n := range resp.RepositoryOwner.Repositories.Nodes {
			repos = append(repos, n.toRepo())
		}
		return resp.RepositoryOwner.Repositories.PageInfo, nil
	})
	return repos, err
}

// LoadRules fills r.Rules and r.Bypass from the REST rules API. Only rulesets
// with rules that gate merging are looked up for bypass.
func (c *Client) LoadRules(r *Repo) error {
	if r.DefaultBranch == "" {
		return nil
	}
	path := fmt.Sprintf("repos/%s/rules/branches/%s", r.NameWithOwner, url.PathEscape(r.DefaultBranch))
	if err := c.get(path, &r.Rules); err != nil {
		return fmt.Errorf("rules of %s: %w", r.NameWithOwner, err)
	}
	r.Bypass = map[int64]string{}
	for _, rule := range r.Rules {
		if rule.Type != "pull_request" && rule.Type != "update" {
			continue
		}
		if _, done := r.Bypass[rule.RulesetID]; done {
			continue
		}
		var rs struct {
			CurrentUserCanBypass string `json:"current_user_can_bypass"`
		}
		if err := c.get(fmt.Sprintf("repos/%s/rulesets/%d", r.NameWithOwner, rule.RulesetID), &rs); err != nil {
			return fmt.Errorf("ruleset %d of %s: %w", rule.RulesetID, r.NameWithOwner, err)
		}
		r.Bypass[rule.RulesetID] = rs.CurrentUserCanBypass
	}
	return nil
}
