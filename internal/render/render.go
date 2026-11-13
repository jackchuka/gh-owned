// Package render writes a scan report as a table, JSON, or gh-dash config.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/cli/go-gh/v2/pkg/tableprinter"
	"gopkg.in/yaml.v3"

	"github.com/jackchuka/gh-owned/internal/gate"
	"github.com/jackchuka/gh-owned/internal/owner"
	"github.com/jackchuka/gh-owned/internal/scan"
)

// TableOptions describe the terminal the table is written to.
type TableOptions struct {
	TTY   bool // aligned sections; otherwise tab-separated rows with a ROLE column
	Color bool
	Width int
}

var roleTitles = []struct {
	role        owner.Role
	title, hint string
}{
	{owner.RoleOwner, "Owner", "you own the default"},
	{owner.RolePathOwner, "Path owner", "you own some paths"},
	{owner.RoleApprover, "Approver", "nobody named you; any writer's approval merges"},
	{owner.RoleNone, "Not responsible", ""},
}

// Table writes the report for people: a summary, then one section per role.
// Without a TTY it writes tab-separated rows instead, for scripts.
func Table(w io.Writer, r *scan.Report, o TableOptions) error {
	st := styles{on: o.TTY && o.Color}
	if !o.TTY {
		tp := tableprinter.New(w, false, 0)
		for _, rep := range r.Repos {
			tp.AddField(string(rep.Role))
			tp.AddField(rep.Repo)
			tp.AddField(string(rep.Solo))
			tp.AddField(strings.Join(rep.Via, ","))
			tp.AddField(strings.Join(notes(rep), "; "))
			tp.EndRow()
		}
		return tp.Render()
	}

	counts := map[owner.Role]int{}
	via := map[string]int{}
	for _, rep := range r.Repos {
		counts[rep.Role]++
		if rep.Role == owner.RoleOwner || rep.Role == owner.RolePathOwner {
			for _, h := range rep.Via {
				via[h]++
			}
		}
	}
	p := &printer{w: w}
	p.f("%s  %s\n", st.bold(r.Viewer), st.dim(fmt.Sprintf("%d repos scanned · %d owner · %d path owner · %d approver",
		r.Scanned, counts[owner.RoleOwner], counts[owner.RolePathOwner], counts[owner.RoleApprover])))
	if len(via) > 0 {
		p.f("%s  %s\n", st.dim("via"), joinCounts(via))
	}
	if p.err != nil {
		return p.err
	}

	// Pad the leading columns to widths shared by every section so they line up.
	// A via list too long for the terminal moves to its own line, so one repo
	// with many owning teams neither widens the column for every row nor gets cut.
	var wRepo, wSolo, wVia int
	for _, rep := range r.Repos {
		wRepo = max(wRepo, runeLen(rep.Repo))
		wSolo = max(wSolo, runeLen(string(rep.Solo))+2)
	}
	viaFits := func(rep scan.RepoReport) bool {
		return o.Width <= 0 || 2+wRepo+2+wSolo+2+runeLen(viaText(rep)) <= o.Width
	}
	for _, rep := range r.Repos {
		if viaFits(rep) {
			wVia = max(wVia, runeLen(viaText(rep)))
		}
	}
	pad := func(t string, n int) string { return t + strings.Repeat(" ", max(n-runeLen(t), 0)) }

	for _, rt := range roleTitles {
		var reps []scan.RepoReport
		for _, rep := range r.Repos {
			if rep.Role == rt.role {
				reps = append(reps, rep)
			}
		}
		if len(reps) == 0 {
			continue
		}
		p.f("\n%s %s\n", st.bold(fmt.Sprintf("%s (%d)", rt.title, len(reps))), st.dim(rt.hint))
		for _, rep := range reps {
			mark, color := verdictMark(rep.Solo)
			cols := []column{
				{rep.Repo, wRepo, 1},
				{mark + " " + string(rep.Solo), wSolo, color},
			}
			var below []string
			inVia := viaFits(rep)
			if inVia {
				cols = append(cols, column{viaText(rep), wVia, cyan})
			} else {
				below = append(below, st.sgr(cyan, "via: "+viaText(rep)))
			}
			ns := notes(rep)
			inline := strings.Join(ns, " · ")
			width := 0
			for _, c := range cols {
				width += 2 + c.width
			}
			if len(ns) > 0 && inVia && (o.Width <= 0 || width+2+runeLen(inline) <= o.Width) {
				cols = append(cols, column{inline, 0, 2})
			} else {
				for len(cols) > 0 && cols[len(cols)-1].text == "" {
					cols = cols[:len(cols)-1]
				}
				for _, n := range ns {
					below = append(below, st.dim(n))
				}
			}
			var line strings.Builder
			for i, c := range cols {
				t := c.text
				if i < len(cols)-1 {
					t = pad(t, c.width)
				}
				line.WriteString("  " + st.sgr(c.code, t))
			}
			p.f("%s\n", line.String())
			for _, b := range below {
				p.f("      %s\n", b)
			}
		}
		if p.err != nil {
			return p.err
		}
	}
	return nil
}

type column struct {
	text  string
	width int
	code  int
}

func viaText(rep scan.RepoReport) string { return strings.Join(rep.Via, " ") }

func runeLen(t string) int { return len([]rune(t)) }

func notes(rep scan.RepoReport) []string {
	var out []string
	if len(rep.OwnedPaths) > 0 {
		out = append(out, "paths: "+gate.Abbreviate(rep.OwnedPaths, 3))
	}
	if len(rep.Unenforced) > 0 {
		out = append(out, "CODEOWNERS not enforced: "+strings.Join(rep.Unenforced, " "))
	}
	return append(out, rep.Notes...)
}

func verdictMark(v gate.Verdict) (string, int) {
	switch v {
	case gate.Open, gate.Bypass, gate.Approve, gate.Codeowner:
		return "✓", green
	case gate.Partial:
		return "~", yellow
	case gate.Blocked, gate.NoWrite:
		return "✗", red
	default:
		return "·", 0
	}
}

const (
	red    = 31
	green  = 32
	yellow = 33
	cyan   = 36
)

// styles wraps text in ANSI codes when on.
type styles struct{ on bool }

func (s styles) sgr(code int, t string) string {
	if !s.on || code == 0 {
		return t
	}
	return fmt.Sprintf("\x1b[%dm%s\x1b[0m", code, t)
}

func (s styles) bold(t string) string { return s.sgr(1, t) }
func (s styles) dim(t string) string  { return s.sgr(2, t) }

// JSON writes the report as indented JSON.
func JSON(w io.Writer, r *scan.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// maxReposPerSection keeps each search query well within GitHub's limits.
const maxReposPerSection = 25

type section struct {
	Title   string `yaml:"title"`
	Filters string `yaml:"filters"`
}

// GhDash writes a gh-dash config with prSections built from the report.
// Filters use the legacy search syntax gh-dash queries with (search type
// ISSUE), where repeated repo: qualifiers are ORed. Advanced search (as in
// `gh search prs`) would AND them.
func GhDash(w io.Writer, r *scan.Report) error {
	var owned, approver []string
	for _, rep := range r.Repos {
		switch rep.Role {
		case owner.RoleOwner, owner.RolePathOwner:
			owned = append(owned, rep.Repo)
		case owner.RoleApprover:
			approver = append(approver, rep.Repo)
		}
	}
	var sections []section
	sections = append(sections, section{
		Title:   "Review requested",
		Filters: "is:open -author:@me draft:false review-requested:@me",
	})
	sections = append(sections, repoSections("Owned: needs review", "is:open -author:@me draft:false review:required", owned)...)
	sections = append(sections, repoSections("Owned: approved", "is:open review:approved", owned)...)
	sections = append(sections, repoSections("Supplementary: any writer can approve", "is:open -author:@me draft:false review:required", approver)...)

	if _, err := fmt.Fprintf(w, "# Generated by gh-owned for %s. Merge prSections into your gh-dash config.\n", r.Viewer); err != nil {
		return err
	}
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(map[string][]section{"prSections": sections}); err != nil {
		return err
	}
	return enc.Close()
}

// repoSections splits repos into sections per org, chunked to keep queries short.
func repoSections(title, base string, repos []string) []section {
	byOrg := map[string][]string{}
	for _, r := range repos {
		org := strings.SplitN(r, "/", 2)[0]
		byOrg[org] = append(byOrg[org], r)
	}
	orgs := make([]string, 0, len(byOrg))
	for o := range byOrg {
		orgs = append(orgs, o)
	}
	sort.Strings(orgs)

	var out []section
	for _, org := range orgs {
		list := byOrg[org]
		for i := 0; i < len(list); i += maxReposPerSection {
			chunk := list[i:min(i+maxReposPerSection, len(list))]
			t := fmt.Sprintf("%s · %s", title, org)
			if len(list) > maxReposPerSection {
				t = fmt.Sprintf("%s (%d/%d)", t, i/maxReposPerSection+1, (len(list)+maxReposPerSection-1)/maxReposPerSection)
			}
			qs := make([]string, len(chunk))
			for j, r := range chunk {
				qs[j] = "repo:" + r
			}
			out = append(out, section{Title: t, Filters: base + " " + strings.Join(qs, " ")})
		}
	}
	return out
}

func joinCounts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if m[keys[i]] != m[keys[j]] {
			return m[keys[i]] > m[keys[j]]
		}
		return keys[i] < keys[j]
	})
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s %d", k, m[k])
	}
	return strings.Join(parts, " · ")
}

type printer struct {
	w   io.Writer
	err error
}

func (p *printer) f(format string, args ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, args...)
	}
}
