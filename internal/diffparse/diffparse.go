// Package diffparse splits a unified git diff into per-file sections and
// provides path filtering and size limiting so agents can request only the
// part of a PR diff they need without blowing their context window.
package diffparse

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// File is one file section of a unified diff.
type File struct {
	Path         string `json:"path"`
	OldPath      string `json:"old_path,omitempty"`
	Status       string `json:"status"` // added | removed | modified | renamed
	Binary       bool   `json:"binary,omitempty"`
	LinesAdded   int    `json:"lines_added"`
	LinesRemoved int    `json:"lines_removed"`
	Patch        string `json:"patch"`
	// Truncated is true when Patch was shortened; OmittedLines says how
	// many patch lines were dropped. PatchOmitted means the whole patch was
	// dropped to respect a total byte budget.
	Truncated    bool `json:"truncated,omitempty"`
	OmittedLines int  `json:"omitted_lines,omitempty"`
	PatchOmitted bool `json:"patch_omitted,omitempty"`
}

// Parse splits diff into files. Text before the first "diff --git" is ignored.
func Parse(diff string) []File {
	var files []File
	lines := strings.SplitAfter(diff, "\n")
	start := -1
	flush := func(end int) {
		if start >= 0 {
			files = append(files, parseFile(lines[start:end]))
		}
	}
	for i, l := range lines {
		if strings.HasPrefix(l, "diff --git ") {
			flush(i)
			start = i
		}
	}
	flush(len(lines))
	return files
}

func parseFile(lines []string) File {
	f := File{Status: "modified", Patch: strings.Join(lines, "")}
	var oldP, newP string
	inHunks := false
	for _, raw := range lines {
		l := strings.TrimRight(raw, "\r\n")
		if !inHunks {
			switch {
			case strings.HasPrefix(l, "@@"):
				inHunks = true
			case strings.HasPrefix(l, "--- "):
				oldP = stripPrefix(l[4:], "a/")
			case strings.HasPrefix(l, "+++ "):
				newP = stripPrefix(l[4:], "b/")
			case strings.HasPrefix(l, "rename from "):
				oldP, f.Status = l[len("rename from "):], "renamed"
			case strings.HasPrefix(l, "rename to "):
				newP, f.Status = l[len("rename to "):], "renamed"
			case strings.HasPrefix(l, "new file mode"):
				f.Status = "added"
			case strings.HasPrefix(l, "deleted file mode"):
				f.Status = "removed"
			case strings.HasPrefix(l, "Binary files ") || l == "GIT binary patch":
				f.Binary = true
			}
			continue
		}
		switch {
		case strings.HasPrefix(l, "+"):
			f.LinesAdded++
		case strings.HasPrefix(l, "-"):
			f.LinesRemoved++
		}
	}
	if oldP == "" && newP == "" {
		oldP, newP = pathsFromGitHeader(strings.TrimRight(lines[0], "\r\n"))
	}
	if oldP == "/dev/null" {
		oldP, f.Status = "", "added"
	}
	if newP == "/dev/null" {
		newP, f.Status = "", "removed"
	}
	f.Path = newP
	if f.Path == "" {
		f.Path = oldP
	}
	if oldP != f.Path {
		f.OldPath = oldP
	}
	return f
}

func stripPrefix(p, prefix string) string {
	p = strings.TrimSuffix(p, "\t")
	if i := strings.IndexByte(p, '\t'); i >= 0 {
		p = p[:i]
	}
	return strings.TrimPrefix(p, prefix)
}

// pathsFromGitHeader handles "diff --git a/X b/Y" for sections without
// ---/+++ lines (binary files, pure renames, mode changes).
func pathsFromGitHeader(l string) (string, string) {
	rest := strings.TrimPrefix(l, "diff --git ")
	// Same path on both sides is the common case: "a/P b/P".
	if n := len(rest); n%2 == 1 {
		half := (n - 1) / 2
		a, b := rest[:half], rest[half+1:]
		if strings.HasPrefix(a, "a/") && strings.HasPrefix(b, "b/") && a[2:] == b[2:] {
			return a[2:], b[2:]
		}
	}
	if i := strings.LastIndex(rest, " b/"); i >= 0 {
		return strings.TrimPrefix(rest[:i], "a/"), rest[i+3:]
	}
	return rest, rest
}

// MatchGlob reports whether p matches pattern. Supports *, ?, ** and
// directory prefixes ("src/" or "src"). Patterns without "/" also match
// against the base name, so "*.lock" matches "a/b/yarn.lock".
func MatchGlob(pattern, p string) bool {
	pattern = strings.TrimPrefix(pattern, "./")
	if pattern == "" {
		return false
	}
	if p == pattern || strings.HasPrefix(p, strings.TrimSuffix(pattern, "/")+"/") {
		return true
	}
	re := globRegexp(pattern)
	if re.MatchString(p) {
		return true
	}
	return !strings.Contains(pattern, "/") && re.MatchString(path.Base(p))
}

func globRegexp(g string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(g); i++ {
		switch c := g[i]; c {
		case '*':
			if i+1 < len(g) && g[i+1] == '*' {
				i++
				if i+1 < len(g) && g[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

// Filter keeps files matching any include pattern (all when empty) and
// no exclude pattern.
func Filter(files []File, include, exclude []string) []File {
	out := files[:0:0]
	for _, f := range files {
		if matchesAny(include, f) && !(len(exclude) > 0 && matchesAny(exclude, f)) {
			out = append(out, f)
		}
	}
	return out
}

func matchesAny(patterns []string, f File) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if MatchGlob(p, f.Path) || (f.OldPath != "" && MatchGlob(p, f.OldPath)) {
			return true
		}
	}
	return false
}

// LimitLines shortens the patch to maxLines body lines (header lines are
// always kept), keeping the first 60% and the last 40% around a marker.
func LimitLines(f *File, maxLines int) {
	if maxLines <= 0 {
		return
	}
	lines := strings.SplitAfter(f.Patch, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	body := 0
	for body < len(lines) && !strings.HasPrefix(lines[body], "@@") {
		body++
	}
	header, hunks := lines[:body], lines[body:]
	if len(hunks) <= maxLines {
		return
	}
	head := maxLines * 6 / 10
	tail := maxLines - head
	omitted := len(hunks) - head - tail
	var b strings.Builder
	for _, l := range header {
		b.WriteString(l)
	}
	for _, l := range hunks[:head] {
		b.WriteString(l)
	}
	fmt.Fprintf(&b, "... [%d líneas omitidas por --max-lines-per-file] ...\n", omitted)
	for _, l := range hunks[len(hunks)-tail:] {
		b.WriteString(l)
	}
	f.Patch = b.String()
	f.Truncated = true
	f.OmittedLines += omitted
}

// LimitBytes drops whole patches (in order) once the cumulative patch size
// would exceed maxBytes. It returns the paths whose patch was omitted.
func LimitBytes(files []File, maxBytes int) []string {
	if maxBytes <= 0 {
		return nil
	}
	var omitted []string
	total := 0
	for i := range files {
		if total+len(files[i].Patch) > maxBytes {
			files[i].Patch = ""
			files[i].PatchOmitted = true
			omitted = append(omitted, files[i].Path)
			continue
		}
		total += len(files[i].Patch)
	}
	return omitted
}

// Join concatenates the patches back into a unified diff.
func Join(files []File) string {
	var b strings.Builder
	for _, f := range files {
		if f.PatchOmitted {
			fmt.Fprintf(&b, "diff --git a/%s b/%s\n... [patch omitido por --max-bytes: +%d -%d] ...\n", f.Path, f.Path, f.LinesAdded, f.LinesRemoved)
			continue
		}
		b.WriteString(f.Patch)
		if !strings.HasSuffix(f.Patch, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}
