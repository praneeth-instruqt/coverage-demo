package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const mod = "example.com/m"

// profile: a.go 4/6 covered (lines 10-12 and 20 uncovered), b.go fully covered,
// gen/x.go 0/2. The duplicated a.go block (count 0 then 3) must merge as covered.
const testProfile = `mode: atomic
example.com/m/a.go:1.1,5.2 2 1
example.com/m/a.go:10.1,11.5 1 0
example.com/m/a.go:12.1,12.9 1 0
example.com/m/a.go:20.1,20.9 1 0
example.com/m/a.go:30.1,31.2 1 0
example.com/m/a.go:30.1,31.2 1 3
example.com/m/b.go:1.1,2.2 3 5
example.com/m/gen/x.go:1.1,3.2 2 0
`

func mustReport(t *testing.T, ig Ignore) Report {
	t.Helper()
	blocks, err := parseProfile(strings.NewReader(testProfile))
	if err != nil {
		t.Fatal(err)
	}
	return summarize(blocks, mod, ig)
}

func TestParseProfileErrors(t *testing.T) {
	for name, in := range map[string]string{
		"empty":        "",
		"no mode":      "example.com/m/a.go:1.1,2.2 1 1\n",
		"no colon":     "mode: set\ngarbage\n",
		"bad numbers":  "mode: set\na.go:x.1,2.2 1 1\n",
		"short fields": "mode: set\na.go:1.1,2.2 1\n",
	} {
		if _, err := parseProfile(strings.NewReader(in)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestSummarize(t *testing.T) {
	r := mustReport(t, Ignore{"gen/"})

	want := []FileCoverage{
		{Path: "a.go", Statements: 6, Covered: 3, Uncovered: []LineRange{{10, 12}, {20, 20}}},
		{Path: "b.go", Statements: 3, Covered: 3},
	}
	if !reflect.DeepEqual(r.Files, want) {
		t.Errorf("files =\n%+v\nwant\n%+v", r.Files, want)
	}
	if len(r.Excluded) != 1 || r.Excluded[0].Path != "gen/x.go" {
		t.Errorf("excluded = %+v", r.Excluded)
	}
	if r.Statements != 9 || r.Covered != 6 {
		t.Errorf("total = %d/%d, want 6/9", r.Covered, r.Statements)
	}

	all := mustReport(t, nil)
	if all.Statements != 11 || len(all.Excluded) != 0 {
		t.Errorf("without ignore: %d statements, %d excluded", all.Statements, len(all.Excluded))
	}
}

func TestPercentHelpers(t *testing.T) {
	r := mustReport(t, Ignore{"gen/"}) // 6/9 = 66.67%
	if got := formatPct(r.Percent()); got != "66.6%" {
		t.Errorf("formatPct = %s (must round down)", got)
	}
	if got := formatPct(84.96); got != "84.9%" {
		t.Errorf("formatPct(84.96) = %s", got)
	}
	if got := formatPct(85); got != "85.0%" {
		t.Errorf("formatPct(85) = %s", got)
	}
	if n := r.StatementsNeeded(85); n != 2 { // ceil(7.65)=8 - 6
		t.Errorf("needed = %d, want 2", n)
	}
	if n := r.StatementsNeeded(50); n != 0 {
		t.Errorf("needed = %d, want 0", n)
	}
	if got := r.Impact(r.Files[0]); got < 33.3 || got > 33.4 { // 3 missed / 9
		t.Errorf("impact = %v", got)
	}
	if (Report{}).Percent() != 100 || (Report{}).Impact(FileCoverage{Statements: 1}) != 0 {
		t.Error("empty report should be 100% with zero impact")
	}
}

func TestMergeRanges(t *testing.T) {
	got := mergeRanges([]LineRange{{20, 22}, {1, 3}, {4, 5}, {21, 25}, {30, 30}})
	want := []LineRange{{1, 5}, {20, 25}, {30, 30}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if mergeRanges(nil) != nil {
		t.Error("nil in, nil out")
	}
}

func TestIgnore(t *testing.T) {
	ig, err := parseIgnore(strings.NewReader(`
# comment
./internal/legacy/
/cmd/server/main.go
internal/*/mock_*.go
*_gen.go
tools
`))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{
		"internal/legacy/a.go":       true,
		"internal/legacy/sub/b.go":   true,
		"internal/legacyx/a.go":      false,
		"cmd/server/main.go":         true,
		"cmd/server/other.go":        false,
		"internal/api/mock_store.go": true,
		"internal/api/sub/mock_x.go": false,
		"internal/todo/todo_gen.go":  true,
		"tools/coverage/main.go":     true,
		"internal/todo/todo.go":      false,
	}
	for p, want := range cases {
		if got := ig.Match(p); got != want {
			t.Errorf("Match(%q) = %v, want %v", p, got, want)
		}
	}

	if _, err := parseIgnore(strings.NewReader("[bad\n")); err == nil {
		t.Error("expected error for invalid glob")
	}
}

const testDiff = `diff --git a/a.go b/a.go
index 111..222 100644
--- a/a.go
+++ b/a.go
@@ -9,0 +10,3 @@ func x() {
+	one()
+	two()
+	three()
@@ -18,2 +21,3 @@ func y() {
 	ctx
-	old
+	new
+++weird line that starts with plus signs
\ No newline at end of file
diff --git a/gone.go b/gone.go
--- a/gone.go
+++ /dev/null
@@ -1,1 +0,0 @@
-package gone
`

func TestParseDiff(t *testing.T) {
	added, err := parseDiff(strings.NewReader(testDiff))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]map[int]bool{
		"a.go": {10: true, 11: true, 12: true, 22: true, 23: true},
	}
	if !reflect.DeepEqual(added, want) {
		t.Errorf("added = %v, want %v", added, want)
	}

	if _, err := parseDiff(strings.NewReader("diff --git a/x b/x\n+++ b/x\n@@ nonsense\n")); err == nil {
		t.Error("expected error for malformed hunk header")
	}
}

func TestReviewComments(t *testing.T) {
	r := mustReport(t, Ignore{"gen/"})
	added := map[string]map[int]bool{
		"a.go": {10: true, 11: true, 12: true, 15: true, 20: true},
		"b.go": {1: true},
	}

	comments, dropped := reviewComments(r, added, 10)
	if dropped != 0 || len(comments) != 2 {
		t.Fatalf("comments = %+v, dropped = %d", comments, dropped)
	}
	if c := comments[0]; c.Path != "a.go" || c.StartLine != 10 || c.Line != 12 || !strings.Contains(c.Body, "Lines 10–12 are") {
		t.Errorf("multi-line comment = %+v", c)
	}
	if c := comments[1]; c.StartLine != 0 || c.Line != 20 || !strings.Contains(c.Body, "Line 20 is") {
		t.Errorf("single-line comment = %+v", c)
	}
	for _, c := range comments {
		if !strings.HasPrefix(c.Body, commentMarker) || !strings.Contains(c.Body, "`a.go` is at 50.0% coverage") {
			t.Errorf("body missing marker or file coverage: %q", c.Body)
		}
	}

	comments, dropped = reviewComments(r, added, 1)
	if len(comments) != 1 || dropped != 1 {
		t.Errorf("limit: %d comments, %d dropped", len(comments), dropped)
	}
}

func TestRenderCoverageFile(t *testing.T) {
	out := renderCoverageFile(mustReport(t, Ignore{"gen/"}), 85, ".coverageignore")
	for _, s := range []string{
		"**Overall: 66.6%** (6/9 statements) — ❌ below the 85.0% threshold",
		"| `a.go` | 6 | 3 | 50.0% |",
		"| `b.go` | 3 | 3 | 100.0% |",
		"## Excluded via `.coverageignore`",
		"| `gen/x.go` | 2 | 0 | 0.0% |",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in:\n%s", s, out)
		}
	}
	if out := renderCoverageFile(mustReport(t, nil), 50, ".coverageignore"); strings.Contains(out, "## Excluded") || !strings.Contains(out, "✅") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestRenderPRSection(t *testing.T) {
	r := mustReport(t, Ignore{"gen/"})
	added := map[string]map[int]bool{"a.go": {10: true, 11: true}}

	out := renderPRSection(r, 85, added, 3, 2)
	for _, s := range []string{
		sectionStart, sectionEnd,
		"### Files changed in this PR",
		"| `a.go` | 50.0% | 2 |",
		"### What is pulling overall coverage down",
		"2 more statements need tests to reach 85.0%",
		"| `a.go` | 50.0% | 3 | +33.3 pp |",
		"3 inline review comment(s). 2 more were not posted",
		"Excluded from the total",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in:\n%s", s, out)
		}
	}
	if strings.Contains(out, "| `b.go` | 100.0% | 0 | +0.0 pp |") {
		t.Error("fully covered files must not appear in the impact table")
	}

	// Passing threshold, no diff: no impact table, no changed-files table.
	out = renderPRSection(r, 50, nil, 0, 0)
	if strings.Contains(out, "pulling overall") || strings.Contains(out, "Files changed") {
		t.Errorf("unexpected sections:\n%s", out)
	}

	// Diff touching no covered files.
	if out := renderPRSection(r, 50, map[string]map[int]bool{}, 0, 0); !strings.Contains(out, "No covered source files changed") {
		t.Errorf("missing empty-diff note:\n%s", out)
	}
}

func TestRenderPRSectionCapsImpactRows(t *testing.T) {
	var r Report
	for i := range maxImpactRows + 5 {
		r.Files = append(r.Files, FileCoverage{Path: string(rune('a'+i)) + ".go", Statements: 2, Covered: 1})
	}
	r.Statements, r.Covered = 2*len(r.Files), len(r.Files)
	if rows := strings.Count(renderPRSection(r, 85, nil, 0, 0), "pp |"); rows != maxImpactRows {
		t.Errorf("impact rows = %d, want %d", rows, maxImpactRows)
	}
}

func TestRenderText(t *testing.T) {
	out := renderText(mustReport(t, Ignore{"gen/"}), 85)
	for _, s := range []string{"a.go", "50.0%", "gen/x.go", "(excluded)", "TOTAL", "66.6%"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in:\n%s", s, out)
		}
	}
}

// ---------- run (end to end) ----------

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunEndToEnd(t *testing.T) {
	dir := t.TempDir()
	gomod := writeFile(t, dir, "go.mod", "module \"example.com/m\"\n\ngo 1.25\n")
	profile := writeFile(t, dir, "coverage.out", testProfile)
	ignore := writeFile(t, dir, ".coverageignore", "gen/\n")
	diff := writeFile(t, dir, "pr.diff", testDiff)
	report := filepath.Join(dir, "COVERAGE.md")
	section := filepath.Join(dir, "section.md")
	commentsPath := filepath.Join(dir, "comments.json")
	ghOut := filepath.Join(dir, "gh_output")

	var stdout strings.Builder
	err := run([]string{
		"-profile", profile, "-gomod", gomod, "-ignore", ignore, "-threshold", "85",
		"-report", report, "-pr-section", section, "-diff", diff,
		"-comments", commentsPath, "-github-output", ghOut,
	}, &stdout)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(stdout.String(), "TOTAL") {
		t.Errorf("stdout = %q", stdout.String())
	}
	for _, p := range []string{report, section} {
		if b, _ := os.ReadFile(p); !strings.Contains(string(b), "66.6%") {
			t.Errorf("%s missing total:\n%s", p, b)
		}
	}
	var comments []ReviewComment
	b, _ := os.ReadFile(commentsPath)
	if err := json.Unmarshal(b, &comments); err != nil || len(comments) != 1 || comments[0].Line != 12 {
		t.Errorf("comments = %s (%v)", b, err)
	}
	if b, _ := os.ReadFile(ghOut); string(b) != "total=66.6\nbelow=true\ncomments=1\n" {
		t.Errorf("github output = %q", b)
	}
}

func TestRunAboveThresholdWritesNoComments(t *testing.T) {
	dir := t.TempDir()
	commentsPath := filepath.Join(dir, "comments.json")
	err := run([]string{
		"-profile", writeFile(t, dir, "c.out", testProfile),
		"-module", mod, "-ignore", filepath.Join(dir, "missing"), "-threshold", "10",
		"-diff", writeFile(t, dir, "d", testDiff), "-comments", commentsPath,
	}, &strings.Builder{})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(commentsPath); string(b) != "[]" {
		t.Errorf("comments = %s, want []", b)
	}
}

func TestRunErrors(t *testing.T) {
	dir := t.TempDir()
	profile := writeFile(t, dir, "c.out", testProfile)
	badProfile := writeFile(t, dir, "bad.out", "nope")
	noModule := writeFile(t, dir, "go.mod", "go 1.25\n")
	badIgnore := writeFile(t, dir, "ignore", "[bad\n")
	badDiff := writeFile(t, dir, "bad.diff", "diff --git a/x b/x\n@@ broken\n")
	missing := filepath.Join(dir, "missing")
	noDir := filepath.Join(dir, "no", "such", "dir", "out")

	cases := map[string][]string{
		"bad flag":            {"-nope"},
		"missing go.mod":      {"-profile", profile, "-gomod", missing},
		"no module directive": {"-profile", profile, "-gomod", noModule},
		"bad ignore":          {"-profile", profile, "-module", mod, "-ignore", badIgnore},
		"ignore is a dir":     {"-profile", profile, "-module", mod, "-ignore", dir},
		"missing profile":     {"-profile", missing, "-module", mod},
		"bad profile":         {"-profile", badProfile, "-module", mod},
		"missing diff":        {"-profile", profile, "-module", mod, "-diff", missing},
		"bad diff":            {"-profile", profile, "-module", mod, "-diff", badDiff},
		"report unwritable":   {"-profile", profile, "-module", mod, "-report", noDir},
		"section unwritable":  {"-profile", profile, "-module", mod, "-pr-section", noDir},
		"comments unwritable": {"-profile", profile, "-module", mod, "-comments", noDir},
		"output unwritable":   {"-profile", profile, "-module", mod, "-github-output", noDir},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if err := run(append([]string{"-ignore", missing}, args...), &strings.Builder{}); err == nil {
				t.Error("expected error")
			}
		})
	}
}
