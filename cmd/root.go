// Package cmd implements the gh-owned CLI.
package cmd

import (
	"fmt"
	"os"

	"github.com/cli/go-gh/v2/pkg/term"
	"github.com/spf13/cobra"

	"github.com/jackchuka/gh-owned/internal/github"
	"github.com/jackchuka/gh-owned/internal/render"
	"github.com/jackchuka/gh-owned/internal/scan"
)

// NewRootCmd builds the CLI. newFetcher supplies the data source, so the demo
// in assets/demo can run the real command offline on fabricated data.
func NewRootCmd(newFetcher func() (scan.Fetcher, error)) *cobra.Command {
	var (
		opts   scan.Options
		format string
	)
	rootCmd := &cobra.Command{
		Use:   "owned",
		Short: "List the repos you are responsible for and whether you can merge there alone",
		Long: `gh-owned lists the repositories you are responsible for, from CODEOWNERS
(you or one of your teams, ancestor teams included) and ruleset required-reviewer
teams. Admin access alone is not responsibility.

Each repo gets a role and a solo-merge verdict:

  role   owner       you own the default (CODEOWNERS *) or are a required reviewer on all files
         path-owner  you own some paths
         approver    nobody named you, but any writer's approval satisfies review
  solo   open, bypass, approve, codeowner   you can approve and merge alone
         partial                            some paths need someone else
         blocked                            someone else must act
         no-write, empty, error`,
		Example: `  gh owned --org acme
  gh owned --enterprise acme-corp --exclude-org acme-sandbox --ignore-team @acme/everyone
  gh owned --org acme --format gh-dash > owned-sections.yml
  gh owned --repo acme/api --as-pusher --format json`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
	}
	rootCmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return run(cmd, newFetcher, opts, format)
	}
	f := rootCmd.Flags()
	f.StringArrayVar(&opts.Enterprises, "enterprise", nil, "Enterprise slug; scans every org you belong to in it")
	f.StringArrayVar(&opts.Orgs, "org", nil, "Organization or user login")
	f.StringArrayVar(&opts.Repos, "repo", nil, "Repository as OWNER/NAME")
	f.StringArrayVar(&opts.ExcludeOrgs, "exclude-org", nil, "Organization to skip")
	f.StringArrayVar(&opts.IgnoreTeams, "ignore-team", nil, "Team (@org/team) whose ownership does not make you responsible")
	f.BoolVar(&opts.IncludeArchived, "include-archived", false, "Include archived repositories")
	f.BoolVar(&opts.NoApprover, "no-approver", false, "Do not report approver repos")
	f.BoolVar(&opts.AllRepos, "all-repos", false, "Also list repos you are not responsible for")
	f.BoolVar(&opts.Gate.AsAuthor, "as-author", false, "Judge solo-merge as the PR author (your approval does not count)")
	f.BoolVar(&opts.Gate.AsPusher, "as-pusher", false, "Judge solo-merge as the PR's latest pusher")
	f.StringVar(&format, "format", "table", "Output format: table, json, gh-dash")
	f.IntVar(&opts.Jobs, "jobs", 16, "Concurrent repository lookups")
	return rootCmd
}

// Execute runs the CLI against GitHub.
func Execute() {
	cmd := NewRootCmd(func() (scan.Fetcher, error) { return github.NewClient() })
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// tableOptions follows gh: aligned and colored on a terminal, TSV when piped.
func tableOptions() render.TableOptions {
	t := term.FromEnv()
	width, _, err := t.Size()
	if err != nil || width <= 0 {
		width = 120
	}
	return render.TableOptions{TTY: t.IsTerminalOutput(), Color: t.IsColorEnabled(), Width: width}
}

func run(cmd *cobra.Command, newFetcher func() (scan.Fetcher, error), opts scan.Options, format string) error {
	write := map[string]func(*scan.Report) error{
		"table":   func(r *scan.Report) error { return render.Table(cmd.OutOrStdout(), r, tableOptions()) },
		"json":    func(r *scan.Report) error { return render.JSON(cmd.OutOrStdout(), r) },
		"gh-dash": func(r *scan.Report) error { return render.GhDash(cmd.OutOrStdout(), r) },
	}[format]
	if write == nil {
		return fmt.Errorf("unknown --format %q (want table, json, gh-dash)", format)
	}
	if len(opts.Enterprises)+len(opts.Orgs)+len(opts.Repos) == 0 {
		return fmt.Errorf("give at least one of --enterprise, --org, --repo")
	}
	client, err := newFetcher()
	if err != nil {
		return err
	}
	report, err := scan.Run(client, opts)
	if err != nil {
		return err
	}
	return write(report)
}
