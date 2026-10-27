package api

import (
	"net/url"
	"strings"
	"time"
)

// Account is a user as embedded in other resources.
type Account struct {
	UUID        string `json:"uuid,omitempty"`
	AccountID   string `json:"account_id,omitempty"`
	Nickname    string `json:"nickname,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

// Name returns the best human-readable name.
func (a *Account) Name() string {
	if a == nil {
		return ""
	}
	if a.DisplayName != "" {
		return a.DisplayName
	}
	return a.Nickname
}

type href struct {
	Href string `json:"href"`
}

// Links models the subset of "links" we use.
type Links struct {
	HTML href `json:"html"`
}

// CommitRef is {"hash": "..."}.
type CommitRef struct {
	Hash string `json:"hash"`
}

// Content is the {"raw": "..."} body used by comments, tasks and PR summaries.
type Content struct {
	Raw string `json:"raw"`
}

// Time aliases keep struct definitions readable.
type Time = time.Time

func repoPath(workspace, repo string) string {
	return "/repositories/" + url.PathEscape(workspace) + "/" + url.PathEscape(repo)
}

// escapePath escapes each segment of a repository file path.
func escapePath(p string) string {
	segs := strings.Split(strings.Trim(p, "/"), "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}
