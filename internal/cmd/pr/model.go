package pr

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/LeanMendez/prismatic-rainbow/internal/api"
	"github.com/LeanMendez/prismatic-rainbow/internal/tasklink"
)

// This file defines the stable JSON contract of the pr commands. Fields
// are normalized (flat names, explicit enums) instead of mirroring the raw
// Bitbucket payloads, so agents can rely on them across API changes.

// RefView is a PR endpoint.
type RefView struct {
	Branch     string `json:"branch"`
	Commit     string `json:"commit,omitempty"`
	Repository string `json:"repository,omitempty"`
}

// ReviewerView is a reviewer/participant and their decision.
type ReviewerView struct {
	Name      string `json:"name"`
	AccountID string `json:"account_id,omitempty"`
	// Role: reviewer | participant.
	Role string `json:"role"`
	// State: approved | changes_requested | pending (reviewer without decision) | commented.
	State string `json:"state"`
}

// ReviewView summarizes the review status.
type ReviewView struct {
	Approvals        int            `json:"approvals"`
	ChangesRequested int            `json:"changes_requested"`
	Reviewers        []ReviewerView `json:"reviewers"`
}

// PRView is the normalized pull request.
type PRView struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// State: OPEN | MERGED | DECLINED | SUPERSEDED.
	State             string     `json:"state"`
	Draft             bool       `json:"draft"`
	URL               string     `json:"url"`
	Author            string     `json:"author"`
	Source            RefView    `json:"source"`
	Destination       RefView    `json:"destination"`
	MergeCommit       string     `json:"merge_commit,omitempty"`
	CloseSourceBranch bool       `json:"close_source_branch"`
	ClosedBy          string     `json:"closed_by,omitempty"`
	Reason            string     `json:"reason,omitempty"`
	CreatedOn         time.Time  `json:"created_on"`
	UpdatedOn         time.Time  `json:"updated_on"`
	CommentCount      int        `json:"comment_count"`
	TaskCount         int        `json:"task_count"`
	Review            ReviewView `json:"review"`
}

func refView(e api.Endpoint) RefView {
	return RefView{Branch: e.Branch.Name, Commit: e.CommitHash(), Repository: e.RepoFullName()}
}

func newPRView(p *api.PullRequest) PRView {
	v := PRView{
		ID: p.ID, Title: p.Title, Description: p.Description, State: p.State, Draft: p.Draft,
		URL: p.Links.HTML.Href, Author: p.Author.Name(),
		Source: refView(p.Source), Destination: refView(p.Destination),
		CloseSourceBranch: p.CloseSourceBranch, ClosedBy: p.ClosedBy.Name(), Reason: p.Reason,
		CreatedOn: p.CreatedOn, UpdatedOn: p.UpdatedOn,
		CommentCount: p.CommentCount, TaskCount: p.TaskCount,
	}
	if p.MergeCommit != nil {
		v.MergeCommit = p.MergeCommit.Hash
	}
	v.Review = newReviewView(p)
	return v
}

func newReviewView(p *api.PullRequest) ReviewView {
	r := ReviewView{Reviewers: []ReviewerView{}}
	seen := map[string]bool{}
	key := func(a api.Account) string {
		if a.UUID != "" {
			return a.UUID
		}
		return a.Name()
	}
	for _, part := range p.Participants {
		state := part.State
		switch {
		case part.Approved || state == "approved":
			state = "approved"
			r.Approvals++
		case state == "changes_requested":
			r.ChangesRequested++
		case part.Role == "REVIEWER":
			state = "pending"
		default:
			state = "commented"
		}
		role := "participant"
		if part.Role == "REVIEWER" {
			role = "reviewer"
		}
		seen[key(part.User)] = true
		r.Reviewers = append(r.Reviewers, ReviewerView{part.User.Name(), part.User.AccountID, role, state})
	}
	// Reviewers that never interacted don't show up in participants.
	for _, a := range p.Reviewers {
		if !seen[key(a)] {
			r.Reviewers = append(r.Reviewers, ReviewerView{a.Name(), a.AccountID, "reviewer", "pending"})
		}
	}
	return r
}

// FileStatView is one changed file (diffstat).
type FileStatView struct {
	Path         string `json:"path"`
	OldPath      string `json:"old_path,omitempty"`
	Status       string `json:"status"`
	LinesAdded   int    `json:"lines_added"`
	LinesRemoved int    `json:"lines_removed"`
}

// FilesView is the diffstat plus totals.
type FilesView struct {
	Count        int            `json:"count"`
	LinesAdded   int            `json:"lines_added"`
	LinesRemoved int            `json:"lines_removed"`
	Files        []FileStatView `json:"files"`
}

func newFilesView(stats []api.DiffStat) FilesView {
	v := FilesView{Files: make([]FileStatView, len(stats))}
	for i, s := range stats {
		fs := FileStatView{Path: s.Path(), Status: s.Status, LinesAdded: s.LinesAdded, LinesRemoved: s.LinesRemoved}
		if old := s.OldPath(); old != "" && old != fs.Path {
			fs.OldPath = old
		}
		v.Files[i] = fs
		v.LinesAdded += s.LinesAdded
		v.LinesRemoved += s.LinesRemoved
	}
	v.Count = len(stats)
	return v
}

// CommitView is a PR commit.
type CommitView struct {
	Hash    string    `json:"hash"`
	Date    time.Time `json:"date"`
	Author  string    `json:"author"`
	Message string    `json:"message"`
	Merge   bool      `json:"merge,omitempty"`
}

func newCommitViews(cs []api.Commit) []CommitView {
	out := make([]CommitView, len(cs))
	for i, c := range cs {
		author := c.Author.Raw
		if c.Author.User != nil {
			author = c.Author.User.Name()
		}
		out[i] = CommitView{c.Hash, c.Date, author, c.Message, len(c.Parents) > 1}
	}
	return out
}

// CommentView is one comment of a thread.
type CommentView struct {
	ID        int       `json:"id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedOn time.Time `json:"created_on"`
	Pending   bool      `json:"pending,omitempty"`
}

// ThreadView is a root comment plus its replies.
type ThreadView struct {
	ID int `json:"id"`
	// Path/Line/FromLine anchor inline threads: Line is in the new file,
	// FromLine in the old file (comment on a removed line).
	Path       string        `json:"path,omitempty"`
	Line       *int          `json:"line,omitempty"`
	FromLine   *int          `json:"from_line,omitempty"`
	Resolved   bool          `json:"resolved"`
	ResolvedBy string        `json:"resolved_by,omitempty"`
	Comments   []CommentView `json:"comments"`
}

// ThreadsView is the threads list plus counts.
type ThreadsView struct {
	Total      int          `json:"total"`
	Unresolved int          `json:"unresolved"`
	Threads    []ThreadView `json:"threads"`
}

func newThreadsView(cs []api.Comment) ThreadsView {
	byID := map[int]api.Comment{}
	for _, c := range cs {
		byID[c.ID] = c
	}
	rootOf := func(c api.Comment) int {
		for c.Parent != nil {
			p, ok := byID[c.Parent.ID]
			if !ok {
				break
			}
			c = p
		}
		return c.ID
	}
	threads := map[int]*ThreadView{}
	var order []int
	for _, c := range cs {
		root := rootOf(c)
		t, ok := threads[root]
		if !ok {
			rc := byID[root]
			t = &ThreadView{ID: root, Comments: []CommentView{}}
			if rc.Inline != nil {
				t.Path, t.Line, t.FromLine = rc.Inline.Path, rc.Inline.To, rc.Inline.From
			}
			if rc.Resolution != nil {
				t.Resolved, t.ResolvedBy = true, rc.Resolution.User.Name()
			}
			threads[root] = t
			order = append(order, root)
		}
		if c.Deleted {
			continue
		}
		t.Comments = append(t.Comments, CommentView{c.ID, c.User.Name(), c.Content.Raw, c.CreatedOn, c.Pending})
	}
	v := ThreadsView{Threads: []ThreadView{}}
	for _, id := range order {
		t := threads[id]
		if len(t.Comments) == 0 {
			continue
		}
		sort.SliceStable(t.Comments, func(i, j int) bool { return t.Comments[i].CreatedOn.Before(t.Comments[j].CreatedOn) })
		v.Threads = append(v.Threads, *t)
		if !t.Resolved {
			v.Unresolved++
		}
	}
	sort.SliceStable(v.Threads, func(i, j int) bool {
		return v.Threads[i].Comments[0].CreatedOn.Before(v.Threads[j].Comments[0].CreatedOn)
	})
	v.Total = len(v.Threads)
	return v
}

// TaskView is a PR task.
type TaskView struct {
	ID         int        `json:"id"`
	State      string     `json:"state"` // UNRESOLVED | RESOLVED
	Body       string     `json:"body"`
	Creator    string     `json:"creator,omitempty"`
	CommentID  int        `json:"comment_id,omitempty"`
	CreatedOn  time.Time  `json:"created_on"`
	ResolvedBy string     `json:"resolved_by,omitempty"`
	ResolvedOn *time.Time `json:"resolved_on,omitempty"`
}

// TasksView is the tasks list plus counts.
type TasksView struct {
	Total      int        `json:"total"`
	Unresolved int        `json:"unresolved"`
	Tasks      []TaskView `json:"tasks"`
}

func newTasksView(ts []api.Task) TasksView {
	v := TasksView{Tasks: make([]TaskView, len(ts)), Total: len(ts)}
	for i, t := range ts {
		tv := TaskView{ID: t.ID, State: t.State, Body: t.Content.Raw, Creator: t.Creator.Name(),
			CreatedOn: t.CreatedOn, ResolvedBy: t.ResolvedBy.Name(), ResolvedOn: t.ResolvedOn}
		if t.Comment != nil {
			tv.CommentID = t.Comment.ID
		}
		if t.State != api.TaskResolved {
			v.Unresolved++
		}
		v.Tasks[i] = tv
	}
	return v
}

// Check buckets, normalized like gh: pass | fail | pending | stopped.
const (
	BucketPass    = "pass"
	BucketFail    = "fail"
	BucketPending = "pending"
	BucketStopped = "stopped"
	BucketNone    = "none"
)

// StatusView is one build status.
type StatusView struct {
	Name        string    `json:"name"`
	Key         string    `json:"key"`
	State       string    `json:"state"`
	Bucket      string    `json:"bucket"`
	Description string    `json:"description,omitempty"`
	URL         string    `json:"url,omitempty"`
	UpdatedOn   time.Time `json:"updated_on"`
}

// ChecksView summarizes build statuses. Overall: fail if any failed, else
// pending if any running, else pass if any passed, else none.
type ChecksView struct {
	Overall  string       `json:"overall"`
	Passed   int          `json:"passed"`
	Failed   int          `json:"failed"`
	Pending  int          `json:"pending"`
	Stopped  int          `json:"stopped"`
	Statuses []StatusView `json:"statuses"`
}

func bucketOf(state string) string {
	switch state {
	case "SUCCESSFUL":
		return BucketPass
	case "FAILED":
		return BucketFail
	case "INPROGRESS":
		return BucketPending
	default:
		return BucketStopped
	}
}

func newChecksView(ss []api.CommitStatus) ChecksView {
	v := ChecksView{Statuses: make([]StatusView, len(ss))}
	for i, s := range ss {
		b := bucketOf(s.State)
		switch b {
		case BucketPass:
			v.Passed++
		case BucketFail:
			v.Failed++
		case BucketPending:
			v.Pending++
		default:
			v.Stopped++
		}
		v.Statuses[i] = StatusView{s.Name, s.Key, s.State, b, s.Description, s.URL, s.UpdatedOn}
	}
	// Failures first so truncated readers see what matters.
	rank := map[string]int{BucketFail: 0, BucketPending: 1, BucketStopped: 2, BucketPass: 3}
	sort.SliceStable(v.Statuses, func(i, j int) bool { return rank[v.Statuses[i].Bucket] < rank[v.Statuses[j].Bucket] })
	switch {
	case v.Failed > 0:
		v.Overall = BucketFail
	case v.Pending > 0:
		v.Overall = BucketPending
	case v.Passed > 0:
		v.Overall = BucketPass
	case v.Stopped > 0:
		v.Overall = BucketStopped
	default:
		v.Overall = BucketNone
	}
	return v
}

// MergeCheckView is one mergeability check.
type MergeCheckView struct {
	// Name is the check kind (minimum_approvals, resolved_tasks,
	// git_mergeability_check, current_user_permission_check, …) or the
	// custom check's display name.
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Status      string          `json:"status"` // PASSED | FAILED | PENDING
	Required    bool            `json:"required"`
	Blocking    bool            `json:"blocking"`
	Message     string          `json:"message,omitempty"`
	Requirement json.RawMessage `json:"requirement,omitempty"`
	Observed    json.RawMessage `json:"observed,omitempty"`
}

// MergeabilityView answers "can this PR be merged, and what blocks it".
// current_user_permission_check reflects the caller's permissions, so it
// is reported but ignored for MergeableIgnoringPermission.
type MergeabilityView struct {
	Mergeable                   bool             `json:"mergeable"`
	MergeableIgnoringPermission bool             `json:"mergeable_ignoring_permission"`
	Blocking                    []string         `json:"blocking"`
	Checks                      []MergeCheckView `json:"checks"`
}

func newMergeabilityView(cs []api.MergeCheck) MergeabilityView {
	v := MergeabilityView{Blocking: []string{}, Checks: make([]MergeCheckView, len(cs))}
	permOnly := true
	for i, c := range cs {
		name := c.Type
		if c.Check != nil {
			if c.Check.Name != "" {
				name = c.Check.Name
			} else if c.Check.Kind != "" {
				name = c.Check.Kind
			}
		}
		msg := c.Message
		if msg == "" {
			msg = c.Reason
		}
		v.Checks[i] = MergeCheckView{name, c.Type, c.Status, c.Required, c.Blocking, msg, c.Requirement, c.Observed}
		if c.Blocking {
			v.Blocking = append(v.Blocking, name)
			if c.Type != "current_user_permission_check" {
				permOnly = false
			}
		}
	}
	v.Mergeable = len(v.Blocking) == 0
	v.MergeableIgnoringPermission = v.Mergeable || permOnly
	return v
}

// prTexts returns the texts scanned for ClickUp references.
func prTexts(p *api.PullRequest, commits []api.Commit, comments []api.Comment) []tasklink.Text {
	texts := []tasklink.Text{
		{Source: tasklink.SourceTitle, Body: p.Title},
		{Source: tasklink.SourceBranch, Body: p.Source.Branch.Name},
		{Source: tasklink.SourceDescription, Body: p.Description},
	}
	for _, c := range commits {
		texts = append(texts, tasklink.Text{Source: tasklink.SourceCommit, Ref: c.Hash, Body: c.Message})
	}
	for _, c := range comments {
		if !c.Deleted {
			texts = append(texts, tasklink.Text{Source: tasklink.SourceComment, Ref: itoa(c.ID), Body: c.Content.Raw})
		}
	}
	return texts
}
