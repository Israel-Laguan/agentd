// Command checkcommits fails when a commit introduced by the current branch
// (base..HEAD) carries a Co-Authored-By trailer. Commits already on the base
// are never scanned.
//
// It asks git for parsed trailers (%(trailers:key=...)) rather than grepping
// the body, so prose that quotes "Co-Authored-By: ..." at column 0 outside the
// trailer block is not a false positive.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const trailerKey = "Co-Authored-By"

func main() {
	base := flag.String("base", "origin/main", "ref whose history is exempt; only base..HEAD is scanned")
	flag.Parse()
	bad, err := offenders(".", *base)
	if err != nil {
		fmt.Fprintln(os.Stderr, "check-commits:", err)
		os.Exit(2)
	}
	if len(bad) == 0 {
		return
	}
	for _, line := range bad {
		fmt.Fprintf(os.Stderr, "check-commits: %s carries a %s trailer\n", line, trailerKey)
	}
	fmt.Fprintf(os.Stderr, "Remove the trailer (git rebase -i, reword) before pushing. Existing commits on %s are exempt.\n", *base)
	os.Exit(1)
}

// offenders returns "<hash> <value>" for every commit in base..HEAD that has a
// Co-Authored-By trailer, run inside dir.
func offenders(dir, base string) ([]string, error) {
	format := "--format=%x00%h%n%(trailers:key=" + trailerKey + ",valueonly=true)"
	cmd := exec.Command("git", "log", format, base+"..HEAD")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log %s..HEAD: %w: %s", base, err, strings.TrimSpace(stderr.String()))
	}
	return parse(string(out)), nil
}

func parse(out string) []string {
	var bad []string
	for _, rec := range strings.Split(out, "\x00") {
		hash, rest, _ := strings.Cut(rec, "\n")
		if v := strings.TrimSpace(rest); v != "" {
			bad = append(bad, strings.TrimSpace(hash)+" "+strings.Join(strings.Fields(v), " "))
		}
	}
	return bad
}
