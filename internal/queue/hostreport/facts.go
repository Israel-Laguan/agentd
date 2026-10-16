package hostreport

import (
	"regexp"
	"strings"
)

// Input is one collected file.
type Input struct {
	Name    string
	Content string
}

// Disk is one physical block device.
type Disk struct {
	Name string
	Size string
}

// Facts are the whitelisted host facts a report may state. Fields stay empty
// when no collected file carried them.
type Facts struct {
	OS       string
	Kernel   string
	Arch     string
	CPUModel string
	CPUs     string
	Memory   string
	Disks    []Disk
}

var (
	osPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?m)^PRETTY_NAME="?([^"\n]+)"?\s*$`),
		regexp.MustCompile(`(?m)^\s*Operating System:\s*(\S.*?)\s*$`),
		regexp.MustCompile(`(?m)^Description:\s*(\S.*?)\s*$`),
	}
	kernelPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?m)^\s*Kernel:\s*(\S.*?)\s*$`),
		regexp.MustCompile(`(?m)^(Linux)\s+\S+\s+(\d+\.\d+\S*)\s`),
	}
	// lscpu prints the machine architecture at column 0; hostnamectl indents it
	// and spells it x86-64, so the strict form wins.
	archStrict   = regexp.MustCompile(`(?m)^Architecture:\s*(\S+)`)
	archLoose    = regexp.MustCompile(`(?m)^\s*Architecture:\s*(\S+)`)
	modelPattern = regexp.MustCompile(`(?m)^Model name:\s*(\S.*?)\s*$`)
	cpusPattern  = regexp.MustCompile(`(?m)^CPU\(s\):\s*(\d+)\s*$`)
	memPattern   = regexp.MustCompile(`(?m)^Mem:\s+(\S+)`)
	treePrefix   = "├─└│|`- "
)

// Extract reads only the whitelisted facts from the collected files; the first
// file that carries a fact wins.
func Extract(inputs []Input) Facts {
	var f Facts
	for _, in := range inputs {
		text := in.Content
		f.OS = firstNonEmpty(f.OS, matchAny(osPatterns, text))
		f.Kernel = firstNonEmpty(f.Kernel, kernelFrom(text))
		f.CPUModel = firstNonEmpty(f.CPUModel, submatch(modelPattern, text))
		f.CPUs = firstNonEmpty(f.CPUs, submatch(cpusPattern, text))
		f.Memory = firstNonEmpty(f.Memory, submatch(memPattern, text))
		if len(f.Disks) == 0 {
			f.Disks = disksFrom(text)
		}
	}
	f.Arch = firstNonEmpty(archFrom(inputs, archStrict), archFrom(inputs, archLoose))
	return f
}

func archFrom(inputs []Input, re *regexp.Regexp) string {
	for _, in := range inputs {
		if v := submatch(re, in.Content); v != "" {
			return v
		}
	}
	return ""
}

func firstNonEmpty(current, next string) string {
	if current != "" {
		return current
	}
	return next
}

// HasAnyFact reports whether any host fact was collected at all. Without one
// the renderer steps aside and the worker falls back to the model path.
func HasAnyFact(f Facts) bool {
	scalar := f.OS != "" || f.Kernel != "" || f.CPUModel != "" || f.Memory != ""
	if scalar {
		return true
	}
	return len(f.Disks) != 0
}

// MissingFacts names the core facts no collected file carried, in report
// order. A non-empty result means the collection is partial: the report task
// must fail with retry feedback instead of writing "not collected".
func MissingFacts(f Facts) []string {
	var missing []string
	if f.OS == "" {
		missing = append(missing, "OS")
	}
	if f.Kernel == "" {
		missing = append(missing, "kernel")
	}
	if f.CPUModel == "" {
		missing = append(missing, "CPU")
	}
	if f.Memory == "" {
		missing = append(missing, "memory")
	}
	if len(f.Disks) == 0 {
		missing = append(missing, "disks")
	}
	return missing
}

func submatch(re *regexp.Regexp, text string) string {
	if m := re.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

func matchAny(patterns []*regexp.Regexp, text string) string {
	for _, re := range patterns {
		if v := submatch(re, text); v != "" {
			return v
		}
	}
	return ""
}

func kernelFrom(text string) string {
	if v := submatch(kernelPatterns[0], text); v != "" {
		return v
	}
	if m := kernelPatterns[1].FindStringSubmatch(text); m != nil {
		return m[1] + " " + m[2]
	}
	return ""
}

// disksFrom parses lsblk output by its header columns and keeps TYPE == disk.
// Without an lsblk table it falls back to `df -h` rows for /dev entries, so a
// collector that lost the lsblk output still yields disk sizes (B-046). Both
// readings stay inside the collected file, so the evidence rule holds.
func disksFrom(text string) []Disk {
	var disks []Disk
	sizeCol, typeCol := -1, -1
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "NAME" {
			sizeCol, typeCol = indexOf(fields, "SIZE"), indexOf(fields, "TYPE")
			continue
		}
		if sizeCol < 0 || typeCol < 0 || len(fields) <= max(sizeCol, typeCol) {
			continue
		}
		if fields[typeCol] == "disk" {
			disks = append(disks, Disk{Name: strings.TrimLeft(fields[0], treePrefix), Size: fields[sizeCol]})
		}
	}
	if len(disks) > 0 {
		return disks
	}
	return disksFromDf(text)
}

// disksFromDf reads `df -h` output: the device and size of mounted /dev
// entries. Pseudo filesystems (udev, tmpfs) carry no /dev entry and are
// skipped; partitions are kept as named because df cannot tell them apart
// from whole disks.
func disksFromDf(text string) []Disk {
	var disks []Disk
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasPrefix(fields[0], "/dev/") {
			continue
		}
		disks = append(disks, Disk{Name: strings.TrimPrefix(fields[0], "/dev/"), Size: fields[1]})
	}
	return disks
}

func indexOf(fields []string, want string) int {
	for i, f := range fields {
		if f == want {
			return i
		}
	}
	return -1
}
