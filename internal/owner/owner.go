// Package owner decides whether the viewer is responsible for a repository,
// from CODEOWNERS and ruleset required-reviewer teams.
package owner

import (
	"slices"
	"sort"
	"strings"

	"github.com/hmarr/codeowners"

	"github.com/jackchuka/gh-owned/internal/github"
)

// Role is the viewer's responsibility for a repo.
type Role string

const (
	RoleOwner     Role = "owner"      // owns the default (catch-all) or a required-reviewer team on all files
	RolePathOwner Role = "path-owner" // owns some paths only
	RoleApprover  Role = "approver"   // named nowhere, but any writer's approval satisfies review
	RoleNone      Role = "none"
)

var catchall = map[string]bool{"*": true, "/*": true, "**": true, "/**": true, "**/*": true, "/**/*": true}

// IsCatchall reports whether a CODEOWNERS or file pattern matches every file.
func IsCatchall(pattern string) bool { return catchall[pattern] }

// Matcher answers "is this handle the viewer?" for ownership purposes.
type Matcher struct {
	Identity *github.Identity
	Ignored  map[string]bool // lowercase handles that do not make the viewer responsible
}

// Owns returns the owners in the list that are the viewer and not ignored.
func (m Matcher) Owns(owners []codeowners.Owner) []string {
	var out []string
	for _, o := range owners {
		h := strings.ToLower(o.String())
		if m.Identity.Is(h) && !m.Ignored[h] {
			out = append(out, o.String())
		}
	}
	return out
}

// OwnsTeam reports whether a required-reviewer team counts as the viewer.
func (m Matcher) OwnsTeam(teamID int64) bool {
	return m.Identity.TeamIDs[teamID] && !m.Ignored[strings.ToLower(m.Identity.TeamName(teamID))]
}

// Parse reads CODEOWNERS text; unparsable lines are skipped by the library's
// error, so a broken file yields no rules.
func Parse(text string) codeowners.Ruleset {
	rs, err := codeowners.ParseFile(strings.NewReader(text))
	if err != nil {
		return nil
	}
	return rs
}

// Catchall returns the owners of the last catch-all rule, and whether one exists.
func Catchall(rs codeowners.Ruleset) ([]codeowners.Owner, bool) {
	for i := len(rs) - 1; i >= 0; i-- {
		if IsCatchall(rs[i].RawPattern()) {
			return rs[i].Owners, true
		}
	}
	return nil, false
}

// Result is the ownership of one repo.
type Result struct {
	Role  Role
	Paths []string // owned patterns for path-owner
	Via   []string // handles that make the viewer responsible
}

// Evaluate decides the role from CODEOWNERS rules and required reviewers.
func Evaluate(rs codeowners.Ruleset, required []github.RequiredReviewer, m Matcher) Result {
	via := map[string]bool{}
	var rrPatterns []string
	allFiles := false
	for _, rr := range required {
		if rr.Minimum() == 0 || !m.OwnsTeam(rr.Reviewer.ID) {
			continue
		}
		via[m.Identity.TeamName(rr.Reviewer.ID)] = true
		patterns := rr.FilePatterns
		if len(patterns) == 0 {
			patterns = []string{"*"}
		}
		for _, p := range patterns {
			allFiles = allFiles || IsCatchall(p)
		}
		rrPatterns = append(rrPatterns, patterns...)
	}

	if top, ok := Catchall(rs); ok {
		for _, h := range m.Owns(top) {
			via[h] = true
			allFiles = true
		}
	}
	if allFiles {
		return Result{Role: RoleOwner, Via: keys(via)}
	}

	var paths []string
	for _, rule := range rs {
		if IsCatchall(rule.RawPattern()) {
			continue
		}
		if hit := m.Owns(rule.Owners); len(hit) > 0 {
			paths = append(paths, rule.RawPattern())
			for _, h := range hit {
				via[h] = true
			}
		}
	}
	paths = append(paths, rrPatterns...)
	if len(paths) == 0 {
		return Result{Role: RoleNone}
	}
	return Result{Role: RolePathOwner, Paths: paths, Via: keys(via)}
}

// Coverage describes how much of CODEOWNERS the viewer owns, for merge gating.
type Coverage string

const (
	CoverageNone    Coverage = "none"    // no CODEOWNERS rules
	CoverageFull    Coverage = "full"    // the viewer owns the default and every owned path
	CoveragePartial Coverage = "partial" // the viewer owns the default; some paths belong to others
	CoverageForeign Coverage = "foreign" // the default belongs to someone else
)

// CodeownerCoverage reports the viewer's coverage of CODEOWNERS, ignoring no
// teams: for merging, every team you are in counts. foreign lists patterns
// owned only by others.
func CodeownerCoverage(rs codeowners.Ruleset, id *github.Identity) (Coverage, []string) {
	if len(rs) == 0 {
		return CoverageNone, nil
	}
	m := Matcher{Identity: id}
	top, ok := Catchall(rs)
	defaultOK := !ok || len(top) == 0 || len(m.Owns(top)) > 0
	var foreign []string
	for _, rule := range rs {
		if IsCatchall(rule.RawPattern()) || len(rule.Owners) == 0 || len(m.Owns(rule.Owners)) > 0 {
			continue
		}
		foreign = append(foreign, rule.RawPattern())
	}
	switch {
	case !defaultOK:
		return CoverageForeign, foreign
	case len(foreign) > 0:
		return CoveragePartial, foreign
	default:
		return CoverageFull, nil
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return slices.Compact(out)
}
