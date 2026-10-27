package api

import (
	"encoding/json"
	"net/http"
)

// Inline anchors a comment to a diff line. To is a line of the new file,
// From a line of the old file (removed lines). Both nil = file-level.
type Inline struct {
	Path string `json:"path"`
	From *int   `json:"from,omitempty"`
	To   *int   `json:"to,omitempty"`
}

// Resolution is set when a comment thread was resolved.
type Resolution struct {
	Type      string  `json:"type"`
	User      Account `json:"user"`
	CreatedOn Time    `json:"created_on"`
}

// Comment is a PR comment.
type Comment struct {
	ID     int `json:"id"`
	Parent *struct {
		ID int `json:"id"`
	} `json:"parent,omitempty"`
	Content    Content     `json:"content"`
	Inline     *Inline     `json:"inline,omitempty"`
	Deleted    bool        `json:"deleted"`
	Pending    bool        `json:"pending"`
	User       Account     `json:"user"`
	CreatedOn  Time        `json:"created_on"`
	UpdatedOn  Time        `json:"updated_on"`
	Resolution *Resolution `json:"resolution,omitempty"`
	Links      Links       `json:"links"`
}

// ListPullRequestComments returns every comment (all pages).
func (c *Client) ListPullRequestComments(ws, repo string, id int) ([]Comment, error) {
	return paginate[Comment](c, prPath(ws, repo, id)+"/comments", 100, 0)
}

// CreateCommentInput describes a new comment. Path "" = general comment.
// Line anchors to the new file; set FromLine for a removed line instead.
type CreateCommentInput struct {
	Body     string
	Path     string
	Line     int
	FromLine int
	ParentID int
}

// CreatePullRequestComment posts a general, inline or reply comment.
func (c *Client) CreatePullRequestComment(ws, repo string, id int, in CreateCommentInput) (*Comment, error) {
	body := map[string]any{"content": Content{Raw: in.Body}}
	if in.Path != "" {
		inline := Inline{Path: in.Path}
		if in.Line > 0 {
			inline.To = &in.Line
		}
		if in.FromLine > 0 {
			inline.From = &in.FromLine
		}
		body["inline"] = inline
	}
	if in.ParentID > 0 {
		body["parent"] = map[string]int{"id": in.ParentID}
	}
	var out Comment
	if err := c.sendJSON(http.MethodPost, prPath(ws, repo, id)+"/comments", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Task states.
const (
	TaskUnresolved = "UNRESOLVED"
	TaskResolved   = "RESOLVED"
)

// Task is a PR task (blocking to-do), optionally attached to a comment.
type Task struct {
	ID         int      `json:"id"`
	State      string   `json:"state"`
	Content    Content  `json:"content"`
	Creator    *Account `json:"creator"`
	CreatedOn  Time     `json:"created_on"`
	ResolvedOn *Time    `json:"resolved_on"`
	ResolvedBy *Account `json:"resolved_by"`
	Comment    *struct {
		ID int `json:"id"`
	} `json:"comment,omitempty"`
}

// ListPullRequestTasks returns every PR task.
func (c *Client) ListPullRequestTasks(ws, repo string, id int) ([]Task, error) {
	return paginate[Task](c, prPath(ws, repo, id)+"/tasks", 100, 0)
}

// CreatePullRequestTask creates a task, linked to commentID when > 0.
func (c *Client) CreatePullRequestTask(ws, repo string, prID int, body string, commentID int) (*Task, error) {
	in := map[string]any{"content": Content{Raw: body}}
	if commentID > 0 {
		in["comment"] = map[string]int{"id": commentID}
	}
	var out Task
	if err := c.sendJSON(http.MethodPost, prPath(ws, repo, prID)+"/tasks", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CommitStatus is a build/check status reported on a commit (Pipelines,
// Jenkins, Bamboo, …). State: SUCCESSFUL, FAILED, INPROGRESS, STOPPED.
type CommitStatus struct {
	Key         string     `json:"key"`
	Name        string     `json:"name"`
	State       string     `json:"state"`
	Description string     `json:"description"`
	URL         string     `json:"url"`
	Refname     string     `json:"refname"`
	CreatedOn   Time       `json:"created_on"`
	UpdatedOn   Time       `json:"updated_on"`
	Commit      *CommitRef `json:"commit"`
}

// ListPullRequestStatuses returns build statuses for the PR source commit.
func (c *Client) ListPullRequestStatuses(ws, repo string, id int) ([]CommitStatus, error) {
	return paginate[CommitStatus](c, prPath(ws, repo, id)+"/statuses", 100, 0)
}

// MergeCheck is one entry of /mergeability/checks: PR state, permission,
// git conflicts, merge checks (approvals, builds, tasks) and custom checks.
type MergeCheck struct {
	Type     string `json:"type"`
	Status   string `json:"status"`
	Required bool   `json:"required"`
	Blocking bool   `json:"blocking"`
	State    string `json:"state,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Message  string `json:"message,omitempty"`
	Check    *struct {
		Kind string `json:"kind,omitempty"`
		Name string `json:"name,omitempty"`
	} `json:"check,omitempty"`
	Requirement json.RawMessage `json:"requirement,omitempty"`
	Observed    json.RawMessage `json:"observed,omitempty"`
}

// GetMergeabilityChecks returns why a PR can or cannot be merged.
func (c *Client) GetMergeabilityChecks(ws, repo string, id int) ([]MergeCheck, error) {
	var out struct {
		Values []MergeCheck `json:"values"`
	}
	if err := c.getJSON(prPath(ws, repo, id)+"/mergeability/checks", &out); err != nil {
		return nil, err
	}
	return out.Values, nil
}
