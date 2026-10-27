// Package tasklink finds ClickUp task references in pull request text
// (title, branch, description, commit messages, comments) using the same
// conventions ClickUp's Bitbucket integration recognizes:
//
//	CU-<task_id>            e.g. CU-86b1x2y3z (also the default branch format CU-<id>_<name>)
//	#<task_id>              e.g. #86b1x2y3z   (ambiguous with other '#' uses → low confidence)
//	<PREFIX>-<number>       custom task IDs, only for configured prefixes (e.g. ENG-123)
//	https://app.clickup.com/t/<task_id> and /t/<team_id>/<custom_id>
//
// An optional "[status]" suffix (CU-abc[ready]) is ClickUp's syntax for
// changing the task status; it is reported as StatusHint.
package tasklink

import (
	"regexp"
	"strings"
)

// Source kinds, in the order they're scanned.
const (
	SourceTitle       = "title"
	SourceBranch      = "branch"
	SourceDescription = "description"
	SourceCommit      = "commit"
	SourceComment     = "comment"
)

// Kinds of references.
const (
	KindTaskID   = "task_id"
	KindCustomID = "custom_id"
)

// Text is a named piece of text to scan.
type Text struct {
	Source string
	// Ref identifies the item within the source: commit hash or comment ID.
	Ref  string
	Body string
}

// Location says where a reference was found.
type Location struct {
	Source string `json:"source"`
	Ref    string `json:"ref,omitempty"`
	Match  string `json:"match"`
}

// Reference is a deduplicated ClickUp task reference.
type Reference struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Confidence string     `json:"confidence"` // high | low
	StatusHint string     `json:"status_hint,omitempty"`
	URL        string     `json:"url,omitempty"`
	FoundIn    []Location `json:"found_in"`
}

var (
	urlRe = regexp.MustCompile(`app\.clickup\.com/t/(?:(\d+)/)?([A-Za-z0-9]+(?:-\d+)?)`)
	cuRe  = regexp.MustCompile(`(?i)\bCU-([0-9a-z]+)(?:\[([^\]\s]+)\])?`)
	// '#' IDs must not follow a word char, '&' (HTML entities) or '/' (URL fragments).
	hashRe   = regexp.MustCompile(`(?:^|[^\w&/])#([0-9a-z]{6,12})\b(?:\[([^\]\s]+)\])?`)
	letterRe = regexp.MustCompile(`[a-z]`)
	digitRe  = regexp.MustCompile(`[0-9]`)
)

// Extract scans texts in order and returns references in first-seen order.
// customPrefixes enables PREFIX-123 custom task IDs (case-insensitive).
func Extract(texts []Text, customPrefixes []string) []Reference {
	var customRe *regexp.Regexp
	if len(customPrefixes) > 0 {
		quoted := make([]string, 0, len(customPrefixes))
		for _, p := range customPrefixes {
			if p = strings.TrimSpace(p); p != "" {
				quoted = append(quoted, regexp.QuoteMeta(p))
			}
		}
		if len(quoted) > 0 {
			customRe = regexp.MustCompile(`(?i)\b(` + strings.Join(quoted, "|") + `)-(\d+)\b(?:\[([^\]\s]+)\])?`)
		}
	}

	var out []Reference
	index := map[string]int{}
	add := func(id, kind, conf, status string, loc Location) {
		i, ok := index[id]
		if !ok {
			ref := Reference{ID: id, Kind: kind, Confidence: conf}
			if kind == KindTaskID {
				ref.URL = "https://app.clickup.com/t/" + id
			}
			out = append(out, ref)
			i = len(out) - 1
			index[id] = i
		}
		r := &out[i]
		if conf == "high" {
			r.Confidence = "high"
		}
		if r.StatusHint == "" {
			r.StatusHint = status
		}
		for _, l := range r.FoundIn {
			if l == loc {
				return
			}
		}
		r.FoundIn = append(r.FoundIn, loc)
	}

	for _, t := range texts {
		loc := func(m string) Location { return Location{Source: t.Source, Ref: t.Ref, Match: m} }
		for _, m := range urlRe.FindAllStringSubmatch(t.Body, -1) {
			id := m[2]
			if strings.Contains(id, "-") {
				add(strings.ToUpper(id), KindCustomID, "high", "", loc(m[0]))
			} else {
				add(strings.ToLower(id), KindTaskID, "high", "", loc(m[0]))
			}
		}
		for _, m := range cuRe.FindAllStringSubmatch(t.Body, -1) {
			add(strings.ToLower(m[1]), KindTaskID, "high", m[2], loc(m[0]))
		}
		for _, m := range hashRe.FindAllStringSubmatchIndex(t.Body, -1) {
			id := t.Body[m[2]:m[3]]
			if !letterRe.MatchString(id) || !digitRe.MatchString(id) {
				continue
			}
			status := ""
			if m[4] >= 0 {
				status = t.Body[m[4]:m[5]]
			}
			add(id, KindTaskID, "low", status, loc(t.Body[m[2]-1:m[1]]))
		}
		if customRe != nil {
			for _, m := range customRe.FindAllStringSubmatch(t.Body, -1) {
				add(strings.ToUpper(m[1])+"-"+m[2], KindCustomID, "high", m[3], loc(m[0]))
			}
		}
	}
	return out
}
