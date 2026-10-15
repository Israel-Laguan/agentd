package hostreport

import (
	"strings"
	"testing"
)

const (
	hostnamectlOut = ` Static hostname: acer
       Icon name: computer-laptop
      Machine ID: 9ebbf53566051e82c3e15b536a381807
         Boot ID: 5750069a9cf54af48d9c614bab1f62b0
Operating System: Debian GNU/Linux 13 (trixie)
          Kernel: Linux 7.2.9-2-liquorix-amd64
    Architecture: x86-64
 Hardware Vendor: Acer
`
	unameOut = "Linux acer 7.2.9-2-liquorix-amd64 #1 ZEN SMP PREEMPT_DYNAMIC liquorix 7.2-9~trixie x86_64 GNU/Linux\n"
	lscpuOut = `Architecture:                            x86_64
CPU(s):                                  8
On-line CPU(s) list:                     0-7
Model name:                              Intel(R) Core(TM) Ultra 5 226V
NUMA node0 CPU(s):                       0-7
Flags:                                   fpu vme de pse tsc msr pae
Vulnerability Spectre v2:                Mitigation; Enhanced / Automatic IBRS
`
	freeOut = `               total        used        free      shared  buff/cache   available
Mem:            15Gi       7.9Gi       1.2Gi       1.0Gi       7.5Gi       7.7Gi
Swap:          8.0Gi          0B       8.0Gi
`
	lsblkOut = `NAME        MAJ:MIN RM   SIZE RO TYPE MOUNTPOINTS
sda           8:0    1  28.9G  0 disk
└─sda1        8:1    1  28.9G  0 part /run/media/usb
nvme0n1     259:0    0 476.9G  0 disk
├─nvme0n1p1 259:1    0   512M  0 part /boot/efi
└─nvme0n1p2 259:2    0 476.4G  0 part /
loop0         7:0    0     4K  1 loop /snap/x
`
)

func collected() []Input {
	return []Input{
		{Name: "os_info.txt", Content: hostnamectlOut + unameOut},
		{Name: "cpu_info.txt", Content: lscpuOut},
		{Name: "mem_info.txt", Content: freeOut},
		{Name: "disk_info.txt", Content: lsblkOut},
	}
}

func TestExtractFindsHostFacts(t *testing.T) {
	t.Parallel()
	f := Extract(collected())
	if f.OS != "Debian GNU/Linux 13 (trixie)" || f.Kernel != "Linux 7.2.9-2-liquorix-amd64" {
		t.Fatalf("OS/Kernel = %q / %q", f.OS, f.Kernel)
	}
	if f.CPUModel != "Intel(R) Core(TM) Ultra 5 226V" || f.CPUs != "8" || f.Arch != "x86_64" {
		t.Fatalf("CPU = %q x%q arch %q", f.CPUModel, f.CPUs, f.Arch)
	}
	if f.Memory != "15Gi" {
		t.Fatalf("Memory = %q", f.Memory)
	}
	want := []Disk{{Name: "sda", Size: "28.9G"}, {Name: "nvme0n1", Size: "476.9G"}}
	if len(f.Disks) != len(want) || f.Disks[0] != want[0] || f.Disks[1] != want[1] {
		t.Fatalf("Disks = %#v, want %#v (partitions and loop devices excluded)", f.Disks, want)
	}
}

func TestExtractFallsBackToUnameAndOSRelease(t *testing.T) {
	t.Parallel()
	f := Extract([]Input{{Name: "a.txt", Content: "PRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\nID=debian\n" + unameOut}})
	if f.OS != "Debian GNU/Linux 13 (trixie)" || f.Kernel != "Linux 7.2.9-2-liquorix-amd64" {
		t.Fatalf("OS/Kernel = %q / %q", f.OS, f.Kernel)
	}
}

// Every value in the report must come from the collected files (SP-018), and
// machine/boot identifiers must never reach it (B-045).
func TestRenderUsesOnlyCollectedValuesAndOmitsIdentifiers(t *testing.T) {
	t.Parallel()
	inputs := collected()
	report, ok := Render(inputs)
	if !ok {
		t.Fatal("Render() ok = false, want a report")
	}
	for _, secret := range []string{"9ebbf535", "5750069a", "acer", "Machine ID", "Boot ID", "Flags", "Spectre"} {
		if strings.Contains(report, secret) {
			t.Fatalf("report leaks %q:\n%s", secret, report)
		}
	}
	all := ""
	for _, in := range inputs {
		all += in.Content
	}
	for _, v := range []string{"Debian GNU/Linux 13 (trixie)", "Linux 7.2.9-2-liquorix-amd64", "Intel(R) Core(TM) Ultra 5 226V", "15Gi", "nvme0n1", "476.9G", "sda", "28.9G"} {
		if !strings.Contains(report, v) {
			t.Fatalf("report is missing %q:\n%s", v, report)
		}
		if !strings.Contains(all, v) {
			t.Fatalf("test fixture does not contain %q", v)
		}
	}
	if len(report) > 800 {
		t.Fatalf("report is %d bytes, want a short summary", len(report))
	}
}

func TestRenderRefusesWithoutAnyFacts(t *testing.T) {
	t.Parallel()
	if report, ok := Render([]Input{{Name: "x.txt", Content: "hello world"}}); ok {
		t.Fatalf("Render() = %q, want no report", report)
	}
}

func TestRenderMarksMissingFactsInsteadOfInventingThem(t *testing.T) {
	t.Parallel()
	report, ok := Render([]Input{{Name: "m.txt", Content: freeOut}})
	if !ok || !strings.Contains(report, "15Gi") || !strings.Contains(report, "not collected") {
		t.Fatalf("report = %q (ok=%v), want memory plus 'not collected' markers", report, ok)
	}
}

func TestIsReportTask(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, title, desc, out string
	}{
		{"report", "Build report", "Create report.md summarizing the OS and hardware details from os_info.txt", "report.md"},
		{"summary", "Write system summary", "Summarize the machine hardware into summary.md", "summary.md"},
		{"collect", "Collect CPU info", "Run lscpu and save the output to cpu_info.txt", ""},
		{"not host", "Build report", "Create report.md summarizing test coverage", ""},
		{"nested path", "Build report", "Summarize the OS and CPU into out/report.md", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := ReportTarget(tt.title, tt.desc)
			if got != tt.out {
				t.Fatalf("ReportTarget() = %q, want %q", got, tt.out)
			}
		})
	}
}
