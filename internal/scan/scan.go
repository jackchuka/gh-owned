// Package scan runs gh-owned end to end: resolve targets, fetch repos, and
// evaluate ownership and the solo-merge verdict for each.
package scan

import (
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/jackchuka/gh-owned/internal/gate"
	"github.com/jackchuka/gh-owned/internal/github"
	"github.com/jackchuka/gh-owned/internal/owner"
)

// Options select targets and shape the evaluation.
type Options struct {
	Enterprises     []string
	Orgs            []string
	Repos           []string
	ExcludeOrgs     []string
	IgnoreTeams     []string
	IncludeArchived bool
	NoApprover      bool
	AllRepos        bool
	Gate            gate.Options
	Jobs            int
}

// RepoReport is the outcome for one repository.
type RepoReport struct {
	Repo         string       `json:"repo"`
	Permission   string       `json:"permission"`
	Role         owner.Role   `json:"role"`
	OwnedPaths   []string     `json:"owned_paths,omitempty"`
	Via          []string     `json:"via,omitempty"`
	Solo         gate.Verdict `json:"solo"`
	Notes        []string     `json:"notes,omitempty"`
	CopilotExtra bool         `json:"copilot_extra,omitempty"`
	Unenforced   []string     `json:"unenforced_codeowners,omitempty"`
}

// Report is the full result.
type Report struct {
	Viewer  string       `json:"viewer"`
	Scanned int          `json:"scanned"`
	Repos   []RepoReport `json:"repos"`
}

// Fetcher is the subset of the GitHub client the scan uses.
type Fetcher interface {
	EnterpriseOrgs(slug string) ([]string, error)
	LoadIdentity(orgs []string) (*github.Identity, error)
	ListRepos(org string) ([]github.Repo, error)
	LoadRules(r *github.Repo) error
	OrgTeamNames(org string) (map[int64]string, error)
}

// Run executes the scan.
func Run(c Fetcher, opts Options) (*Report, error) {
	orgs, err := resolveOrgs(c, opts)
	if err != nil {
		return nil, err
	}
	id, err := c.LoadIdentity(orgs)
	if err != nil {
		return nil, err
	}
	matcher := owner.Matcher{Identity: id, Ignored: map[string]bool{}}
	for _, t := range opts.IgnoreTeams {
		matcher.Ignored["@"+strings.ToLower(strings.TrimPrefix(t, "@"))] = true
	}

	wanted := map[string]bool{}
	for _, r := range opts.Repos {
		wanted[strings.ToLower(r)] = true
	}
	var repos []github.Repo
	for _, org := range orgs {
		list, err := c.ListRepos(org)
		if err != nil {
			return nil, err
		}
		for _, r := range list {
			if len(wanted) > 0 && !wanted[strings.ToLower(r.NameWithOwner)] {
				continue
			}
			if r.IsArchived && !opts.IncludeArchived {
				continue
			}
			repos = append(repos, r)
		}
	}

	names := &teamNameCache{c: c, done: map[string]bool{}}
	reports := make([]RepoReport, len(repos))
	parallel(len(repos), max(opts.Jobs, 1), func(i int) {
		reports[i] = evaluate(c, &repos[i], id, matcher, names, opts)
	})

	out := &Report{Viewer: id.Login, Scanned: len(reports)}
	for _, r := range reports {
		if opts.AllRepos || r.Role != owner.RoleNone || r.Solo == gate.Error {
			out.Repos = append(out.Repos, r)
		}
	}
	order := map[owner.Role]int{owner.RoleOwner: 0, owner.RolePathOwner: 1, owner.RoleApprover: 2, owner.RoleNone: 3}
	sort.SliceStable(out.Repos, func(i, j int) bool {
		a, b := out.Repos[i], out.Repos[j]
		if order[a.Role] != order[b.Role] {
			return order[a.Role] < order[b.Role]
		}
		return strings.ToLower(a.Repo) < strings.ToLower(b.Repo)
	})
	return out, nil
}

func resolveOrgs(c Fetcher, opts Options) ([]string, error) {
	set := map[string]bool{}
	for _, o := range opts.Orgs {
		set[o] = true
	}
	for _, slug := range opts.Enterprises {
		orgs, err := c.EnterpriseOrgs(slug)
		if err != nil {
			return nil, err
		}
		for _, o := range orgs {
			set[o] = true
		}
	}
	for _, r := range opts.Repos {
		set[strings.SplitN(r, "/", 2)[0]] = true
	}
	for _, o := range opts.ExcludeOrgs {
		delete(set, o)
	}
	orgs := make([]string, 0, len(set))
	for o := range set {
		orgs = append(orgs, o)
	}
	sort.Strings(orgs)
	return orgs, nil
}

func evaluate(c Fetcher, r *github.Repo, id *github.Identity, m owner.Matcher, names *teamNameCache, opts Options) RepoReport {
	rep := RepoReport{Repo: r.NameWithOwner, Permission: r.ViewerPermission, Role: owner.RoleNone}
	fail := func(err error) RepoReport {
		rep.Solo, rep.Notes = gate.Error, []string{err.Error()}
		return rep
	}
	if r.ViewerPermission == "ADMIN" || r.ViewerPermission == "MAINTAIN" || r.ViewerPermission == "WRITE" {
		if err := c.LoadRules(r); err != nil {
			return fail(err)
		}
	}
	reqs, err := gate.Requirements(*r, id)
	if err != nil {
		return fail(err)
	}
	var required []github.RequiredReviewer
	for _, q := range reqs {
		required = append(required, q.RequiredReviewers...)
	}
	names.resolve(r.Owner(), required, id)

	rs := owner.Parse(r.Codeowners)
	own := owner.Evaluate(rs, required, m)
	res := gate.Evaluate(*r, reqs, rs, id, opts.Gate)
	rep.Role, rep.OwnedPaths, rep.Via = own.Role, own.Paths, own.Via
	if strings.EqualFold(r.Owner(), id.Login) && !r.IsFork && rep.Role != owner.RoleOwner {
		// A repo under your own user account is yours, CODEOWNERS or not.
		rep.Role, rep.OwnedPaths = owner.RoleOwner, nil
		rep.Via = append(rep.Via, "@"+id.Login)
		sort.Strings(rep.Via)
		rep.Via = slices.Compact(rep.Via)
	}
	rep.Solo, rep.Notes, rep.CopilotExtra = res.Verdict, res.Notes, res.CopilotExtra

	if rep.Role == owner.RoleNone && rep.Solo == gate.Approve && !opts.NoApprover {
		rep.Role = owner.RoleApprover
		if top, ok := owner.Catchall(rs); ok {
			for _, o := range top {
				rep.Unenforced = append(rep.Unenforced, o.String())
			}
		}
	}
	if rep.CopilotExtra {
		rep.Notes = append(rep.Notes, "Copilot-opened PRs need one more approval")
	}
	return rep
}

// teamNameCache looks up org team names once per org, only when a
// required reviewer's team is unknown.
type teamNameCache struct {
	c    Fetcher
	mu   sync.Mutex
	done map[string]bool
}

func (t *teamNameCache) resolve(org string, required []github.RequiredReviewer, id *github.Identity) {
	missing := false
	for _, rr := range required {
		missing = missing || !id.HasTeamName(rr.Reviewer.ID)
	}
	if !missing {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done[org] {
		return
	}
	t.done[org] = true
	if names, err := t.c.OrgTeamNames(org); err == nil {
		id.AddTeamNames(names)
	}
}

func parallel(n, jobs int, fn func(i int)) {
	sem := make(chan struct{}, jobs)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}()
	}
	wg.Wait()
}
