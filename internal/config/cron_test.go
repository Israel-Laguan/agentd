package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadCronMissingFileReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentd.crontab")

	got, err := LoadCron(path)
	if err != nil {
		t.Fatalf("LoadCron() error = %v", err)
	}

	assertDefaultIntervals(t, got)
	if got.Path != path {
		t.Fatalf("Path = %q, want %q", got.Path, path)
	}
	if got.DiskWatchdog.Schedule == nil {
		t.Fatal("DiskWatchdog.Schedule is nil")
	}
	if got.MemoryCurator.Schedule == nil {
		t.Fatal("MemoryCurator.Schedule is nil")
	}
}

func TestLoadCronParsesDefaultFile(t *testing.T) {
	path := writeCronFile(t, defaultCronFile)

	got, err := LoadCron(path)
	if err != nil {
		t.Fatalf("LoadCron() error = %v", err)
	}

	assertDefaultIntervals(t, got)
	if got.DiskWatchdog.Spec != "*/10 * * * *" {
		t.Fatalf("DiskWatchdog.Spec = %q", got.DiskWatchdog.Spec)
	}
	if got.MemoryCurator.Spec != "0 * * * *" {
		t.Fatalf("MemoryCurator.Spec = %q", got.MemoryCurator.Spec)
	}
}

func TestLoadCronParsesFiveFieldJobs(t *testing.T) {
	path := writeCronFile(t, "*/15 * * * * disk-watchdog\n30 * * * * memory-curator\n")

	got, err := LoadCron(path)
	if err != nil {
		t.Fatalf("LoadCron() error = %v", err)
	}

	if got.DiskWatchdog.Schedule == nil {
		t.Fatal("DiskWatchdog.Schedule is nil")
	}
	if got.DiskWatchdog.Spec != "*/15 * * * *" {
		t.Fatalf("DiskWatchdog.Spec = %q", got.DiskWatchdog.Spec)
	}
	if got.MemoryCurator.Spec != "30 * * * *" {
		t.Fatalf("MemoryCurator.Spec = %q", got.MemoryCurator.Spec)
	}
}

func TestLoadCronSkipsCommentsBlankAndUnknownJobs(t *testing.T) {
	path := writeCronFile(t, "\n# comment\n@every 1s unknown-job\n@every 7s task-dispatch\n")

	got, err := LoadCron(path)
	if err != nil {
		t.Fatalf("LoadCron() error = %v", err)
	}

	if got.TaskDispatch != 7*time.Second {
		t.Fatalf("TaskDispatch = %s, want 7s", got.TaskDispatch)
	}
	if got.Intake != DefaultCronSchedule.Intake {
		t.Fatalf("Intake = %s, want default %s", got.Intake, DefaultCronSchedule.Intake)
	}
}

func TestLoadCronLastDuplicateWins(t *testing.T) {
	path := writeCronFile(t, "@every 1s intake\n@every 9s intake\n")

	got, err := LoadCron(path)
	if err != nil {
		t.Fatalf("LoadCron() error = %v", err)
	}

	if got.Intake != 9*time.Second {
		t.Fatalf("Intake = %s, want 9s", got.Intake)
	}
}

func TestLoadCronMalformedLineReportsLineNumber(t *testing.T) {
	path := writeCronFile(t, "# ok\n@every nope task-dispatch\n")

	_, err := LoadCron(path)
	if err == nil {
		t.Fatal("LoadCron() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("error = %q, want line number", err)
	}
}

func TestWriteDefaultCronDoesNotOverwriteUserFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentd.crontab")
	if err := WriteDefaultCron(path); err != nil {
		t.Fatalf("WriteDefaultCron() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("@every 11s intake\n"), 0o644); err != nil {
		t.Fatalf("overwrite test file: %v", err)
	}
	if err := WriteDefaultCron(path); err != nil {
		t.Fatalf("WriteDefaultCron() second error = %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cron file: %v", err)
	}
	if string(contents) != "@every 11s intake\n" {
		t.Fatalf("cron file was overwritten: %q", contents)
	}
}

func writeCronFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agentd.crontab")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write cron file: %v", err)
	}
	return path
}

func assertDefaultIntervals(t *testing.T, got CronSchedule) {
	t.Helper()
	if got.TaskDispatch != DefaultCronSchedule.TaskDispatch {
		t.Fatalf("TaskDispatch = %s, want %s", got.TaskDispatch, DefaultCronSchedule.TaskDispatch)
	}
	if got.Intake != DefaultCronSchedule.Intake {
		t.Fatalf("Intake = %s, want %s", got.Intake, DefaultCronSchedule.Intake)
	}
	if got.Heartbeat != DefaultCronSchedule.Heartbeat {
		t.Fatalf("Heartbeat = %s, want %s", got.Heartbeat, DefaultCronSchedule.Heartbeat)
	}
}
