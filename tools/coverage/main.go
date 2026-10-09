// Command coverage turns a Go cover profile into the reports used by CI:
// COVERAGE.md, the coverage section of the PR description, and inline
// review comments on new lines that tests don't cover.
//
// Usage:
//
//	go run ./tools/coverage -profile coverage.out [flags]
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "coverage:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	fl := flag.NewFlagSet("coverage", flag.ContinueOnError)
	var (
		profile      = fl.String("profile", "coverage.out", "Go cover profile to read")
		ignoreFile   = fl.String("ignore", ".coverageignore", "file listing paths excluded from the total (optional)")
		threshold    = fl.Float64("threshold", 85, "minimum overall coverage percentage")
		module       = fl.String("module", "", "module path (default: read from -gomod)")
		gomod        = fl.String("gomod", "go.mod", "go.mod used to detect the module path")
		reportOut    = fl.String("report", "", "write COVERAGE.md here")
		sectionOut   = fl.String("pr-section", "", "write the PR description section here")
		diffIn       = fl.String("diff", "", "unified diff of the PR, used to find new uncovered lines")
		commentsOut  = fl.String("comments", "", "write inline review comments (JSON) here")
		maxComments  = fl.Int("max-comments", 30, "maximum number of inline review comments")
		githubOutput = fl.String("github-output", "", "append total/below/comments outputs to this file ($GITHUB_OUTPUT)")
	)
	if err := fl.Parse(args); err != nil {
		return err
	}

	if *module == "" {
		m, err := readModulePath(*gomod)
		if err != nil {
			return err
		}
		*module = m
	}

	ig, err := loadIgnore(*ignoreFile)
	if err != nil {
		return err
	}

	pf, err := os.Open(*profile)
	if err != nil {
		return err
	}
	blocks, err := parseProfile(pf)
	_ = pf.Close()
	if err != nil {
		return fmt.Errorf("%s: %w", *profile, err)
	}
	r := summarize(blocks, *module, ig)
	below := r.Percent() < *threshold

	var added map[string]map[int]bool
	if *diffIn != "" {
		df, err := os.Open(*diffIn)
		if err != nil {
			return err
		}
		added, err = parseDiff(df)
		_ = df.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", *diffIn, err)
		}
	}

	// Line comments are only posted when the total is below the threshold.
	comments, dropped := []ReviewComment{}, 0
	if below && added != nil {
		comments, dropped = reviewComments(r, added, *maxComments)
	}

	if *reportOut != "" {
		if err := os.WriteFile(*reportOut, []byte(renderCoverageFile(r, *threshold, *ignoreFile)), 0o644); err != nil {
			return err
		}
	}
	if *sectionOut != "" {
		if err := os.WriteFile(*sectionOut, []byte(renderPRSection(r, *threshold, added, len(comments), dropped)), 0o644); err != nil {
			return err
		}
	}
	if *commentsOut != "" {
		data, err := json.MarshalIndent(comments, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(*commentsOut, data, 0o644); err != nil {
			return err
		}
	}
	if *githubOutput != "" {
		f, err := os.OpenFile(*githubOutput, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, werr := fmt.Fprintf(f, "total=%s\nbelow=%t\ncomments=%d\n",
			strings.TrimSuffix(formatPct(r.Percent()), "%"), below, len(comments))
		if err := errors.Join(werr, f.Close()); err != nil {
			return err
		}
	}

	_, err = io.WriteString(stdout, renderText(r, *threshold))
	return err
}

func readModulePath(gomod string) (string, error) {
	f, err := os.Open(gomod)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if m, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "module "); ok {
			return strings.Trim(strings.TrimSpace(m), `"`), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s: no module directive", gomod)
}

// loadIgnore reads the ignore file; a missing file means nothing is excluded.
func loadIgnore(name string) (Ignore, error) {
	f, err := os.Open(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	ig, err := parseIgnore(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return ig, nil
}
