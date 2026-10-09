package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// LineRange is an inclusive range of source lines.
type LineRange struct{ Start, End int }

// FileCoverage is the statement coverage of one source file.
type FileCoverage struct {
	Path       string // repo-relative
	Statements int
	Covered    int
	Uncovered  []LineRange // merged, sorted
}

func (f FileCoverage) Percent() float64 { return percent(f.Covered, f.Statements) }
func (f FileCoverage) Missed() int      { return f.Statements - f.Covered }

// Report is the coverage of the whole module.
type Report struct {
	Files      []FileCoverage // counted toward the total, sorted by path
	Excluded   []FileCoverage // matched by the ignore file, sorted by path
	Statements int
	Covered    int
}

func (r Report) Percent() float64 { return percent(r.Covered, r.Statements) }

// StatementsNeeded is how many more statements must be covered to reach threshold.
func (r Report) StatementsNeeded(threshold float64) int {
	need := int(math.Ceil(threshold*float64(r.Statements)/100-1e-9)) - r.Covered
	return max(need, 0)
}

// Impact is how many percentage points the total would rise if f were fully covered.
func (r Report) Impact(f FileCoverage) float64 {
	if r.Statements == 0 {
		return 0
	}
	return 100 * float64(f.Missed()) / float64(r.Statements)
}

func percent(covered, total int) float64 {
	if total == 0 {
		return 100
	}
	return 100 * float64(covered) / float64(total)
}

// formatPct rounds down to one decimal so a value below the threshold never
// displays as equal to it (84.96 shows as 84.9, not 85.0).
func formatPct(p float64) string {
	return strconv.FormatFloat(math.Floor(p*10+1e-9)/10, 'f', 1, 64) + "%"
}

// ---------- cover profile ----------

type block struct {
	file                                 string
	startLine, startCol, endLine, endCol int
	stmts, count                         int
}

// parseProfile reads a `go test -coverprofile` file. Blocks reported more
// than once (e.g. by several test binaries) are merged, keeping the highest count.
func parseProfile(r io.Reader) ([]block, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	merged := map[string]int{} // key -> index into blocks
	var blocks []block
	sawMode := false
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if !sawMode {
			if !strings.HasPrefix(line, "mode:") {
				return nil, errors.New("cover profile must start with a mode line")
			}
			sawMode = true
			continue
		}
		colon := strings.LastIndex(line, ":")
		if colon <= 0 {
			return nil, fmt.Errorf("line %d: malformed block %q", n, line)
		}
		b := block{file: line[:colon]}
		if _, err := fmt.Sscanf(line[colon+1:], "%d.%d,%d.%d %d %d",
			&b.startLine, &b.startCol, &b.endLine, &b.endCol, &b.stmts, &b.count); err != nil {
			return nil, fmt.Errorf("line %d: malformed block %q: %w", n, line, err)
		}
		key := line[:strings.LastIndex(line, " ")]
		if i, ok := merged[key]; ok {
			blocks[i].count = max(blocks[i].count, b.count)
			continue
		}
		merged[key] = len(blocks)
		blocks = append(blocks, b)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !sawMode {
		return nil, errors.New("cover profile is empty")
	}
	return blocks, nil
}

// summarize aggregates blocks per file. module is stripped from import paths
// to produce repo-relative paths; files matched by ig go to Report.Excluded.
func summarize(blocks []block, module string, ig Ignore) Report {
	byFile := map[string]*FileCoverage{}
	for _, b := range blocks {
		rel := strings.TrimPrefix(b.file, module+"/")
		f, ok := byFile[rel]
		if !ok {
			f = &FileCoverage{Path: rel}
			byFile[rel] = f
		}
		f.Statements += b.stmts
		if b.count > 0 {
			f.Covered += b.stmts
		} else if b.stmts > 0 {
			f.Uncovered = append(f.Uncovered, LineRange{b.startLine, b.endLine})
		}
	}

	var r Report
	for _, f := range byFile {
		f.Uncovered = mergeRanges(f.Uncovered)
		if ig.Match(f.Path) {
			r.Excluded = append(r.Excluded, *f)
			continue
		}
		r.Files = append(r.Files, *f)
		r.Statements += f.Statements
		r.Covered += f.Covered
	}
	byPath := func(a, b FileCoverage) int { return strings.Compare(a.Path, b.Path) }
	slices.SortFunc(r.Files, byPath)
	slices.SortFunc(r.Excluded, byPath)
	return r
}

func mergeRanges(rs []LineRange) []LineRange {
	if len(rs) == 0 {
		return nil
	}
	slices.SortFunc(rs, func(a, b LineRange) int { return a.Start - b.Start })
	out := []LineRange{rs[0]}
	for _, r := range rs[1:] {
		last := &out[len(out)-1]
		if r.Start <= last.End+1 {
			last.End = max(last.End, r.End)
			continue
		}
		out = append(out, r)
	}
	return out
}

// ---------- ignore file ----------

// Ignore is the list of patterns from the coverage ignore file.
//
// Each non-empty, non-# line is one of:
//
//	internal/legacy/          a directory (everything below it)
//	internal/legacy           same, or the exact file of that name
//	cmd/server/main.go        an exact file
//	internal/*/mock_*.go      a glob matched against the full path
//	*_gen.go                  a glob without "/" is matched against the file name
type Ignore []string

func parseIgnore(r io.Reader) (Ignore, error) {
	var ig Ignore
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(strings.TrimPrefix(line, "./"), "/")
		if _, err := path.Match(line, ""); err != nil {
			return nil, fmt.Errorf("invalid pattern %q: %w", line, err)
		}
		ig = append(ig, line)
	}
	return ig, sc.Err()
}

// Match reports whether the repo-relative file path p is excluded.
func (ig Ignore) Match(p string) bool {
	for _, pat := range ig {
		dir := strings.TrimSuffix(pat, "/")
		switch {
		case p == pat, strings.HasPrefix(p, dir+"/"):
			return true
		case !strings.Contains(pat, "/"):
			if ok, _ := path.Match(pat, path.Base(p)); ok {
				return true
			}
		default:
			if ok, _ := path.Match(pat, p); ok {
				return true
			}
		}
	}
	return false
}

// ---------- diff ----------

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// parseDiff returns, per file, the line numbers added on the new side of a
// unified diff (as produced by `git diff`).
func parseDiff(r io.Reader) (map[string]map[int]bool, error) {
	added := map[string]map[int]bool{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var file string
	inHunk := false
	newLine := 0
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			file, inHunk = "", false
		case !inHunk && strings.HasPrefix(line, "+++ "):
			file = ""
			if name := strings.TrimPrefix(line, "+++ "); strings.HasPrefix(name, "b/") {
				file = strings.TrimPrefix(name, "b/")
			}
		case strings.HasPrefix(line, "@@"):
			m := hunkHeader.FindStringSubmatch(line)
			if m == nil {
				return nil, fmt.Errorf("malformed hunk header %q", line)
			}
			newLine, _ = strconv.Atoi(m[1])
			inHunk = true
		case !inHunk:
			// other file headers (index, ---, mode changes)
		case strings.HasPrefix(line, "+"):
			if file != "" {
				if added[file] == nil {
					added[file] = map[int]bool{}
				}
				added[file][newLine] = true
			}
			newLine++
		case strings.HasPrefix(line, " "):
			newLine++
		}
	}
	return added, sc.Err()
}

// ---------- review comments ----------

const commentMarker = "<!-- coverage-bot -->"

// ReviewComment is an inline PR comment on the new side of the diff.
type ReviewComment struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	Line      int    `json:"line"`
	Body      string `json:"body"`
}

// uncoveredAdded returns the added lines of f that are not covered, in order.
func uncoveredAdded(f FileCoverage, added map[int]bool) []int {
	var lines []int
	for _, rg := range f.Uncovered {
		for l := rg.Start; l <= rg.End; l++ {
			if added[l] {
				lines = append(lines, l)
			}
		}
	}
	return lines
}

// reviewComments builds one comment per run of consecutive added-but-uncovered
// lines. At most limit comments are returned; the rest are counted as dropped.
func reviewComments(r Report, added map[string]map[int]bool, limit int) (comments []ReviewComment, dropped int) {
	for _, f := range r.Files {
		lines := uncoveredAdded(f, added[f.Path])
		for i := 0; i < len(lines); {
			j := i
			for j+1 < len(lines) && lines[j+1] == lines[j]+1 {
				j++
			}
			if len(comments) == limit {
				dropped++
			} else {
				c := ReviewComment{Path: f.Path, Line: lines[j]}
				what := fmt.Sprintf("Line %d is", lines[i])
				if j > i {
					c.StartLine = lines[i]
					what = fmt.Sprintf("Lines %d–%d are", lines[i], lines[j])
				}
				c.Body = fmt.Sprintf("%s\n⚠️ **Missing coverage:** %s not covered by tests. `%s` is at %s coverage.",
					commentMarker, what, f.Path, formatPct(f.Percent()))
				comments = append(comments, c)
			}
			i = j + 1
		}
	}
	return comments, dropped
}
