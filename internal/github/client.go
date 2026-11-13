// Package github fetches the data gh-owned evaluates: the viewer's teams,
// repositories with their default-branch protection and CODEOWNERS, and the
// effective branch rules with the viewer's ruleset bypass status.
package github

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/cli/go-gh/v2/pkg/api"
)

// Client wraps the go-gh REST and GraphQL clients, authenticated like `gh`.
type Client struct {
	rest *api.RESTClient
	gql  *api.GraphQLClient
}

// NewClient builds a Client using the same auth as `gh` (token + host).
func NewClient() (*Client, error) {
	rest, err := api.DefaultRESTClient()
	if err != nil {
		return nil, fmt.Errorf("create REST client (is `gh auth login` done?): %w", err)
	}
	gql, err := api.DefaultGraphQLClient()
	if err != nil {
		return nil, err
	}
	return &Client{rest: rest, gql: gql}, nil
}

func (c *Client) query(q string, vars map[string]any, out any) error {
	return mapAuthError(c.gql.Do(q, vars, out))
}

func (c *Client) get(path string, out any) error {
	return mapAuthError(c.rest.Get(path, out))
}

func mapAuthError(err error) error {
	var he *api.HTTPError
	if errors.As(err, &he) {
		switch he.StatusCode {
		case http.StatusUnauthorized:
			return fmt.Errorf("not authenticated, run `gh auth login`: %w", err)
		case http.StatusForbidden:
			return fmt.Errorf("forbidden, you may need the read:org scope (`gh auth refresh -s read:org`): %w", err)
		}
	}
	return err
}

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

// paginate runs a GraphQL query page by page. fetch receives the cursor
// (nil for the first page) and returns the page info of the connection.
func paginate(fetch func(cursor *string) (pageInfo, error)) error {
	var cursor *string
	for {
		pi, err := fetch(cursor)
		if err != nil {
			return err
		}
		if !pi.HasNextPage {
			return nil
		}
		next := pi.EndCursor
		cursor = &next
	}
}
