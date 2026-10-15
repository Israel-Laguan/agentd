package hostreport

import (
	"fmt"
	"regexp"
	"strings"
)

const notCollected = "not collected"

var (
	hostFactIntent = regexp.MustCompile(`(?i)\b(operating system|os|hardware|cpu|processor|ram|memory|disks?|storage|kernel|machine)\b`)
	reportVerb     = regexp.MustCompile(`(?i)\b(report|summar\w*)\b`)
	markdownTarget = regexp.MustCompile("(?:^|[\\s\"'(`])([A-Za-z0-9_][A-Za-z0-9_.-]*\\.md)\\b")
)

// ReportTarget returns the markdown file a host-fact report task should write,
// or false when the task is not one. Paths with directories are not accepted.
func ReportTarget(title, description string) (string, bool) {
	text := title + "\n" + description
	if !hostFactIntent.MatchString(text) || !reportVerb.MatchString(text) {
		return "", false
	}
	if m := markdownTarget.FindStringSubmatch(text); m != nil {
		return m[1], true
	}
	return "", false
}

// Render builds the summary, or false when the files carry no host facts.
func Render(inputs []Input) (string, bool) {
	f := Extract(inputs)
	if f.OS == "" && f.Kernel == "" && f.CPUModel == "" && f.Memory == "" && len(f.Disks) == 0 {
		return "", false
	}
	var b strings.Builder
	b.WriteString("# System report\n\n")
	fmt.Fprintf(&b, "- **OS:** %s\n", orMissing(f.OS))
	fmt.Fprintf(&b, "- **Kernel:** %s\n", orMissing(f.Kernel))
	if f.Arch != "" {
		fmt.Fprintf(&b, "- **Architecture:** %s\n", f.Arch)
	}
	fmt.Fprintf(&b, "- **CPU:** %s\n", cpuLine(f))
	fmt.Fprintf(&b, "- **Memory:** %s\n", memoryLine(f.Memory))
	fmt.Fprintf(&b, "- **Disks:** %s\n", disksLine(f.Disks))
	fmt.Fprintf(&b, "\n_Summarized from: %s._\n", sourceNames(inputs))
	return b.String(), true
}

func orMissing(v string) string {
	if v == "" {
		return notCollected
	}
	return v
}

func cpuLine(f Facts) string {
	switch {
	case f.CPUModel == "":
		return notCollected
	case f.CPUs == "":
		return f.CPUModel
	}
	return fmt.Sprintf("%s (%s CPUs)", f.CPUModel, f.CPUs)
}

func memoryLine(total string) string {
	if total == "" {
		return notCollected
	}
	return total + " total"
}

func disksLine(disks []Disk) string {
	if len(disks) == 0 {
		return notCollected
	}
	parts := make([]string, len(disks))
	for i, d := range disks {
		parts[i] = d.Name + " " + d.Size
	}
	return strings.Join(parts, ", ")
}

func sourceNames(inputs []Input) string {
	names := make([]string, len(inputs))
	for i, in := range inputs {
		names[i] = in.Name
	}
	return strings.Join(names, ", ")
}
