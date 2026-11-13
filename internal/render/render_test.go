package render

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jackchuka/gh-owned/internal/gate"
	"github.com/jackchuka/gh-owned/internal/owner"
	"github.com/jackchuka/gh-owned/internal/scan"
)

var update = flag.Bool("update", false, "rewrite golden files")

func report() *scan.Report {
	r := &scan.Report{Viewer: "alice", Scanned: 4}
	r.Repos = []scan.RepoReport{
		{Repo: "acme/api", Permission: "ADMIN", Role: owner.RoleOwner, Via: []string{"@acme/eng"}, Solo: gate.Codeowner},
		{Repo: "acme/web", Permission: "ADMIN", Role: owner.RolePathOwner, OwnedPaths: []string{"/a/", "/b/", "/c/", "/d/"}, Via: []string{"@acme/eng", "@alice", "@acme/platform-infrastructure"}, Solo: gate.Partial, Notes: []string{"CODEOWNERS owned by others: /e/"}},
		{Repo: "labs/x", Permission: "WRITE", Role: owner.RoleOwner, Via: []string{"@alice"}, Solo: gate.Approve, CopilotExtra: true, Notes: []string{"Copilot-opened PRs need one more approval"}},
		{Repo: "acme/ops", Permission: "ADMIN", Role: owner.RoleApprover, Solo: gate.Approve, Unenforced: []string{"@bob"}},
	}
	for i := range 27 {
		r.Repos = append(r.Repos, scan.RepoReport{Repo: "big/r" + string(rune('a'+i%26)) + string(rune('0'+i/26)), Role: owner.RoleOwner, Solo: gate.Open})
	}
	return r
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		require.NoError(t, os.WriteFile(path, got, 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
}

func TestFormats(t *testing.T) {
	for name, fn := range map[string]func(*bytes.Buffer, *scan.Report) error{
		"table.golden": func(b *bytes.Buffer, r *scan.Report) error {
			return Table(b, r, TableOptions{TTY: true, Width: 160})
		},
		"color.golden": func(b *bytes.Buffer, r *scan.Report) error {
			return Table(b, r, TableOptions{TTY: true, Color: true, Width: 160})
		},
		"narrow.golden": func(b *bytes.Buffer, r *scan.Report) error {
			return Table(b, r, TableOptions{TTY: true, Width: 60})
		},
		"tsv.golden":    func(b *bytes.Buffer, r *scan.Report) error { return Table(b, r, TableOptions{}) },
		"json.golden":   func(b *bytes.Buffer, r *scan.Report) error { return JSON(b, r) },
		"ghdash.golden": func(b *bytes.Buffer, r *scan.Report) error { return GhDash(b, r) },
	} {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			require.NoError(t, fn(&b, report()))
			golden(t, name, b.Bytes())
		})
	}
}
