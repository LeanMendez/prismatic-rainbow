package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Pull request states.
const (
	StateOpen       = "OPEN"
	StateMerged     = "MERGED"
	StateDeclined   = "DECLINED"
	StateSuperseded = "SUPERSEDED"
)

// Merge strategies accepted by the merge endpoint.
const (
	MergeCommit = "merge_commit"
	Squash      = "squash"
	FastForward = "fast_forward"
)

// Endpoint is a PR source/destination.
type Endpoint struct {
	Branch struct {
		Name string `json:"name"`
	} `json:"branch"`
	Commit     *CommitRef `json:"commit,omitempty"`
	Repository *struct {
		FullName string `json:"full_name"`
	} `json:"repository,omitempty"`
}

// CommitHash returns the endpoint commit hash or "".
func (e Endpoint) CommitHash() string {
	if e.Commit == nil {
		return ""
	}
	return e.Commit.Hash
}

// RepoFullName returns "workspace/repo" of the endpoint, "" if unknown.
func (e Endpoint) RepoFullName() string {
	if e.Repository == nil {
		return ""
	}
	return e.Repository.FullName
}

// Participant is a reviewer/participant and their review state
// ("approved", "changes_requested" or "").
type Participant struct {
	User           Account `json:"user"`
	Role           string  `json:"role"`
	Approved       bool    `json:"approved"`
	State          string  `json:"state"`
	ParticipatedOn *Time   `json:"participated_on"`
}

// PullRequest mirrors the API resource (subset).
type PullRequest struct {
	ID                int           `json:"id"`
	Title             string        `json:"title"`
	Description       string        `json:"description"`
	State             string        `json:"state"`
	Draft             bool          `json:"draft"`
	Author            Account       `json:"author"`
	Source            Endpoint      `json:"source"`
	Destination       Endpoint      `json:"destination"`
	MergeCommit       *CommitRef    `json:"merge_commit"`
	CloseSourceBranch bool          `json:"close_source_branch"`
	ClosedBy          *Account      `json:"closed_by"`
	Reason            string        `json:"reason"`
	CreatedOn         Time          `json:"created_on"`
	UpdatedOn         Time          `json:"updated_on"`
	CommentCount      int           `json:"comment_count"`
	TaskCount         int           `json:"task_count"`
	Reviewers         []Account     `json:"reviewers"`
	Participants      []Participant `json:"participants"`
	Links             Links         `json:"links"`
}

func prsPath(ws, repo string) string { return repoPath(ws, repo) + "/pullrequests" }

func prPath(ws, repo string, id int) string { return prsPath(ws, repo) + "/" + strconv.Itoa(id) }

// ListPullRequestsOptions filters the PR list.
type ListPullRequestsOptions struct {
	// States: any of OPEN, MERGED, DECLINED, SUPERSEDED. Empty = API default (OPEN).
	States []string
	// Query is a raw BBQL expression (q=), ANDed with nothing else.
	Query string
	Sort  string
	Limit int
}

// ListPullRequests lists PRs of a repository.
func (c *Client) ListPullRequests(ws, repo string, o ListPullRequestsOptions) ([]PullRequest, error) {
	q := url.Values{}
	for _, s := range o.States {
		q.Add("state", s)
	}
	if o.Query != "" {
		q.Set("q", o.Query)
	}
	if o.Sort != "" {
		q.Set("sort", o.Sort)
	}
	return paginate[PullRequest](c, withQuery(prsPath(ws, repo), q), 50, o.Limit)
}

// GetPullRequest fetches one PR.
func (c *Client) GetPullRequest(ws, repo string, id int) (*PullRequest, error) {
	var pr PullRequest
	if err := c.getJSON(prPath(ws, repo, id), &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// DiffOptions tunes the generated diff. Context < 0 keeps the API default.
type DiffOptions struct {
	Context          int
	IgnoreWhitespace bool
}

// GetPullRequestDiff returns the unified diff. The API answers with a 302
// to the commit-range diff (query parameters are carried over), which
// net/http follows.
func (c *Client) GetPullRequestDiff(ws, repo string, id int, o DiffOptions) (string, error) {
	q := url.Values{}
	if o.Context >= 0 {
		q.Set("context", strconv.Itoa(o.Context))
	}
	if o.IgnoreWhitespace {
		q.Set("ignore_whitespace", "true")
	}
	b, err := c.getText(withQuery(prPath(ws, repo, id)+"/diff", q))
	return string(b), err
}

// DiffStat is one changed file. Status: added, removed, modified, renamed,
// merge conflict, …
type DiffStat struct {
	Status       string `json:"status"`
	LinesAdded   int    `json:"lines_added"`
	LinesRemoved int    `json:"lines_removed"`
	Old          *struct {
		Path string `json:"path"`
	} `json:"old"`
	New *struct {
		Path string `json:"path"`
	} `json:"new"`
}

// Path returns the new path, or the old one for deletions.
func (d DiffStat) Path() string {
	if d.New != nil {
		return d.New.Path
	}
	if d.Old != nil {
		return d.Old.Path
	}
	return ""
}

// OldPath returns the old path ("" for additions).
func (d DiffStat) OldPath() string {
	if d.Old != nil {
		return d.Old.Path
	}
	return ""
}

// GetPullRequestDiffStat lists changed files with line counts.
func (c *Client) GetPullRequestDiffStat(ws, repo string, id int) ([]DiffStat, error) {
	return paginate[DiffStat](c, prPath(ws, repo, id)+"/diffstat", 500, 0)
}

// Commit is a repository commit.
type Commit struct {
	Hash    string `json:"hash"`
	Date    Time   `json:"date"`
	Message string `json:"message"`
	Author  struct {
		Raw  string   `json:"raw"`
		User *Account `json:"user"`
	} `json:"author"`
	Parents []CommitRef `json:"parents"`
}

// ListPullRequestCommits lists the PR commits (newest first).
func (c *Client) ListPullRequestCommits(ws, repo string, id, limit int) ([]Commit, error) {
	return paginate[Commit](c, prPath(ws, repo, id)+"/commits", 100, limit)
}

// CreatePullRequestInput is the body of POST /pullrequests.
type CreatePullRequestInput struct {
	Title             string    `json:"title"`
	Description       string    `json:"description,omitempty"`
	Source            Endpoint  `json:"source"`
	Destination       *Endpoint `json:"destination,omitempty"`
	CloseSourceBranch bool      `json:"close_source_branch"`
	Draft             bool      `json:"draft,omitempty"`
	Reviewers         []Account `json:"reviewers,omitempty"`
}

// CreatePullRequest opens a PR.
func (c *Client) CreatePullRequest(ws, repo string, in CreatePullRequestInput) (*PullRequest, error) {
	var pr PullRequest
	if err := c.sendJSON(http.MethodPost, prsPath(ws, repo), in, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// ApprovePullRequest approves (approve=true) or removes the approval.
func (c *Client) ApprovePullRequest(ws, repo string, id int, approve bool) error {
	method := http.MethodPost
	if !approve {
		method = http.MethodDelete
	}
	return c.sendJSON(method, prPath(ws, repo, id)+"/approve", nil, nil)
}

// RequestChanges marks (or un-marks) "changes requested" by the current user.
func (c *Client) RequestChanges(ws, repo string, id int, request bool) error {
	method := http.MethodPost
	if !request {
		method = http.MethodDelete
	}
	return c.sendJSON(method, prPath(ws, repo, id)+"/request-changes", nil, nil)
}

// MergeInput is the body of POST /merge.
type MergeInput struct {
	MergeStrategy     string `json:"merge_strategy,omitempty"`
	CloseSourceBranch bool   `json:"close_source_branch"`
	Message           string `json:"message,omitempty"`
}

// MergePullRequest merges a PR.
func (c *Client) MergePullRequest(ws, repo string, id int, in MergeInput) (*PullRequest, error) {
	var pr PullRequest
	if err := c.sendJSON(http.MethodPost, prPath(ws, repo, id)+"/merge", in, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// FindOpenPullRequestForBranch returns open PRs whose source is branch.
func (c *Client) FindOpenPullRequestForBranch(ws, repo, branch string) ([]PullRequest, error) {
	return c.ListPullRequests(ws, repo, ListPullRequestsOptions{
		States: []string{StateOpen},
		Query:  fmt.Sprintf("source.branch.name=%q", branch),
		Limit:  10,
	})
}
