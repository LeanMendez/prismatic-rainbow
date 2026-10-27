package diffparse

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The fixture is the real diff of atlassian/amps PR #1255; its diffstat
// (from the API) is the ground truth for paths, statuses and line counts.
func TestParseMatchesBitbucketDiffstat(t *testing.T) {
	raw, err := os.ReadFile("testdata/pr1255.diff")
	if err != nil {
		t.Fatal(err)
	}
	statRaw, err := os.ReadFile("../api/testdata/diffstat.json")
	if err != nil {
		t.Fatal(err)
	}
	var stat struct {
		Values []struct {
			Status       string                 `json:"status"`
			LinesAdded   int                    `json:"lines_added"`
			LinesRemoved int                    `json:"lines_removed"`
			New          *struct{ Path string } `json:"new"`
		} `json:"values"`
	}
	if err := json.Unmarshal(statRaw, &stat); err != nil {
		t.Fatal(err)
	}
	files := Parse(string(raw))
	if len(files) != len(stat.Values) {
		t.Fatalf("parsed %d files, diffstat has %d", len(files), len(stat.Values))
	}
	byPath := map[string]File{}
	for _, f := range files {
		byPath[f.Path] = f
	}
	for _, s := range stat.Values {
		f, ok := byPath[s.New.Path]
		if !ok {
			t.Errorf("missing %s", s.New.Path)
			continue
		}
		if f.Status != s.Status || f.LinesAdded != s.LinesAdded || f.LinesRemoved != s.LinesRemoved {
			t.Errorf("%s: got %s +%d -%d, diffstat %s +%d -%d", f.Path, f.Status, f.LinesAdded, f.LinesRemoved, s.Status, s.LinesAdded, s.LinesRemoved)
		}
	}
	if Join(files) != string(raw) {
		t.Error("Join(Parse(d)) must reproduce the original diff")
	}
}

const special = `diff --git a/old name.txt b/new name.txt
similarity index 100%
rename from old name.txt
rename to new name.txt
diff --git a/img/logo.png b/img/logo.png
Binary files a/img/logo.png and b/img/logo.png differ
diff --git a/gone.go b/gone.go
deleted file mode 100644
index 1111111..0000000
--- a/gone.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package gone
--- a comment line that looks like a header
`

func TestParseRenamesBinaryAndDeletions(t *testing.T) {
	files := Parse(special)
	if len(files) != 3 {
		t.Fatalf("got %d files", len(files))
	}
	if f := files[0]; f.Status != "renamed" || f.Path != "new name.txt" || f.OldPath != "old name.txt" {
		t.Errorf("rename: %+v", f)
	}
	if f := files[1]; !f.Binary || f.Path != "img/logo.png" || f.Status != "modified" {
		t.Errorf("binary: %+v", f)
	}
	if f := files[2]; f.Status != "removed" || f.Path != "gone.go" || f.OldPath != "" || f.LinesRemoved != 2 {
		t.Errorf("deleted: %+v", f)
	}
}

func TestMatchGlob(t *testing.T) {
	for _, tc := range []struct {
		pattern, path string
		want          bool
	}{
		{"*.lock", "web/yarn.lock", true},
		{"src/api", "src/api/client.go", true},
		{"src/api/", "src/api/client.go", true},
		{"src/api", "src/apiary/x.go", false},
		{"src/*.go", "src/a/b.go", false},
		{"src/**/*.go", "src/a/b.go", true},
		{"src/**/*.go", "src/b.go", true},
		{"**/testdata/**", "internal/api/testdata/pr.json", true},
		{"package-lock.json", "frontend/package-lock.json", true},
		{"docs/*.md", "docs/a.md", true},
	} {
		if got := MatchGlob(tc.pattern, tc.path); got != tc.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestFilterIncludeExcludeAndRenames(t *testing.T) {
	files := []File{{Path: "src/a.go"}, {Path: "src/a_test.go"}, {Path: "yarn.lock"}, {Path: "pkg/new.go", OldPath: "src/old.go"}}
	got := Filter(files, []string{"src"}, []string{"*_test.go"})
	var paths []string
	for _, f := range got {
		paths = append(paths, f.Path)
	}
	if strings.Join(paths, ",") != "src/a.go,pkg/new.go" {
		t.Errorf("got %v", paths)
	}
}

func TestLimitLinesKeepsHeaderHeadAndTail(t *testing.T) {
	var b strings.Builder
	b.WriteString("diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1,10 +1,10 @@\n")
	for i := range 9 {
		b.WriteString("+line" + string(rune('0'+i)) + "\n")
	}
	f := File{Path: "x", Patch: b.String()}
	LimitLines(&f, 5) // 10 body lines (hunk header + 9) → keep 3 + 2
	lines := strings.Split(strings.TrimSuffix(f.Patch, "\n"), "\n")
	want := []string{"diff --git a/x b/x", "--- a/x", "+++ b/x", "@@ -1,10 +1,10 @@", "+line0", "+line1",
		"... [5 líneas omitidas por --max-lines-per-file] ...", "+line7", "+line8"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Errorf("got\n%s", f.Patch)
	}
	if !f.Truncated || f.OmittedLines != 5 {
		t.Errorf("truncated=%v omitted=%d", f.Truncated, f.OmittedLines)
	}
}

func TestLimitBytesOmitsWholePatchesThatDoNotFit(t *testing.T) {
	files := []File{{Path: "a", Patch: strings.Repeat("a", 60)}, {Path: "b", Patch: strings.Repeat("b", 60)}, {Path: "c", Patch: strings.Repeat("c", 30)}}
	omitted := LimitBytes(files, 100)
	if strings.Join(omitted, ",") != "b" {
		t.Errorf("omitted = %v, want [b] (c still fits)", omitted)
	}
	if files[1].Patch != "" || !files[1].PatchOmitted || files[2].Patch == "" {
		t.Errorf("files = %+v", files)
	}
}
