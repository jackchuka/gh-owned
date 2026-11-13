// Command demo runs the real gh-owned CLI on a fabricated "acme" org, offline,
// for the README recording (assets/demo.tape).
package main

import (
	"encoding/json"
	"os"

	"github.com/jackchuka/gh-owned/cmd"
	"github.com/jackchuka/gh-owned/internal/github"
	"github.com/jackchuka/gh-owned/internal/scan"
)

type fake struct{}

func (fake) EnterpriseOrgs(string) ([]string, error) { return []string{"acme"}, nil }
func (fake) LoadRules(*github.Repo) error            { return nil }
func (fake) OrgTeamNames(string) (map[int64]string, error) {
	return map[int64]string{9: "acme/payments"}, nil
}

// alice is in acme/platform (child of acme/engineering) and acme/everyone.
func (fake) LoadIdentity([]string) (*github.Identity, error) {
	return github.NewIdentity("alice", map[int64]string{
		1: "acme/platform", 2: "acme/engineering", 3: "acme/everyone",
	}), nil
}

func protection(approvals int, codeOwner bool) *github.Protection {
	return &github.Protection{
		RequiresApprovingReviews: true, RequiredApprovingReviewCount: approvals,
		RequiresCodeOwnerReviews: codeOwner, IsAdminEnforced: true,
	}
}

func pullRequest(id int64, params map[string]any) []github.BranchRule {
	raw, _ := json.Marshal(params)
	return []github.BranchRule{{Type: "pull_request", RulesetID: id, Parameters: raw}}
}

func repo(name, codeowners string) github.Repo {
	return github.Repo{
		NameWithOwner: "acme/" + name, ViewerPermission: "ADMIN", DefaultBranch: "main",
		Codeowners: codeowners, HasCodeowners: codeowners != "",
	}
}

func (fake) ListRepos(string) ([]github.Repo, error) {
	api := repo("api", "* @acme/platform\n")
	api.Protection = protection(1, true)

	web := repo("web", "* @acme/everyone\n/billing/ @acme/payments\n")
	web.Protection = protection(1, true)

	handbook := repo("handbook", "* @acme/everyone\n")
	handbook.Protection = protection(1, false)

	sdk := repo("sdk", "")
	sdk.Rules = pullRequest(11, map[string]any{
		"required_approving_review_count": 1, "require_last_push_approval": true,
		"required_reviewers": []any{map[string]any{
			"reviewer": map[string]any{"id": 2, "type": "Team"}, "file_patterns": []string{"*"}, "minimum_approvals": 1,
		}},
	})
	sdk.Bypass = map[int64]string{11: "never"}

	cli := repo("cli", "* @acme/platform\n")
	cli.Rules = pullRequest(12, map[string]any{"required_approving_review_count": 2})
	cli.Bypass = map[int64]string{12: "always"}

	infra := repo("infra", "* @acme/sre\n/modules/dns/ @alice\n")
	infra.Protection = protection(1, true)

	tools := repo("tools", "* @bob\n")
	tools.Rules = pullRequest(13, map[string]any{
		"required_approving_review_count": 1, "require_extra_approval_for_unattributed_changes": true,
	})
	tools.Bypass = map[int64]string{13: "never"}

	payments := repo("payments", "* @acme/payments\n")
	payments.Protection = protection(1, true)

	return []github.Repo{api, web, handbook, sdk, cli, infra, tools, payments}, nil
}

func main() {
	if err := cmd.NewRootCmd(func() (scan.Fetcher, error) { return fake{}, nil }).Execute(); err != nil {
		os.Exit(1)
	}
}
