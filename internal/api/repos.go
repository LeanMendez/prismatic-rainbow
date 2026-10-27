package api

import (
	"net/url"
	"regexp"
	"strconv"
)

// User is the authenticated account.
type User struct {
	UUID        string `json:"uuid"`
	AccountID   string `json:"account_id"`
	Nickname    string `json:"nickname"`
	DisplayName string `json:"display_name"`
}

// CurrentUser returns the authenticated user (fails for access tokens,
// which have no user identity).
func (c *Client) CurrentUser() (*User, error) {
	var u User
	if err := c.getJSON("/user", &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// Workspace is a Bitbucket workspace.
type Workspace struct {
	UUID string `json:"uuid"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// ListWorkspaces lists workspaces the user can access.
func (c *Client) ListWorkspaces(limit int) ([]Workspace, error) {
	items, err := paginate[struct {
		Workspace Workspace `json:"workspace"`
	}](c, "/user/workspaces", 100, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Workspace, len(items))
	for i, it := range items {
		out[i] = it.Workspace
	}
	return out, nil
}

// ListWorkspaceMembers lists members of a workspace (reviewer lookup).
func (c *Client) ListWorkspaceMembers(ws string) ([]Account, error) {
	items, err := paginate[struct {
		User Account `json:"user"`
	}](c, "/workspaces/"+url.PathEscape(ws)+"/members", 100, 0)
	if err != nil {
		return nil, err
	}
	out := make([]Account, len(items))
	for i, it := range items {
		out[i] = it.User
	}
	return out, nil
}

// Repository is a repository (subset).
type Repository struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	IsPrivate   bool   `json:"is_private"`
	UpdatedOn   Time   `json:"updated_on"`
	Mainbranch  *struct {
		Name string `json:"name"`
	} `json:"mainbranch"`
	Links Links `json:"links"`
}

// MainBranch returns the main branch name or "".
func (r *Repository) MainBranch() string {
	if r.Mainbranch == nil {
		return ""
	}
	return r.Mainbranch.Name
}

// ListRepositories lists repos of a workspace, most recently updated first.
func (c *Client) ListRepositories(ws string, limit int) ([]Repository, error) {
	return paginate[Repository](c, "/repositories/"+url.PathEscape(ws)+"?sort=-updated_on", 100, limit)
}

// GetRepository fetches one repository.
func (c *Client) GetRepository(ws, repo string) (*Repository, error) {
	var r Repository
	if err := c.getJSON(repoPath(ws, repo), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Branch is a branch ref.
type Branch struct {
	Name   string `json:"name"`
	Target struct {
		Hash    string `json:"hash"`
		Date    Time   `json:"date"`
		Message string `json:"message"`
		Author  struct {
			Raw  string   `json:"raw"`
			User *Account `json:"user"`
		} `json:"author"`
	} `json:"target"`
}

// ListBranches lists branches; query is optional BBQL (e.g. name ~ "feat").
func (c *Client) ListBranches(ws, repo, query string, limit int) ([]Branch, error) {
	q := url.Values{"sort": {"-target.date"}}
	if query != "" {
		q.Set("q", query)
	}
	return paginate[Branch](c, withQuery(repoPath(ws, repo)+"/refs/branches", q), 100, limit)
}

var commitHashRe = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// ResolveRef turns a branch/tag name into a commit hash. Hashes are
// returned unchanged. The /src endpoint can't address branch names that
// contain "/", so file reads always go through a hash.
func (c *Client) ResolveRef(ws, repo, ref string) (string, error) {
	if commitHashRe.MatchString(ref) {
		return ref, nil
	}
	var b Branch
	err := c.getJSON(repoPath(ws, repo)+"/refs/branches/"+url.PathEscape(ref), &b)
	if err == nil {
		return b.Target.Hash, nil
	}
	if !IsNotFound(err) {
		return "", err
	}
	if err := c.getJSON(repoPath(ws, repo)+"/refs/tags/"+url.PathEscape(ref), &b); err != nil {
		return "", err
	}
	return b.Target.Hash, nil
}

// SrcEntry is a file or directory in /src listings.
type SrcEntry struct {
	Type     string `json:"type"` // commit_file | commit_directory
	Path     string `json:"path"`
	Size     int64  `json:"size,omitempty"`
	Mimetype string `json:"mimetype,omitempty"`
}

func srcPath(ws, repo, commit, path string) string {
	p := repoPath(ws, repo) + "/src/" + url.PathEscape(commit) + "/"
	if path != "" {
		p += escapePath(path)
	}
	return p
}

// GetFileContent returns the raw bytes of path at commit.
func (c *Client) GetFileContent(ws, repo, commit, path string) ([]byte, error) {
	return c.getText(srcPath(ws, repo, commit, path))
}

// ListDirectory lists path at commit. maxDepth > 1 recurses.
func (c *Client) ListDirectory(ws, repo, commit, path string, maxDepth, limit int) ([]SrcEntry, error) {
	p := srcPath(ws, repo, commit, path)
	if p[len(p)-1] != '/' {
		p += "/"
	}
	if maxDepth > 1 {
		p = withQuery(p, url.Values{"max_depth": {strconv.Itoa(maxDepth)}})
	}
	return paginate[SrcEntry](c, p, 100, limit)
}
