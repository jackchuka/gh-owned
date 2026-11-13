package github

import (
	"fmt"
	"strings"
	"sync"
)

// Identity is who the viewer counts as for reviews: their login and every team
// they belong to, including ancestor teams, since a child-team member counts
// as a member of the parent team.
type Identity struct {
	Login   string
	Handles map[string]bool // lowercase "@login" and "@org/team"
	TeamIDs map[int64]bool

	mu        sync.Mutex
	teamNames map[int64]string // id -> "@org/team"
}

// Is reports whether the handle ("@login" or "@org/team") is the viewer.
func (id *Identity) Is(handle string) bool {
	return id.Handles[strings.ToLower(handle)]
}

// TeamName returns "@org/team" for a team id, or "team <id>" when unknown.
func (id *Identity) TeamName(teamID int64) string {
	id.mu.Lock()
	defer id.mu.Unlock()
	if n, ok := id.teamNames[teamID]; ok {
		return n
	}
	return fmt.Sprintf("team %d", teamID)
}

// AddTeamNames records team names learned from an org-wide team listing.
func (id *Identity) AddTeamNames(names map[int64]string) {
	id.mu.Lock()
	defer id.mu.Unlock()
	for k, v := range names {
		if _, ok := id.teamNames[k]; !ok {
			id.teamNames[k] = v
		}
	}
}

// HasTeamName reports whether the id already has a known name.
func (id *Identity) HasTeamName(teamID int64) bool {
	id.mu.Lock()
	defer id.mu.Unlock()
	_, ok := id.teamNames[teamID]
	return ok
}

// NewIdentity builds an Identity from a login and team (id, "org/slug") pairs.
func NewIdentity(login string, teams map[int64]string) *Identity {
	id := &Identity{
		Login:     login,
		Handles:   map[string]bool{"@" + strings.ToLower(login): true},
		TeamIDs:   map[int64]bool{},
		teamNames: map[int64]string{},
	}
	for tid, slug := range teams {
		id.Handles["@"+strings.ToLower(slug)] = true
		id.TeamIDs[tid] = true
		id.teamNames[tid] = "@" + slug
	}
	return id
}

const viewerQuery = `query { viewer { login } }`

const ownerTypeQuery = `query($login: String!) { repositoryOwner(login: $login) { __typename } }`

// isOrg reports whether login is an organization (as opposed to a user account).
func (c *Client) isOrg(login string) (bool, error) {
	var resp struct {
		RepositoryOwner *struct {
			Typename string `json:"__typename"`
		} `json:"repositoryOwner"`
	}
	if err := c.query(ownerTypeQuery, map[string]any{"login": login}, &resp); err != nil {
		return false, err
	}
	if resp.RepositoryOwner == nil {
		return false, fmt.Errorf("no organization or user named %s", login)
	}
	return resp.RepositoryOwner.Typename == "Organization", nil
}

const teamsQuery = `
query($org: String!, $login: String!, $cursor: String) {
  organization(login: $org) {
    teams(first: 100, after: $cursor, userLogins: [$login]) {
      pageInfo { hasNextPage endCursor }
      nodes {
        combinedSlug databaseId
        ancestors(first: 20) { nodes { combinedSlug databaseId } }
      }
    }
  }
}`

type teamNode struct {
	CombinedSlug string `json:"combinedSlug"`
	DatabaseID   int64  `json:"databaseId"`
}

// LoadIdentity fetches the viewer and their teams (with ancestors) in the
// given owners; user accounts have no teams and are skipped.
func (c *Client) LoadIdentity(orgs []string) (*Identity, error) {
	var v struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
	}
	if err := c.query(viewerQuery, nil, &v); err != nil {
		return nil, err
	}
	teams := map[int64]string{}
	for _, org := range orgs {
		ok, err := c.isOrg(org)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		err = paginate(func(cursor *string) (pageInfo, error) {
			var resp struct {
				Organization struct {
					Teams struct {
						PageInfo pageInfo `json:"pageInfo"`
						Nodes    []struct {
							teamNode
							Ancestors struct {
								Nodes []teamNode `json:"nodes"`
							} `json:"ancestors"`
						} `json:"nodes"`
					} `json:"teams"`
				} `json:"organization"`
			}
			vars := map[string]any{"org": org, "login": v.Viewer.Login, "cursor": cursor}
			if err := c.query(teamsQuery, vars, &resp); err != nil {
				return pageInfo{}, fmt.Errorf("teams of %s: %w", org, err)
			}
			for _, t := range resp.Organization.Teams.Nodes {
				teams[t.DatabaseID] = t.CombinedSlug
				for _, a := range t.Ancestors.Nodes {
					teams[a.DatabaseID] = a.CombinedSlug
				}
			}
			return resp.Organization.Teams.PageInfo, nil
		})
		if err != nil {
			return nil, err
		}
	}
	return NewIdentity(v.Viewer.Login, teams), nil
}

const orgTeamsQuery = `
query($org: String!, $cursor: String) {
  organization(login: $org) {
    teams(first: 100, after: $cursor) {
      pageInfo { hasNextPage endCursor }
      nodes { combinedSlug databaseId }
    }
  }
}`

// OrgTeamNames lists every team visible to the viewer in org as id -> "@org/team".
// Secret teams the viewer is not in stay invisible.
func (c *Client) OrgTeamNames(org string) (map[int64]string, error) {
	names := map[int64]string{}
	err := paginate(func(cursor *string) (pageInfo, error) {
		var resp struct {
			Organization struct {
				Teams struct {
					PageInfo pageInfo   `json:"pageInfo"`
					Nodes    []teamNode `json:"nodes"`
				} `json:"teams"`
			} `json:"organization"`
		}
		if err := c.query(orgTeamsQuery, map[string]any{"org": org, "cursor": cursor}, &resp); err != nil {
			return pageInfo{}, err
		}
		for _, t := range resp.Organization.Teams.Nodes {
			names[t.DatabaseID] = "@" + t.CombinedSlug
		}
		return resp.Organization.Teams.PageInfo, nil
	})
	return names, err
}

const enterpriseQuery = `
query($slug: String!, $cursor: String) {
  enterprise(slug: $slug) {
    organizations(first: 100, after: $cursor) {
      pageInfo { hasNextPage endCursor }
      nodes { login viewerIsAMember }
    }
  }
}`

// EnterpriseOrgs lists the enterprise's orgs the viewer is a member of.
func (c *Client) EnterpriseOrgs(slug string) ([]string, error) {
	var orgs []string
	err := paginate(func(cursor *string) (pageInfo, error) {
		var resp struct {
			Enterprise struct {
				Organizations struct {
					PageInfo pageInfo `json:"pageInfo"`
					Nodes    []struct {
						Login           string `json:"login"`
						ViewerIsAMember bool   `json:"viewerIsAMember"`
					} `json:"nodes"`
				} `json:"organizations"`
			} `json:"enterprise"`
		}
		if err := c.query(enterpriseQuery, map[string]any{"slug": slug, "cursor": cursor}, &resp); err != nil {
			return pageInfo{}, fmt.Errorf("enterprise %s (pass --org instead if you cannot read it): %w", slug, err)
		}
		for _, o := range resp.Enterprise.Organizations.Nodes {
			if o.ViewerIsAMember {
				orgs = append(orgs, o.Login)
			}
		}
		return resp.Enterprise.Organizations.PageInfo, nil
	})
	return orgs, err
}
