package models

import (
	"fmt"
	"regexp"
	"strings"
)

// Tasks run as independent shell commands, so a task that reads a file another
// task produces must wait for it. The planner prompt asks for depends_on, but a
// model can ignore that; Validate enforces it from the task text (B-038).
var (
	planFileNamePattern = regexp.MustCompile(`[A-Za-z0-9_][A-Za-z0-9_.-]*\.(?:txt|md|json|csv|log|out|ya?ml)\b`)
	// planWriteCuePattern matches the words right before a file name that say the
	// task writes it: a redirect, tee, or "save/write/output ... to".
	planWriteCuePattern = regexp.MustCompile("(?i)(?:>>?\\s*|\\btee\\s+(?:-a\\s+)?|" +
		`\b(?:save|saves|saved|saving|write|writes|wrote|writing|output|outputs|redirect|redirects|redirected|store|stores|stored|dump|dumps|capture|captures)\b[^\n]{0,60}\b(?:to|into|in|as)\s+(?:the\s+)?(?:file\s+(?:named|called)\s+)?)` +
		"[`'\"]?$")
)

const planCueWindow = 100

// planFileUse records which files a task writes and which it only mentions.
type planFileUse struct {
	writes map[string]bool
	reads  map[string]bool
}

func scanPlanFileUse(text string) planFileUse {
	use := planFileUse{writes: map[string]bool{}, reads: map[string]bool{}}
	prevEnd := 0
	for _, loc := range planFileNamePattern.FindAllStringIndex(text, -1) {
		start := max(prevEnd, loc[0]-planCueWindow)
		name := strings.ToLower(text[loc[0]:loc[1]])
		if planWriteCuePattern.MatchString(text[start:loc[0]]) {
			use.writes[name] = true
		} else {
			use.reads[name] = true
		}
		prevEnd = loc[1]
	}
	return use
}

// ensureFileDependencies adds a depends_on edge from every task that only reads
// a file to each task that writes it, giving producers a ref_id when they lack
// one. It fails when the implied edge would close a cycle.
func (p *DraftPlan) ensureFileDependencies() error {
	uses := make([]planFileUse, len(p.Tasks))
	for i, t := range p.Tasks {
		uses[i] = scanPlanFileUse(t.Title + "\n" + t.Description)
	}
	for c := range p.Tasks {
		for file := range uses[c].reads {
			if uses[c].writes[file] {
				continue
			}
			for prod := range p.Tasks {
				if prod == c || !uses[prod].writes[file] {
					continue
				}
				if err := p.addFileDependency(c, prod, file); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (p *DraftPlan) addFileDependency(consumer, producer int, file string) error {
	id := p.ensureTaskID(producer)
	if containsDep(p.Tasks[consumer].DependsOn, id) {
		return nil
	}
	if p.dependsTransitively(producer, p.Tasks[consumer].ID()) {
		return fmt.Errorf("task %d reads %s written by task %d, but task %d already depends on task %d: dependency cycle", consumer+1, file, producer+1, producer+1, consumer+1)
	}
	p.Tasks[consumer].DependsOn = append(p.Tasks[consumer].DependsOn, id)
	return nil
}

// ensureTaskID returns the task's draft-local id, minting a unique one if absent.
func (p *DraftPlan) ensureTaskID(idx int) string {
	if id := p.Tasks[idx].ID(); id != "" {
		return id
	}
	taken := map[string]bool{}
	for _, t := range p.Tasks {
		taken[t.ID()] = true
	}
	id := fmt.Sprintf("task-%d", idx+1)
	for taken[id] {
		id += "x"
	}
	p.Tasks[idx].ReferenceID = id
	return id
}

// dependsTransitively reports whether task idx depends, directly or not, on target.
func (p *DraftPlan) dependsTransitively(idx int, target string) bool {
	if target == "" {
		return false
	}
	byID := make(map[string]int, len(p.Tasks))
	for i, t := range p.Tasks {
		if id := t.ID(); id != "" {
			byID[id] = i
		}
	}
	seen := map[int]bool{}
	stack := []int{idx}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		for _, dep := range p.Tasks[cur].DependsOn {
			if dep == target {
				return true
			}
			if next, ok := byID[dep]; ok {
				stack = append(stack, next)
			}
		}
	}
	return false
}

func containsDep(deps []string, id string) bool {
	for _, d := range deps {
		if d == id {
			return true
		}
	}
	return false
}
