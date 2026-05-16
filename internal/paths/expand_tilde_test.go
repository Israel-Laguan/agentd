package paths

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestExpandTildePrefix(t *testing.T) {
	t.Parallel()
	t.Run("non_tilde", func(t *testing.T) {
		t.Parallel()
		in := filepath.Join("a", "b", "c")
		if got := ExpandTildePrefix(in); got != in {
			t.Errorf("ExpandTildePrefix(%q) = %q, want unchanged", in, got)
		}
	})
	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		if got := ExpandTildePrefix(""); got != "" {
			t.Errorf("ExpandTildePrefix(\"\") = %q, want empty", got)
		}
	})
	t.Run("userHomeDir_success", func(t *testing.T) {
		t.Parallel()
		userHome := func() (string, error) { return "/home/u", nil }
		getenv := func(string) string { return "" }
		got := expandTildePrefix("~/skills", userHome, getenv)
		want := filepath.Join("/home/u", "skills")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("fallback_HOME_when_userHomeDir_fails", func(t *testing.T) {
		t.Parallel()
		userHome := func() (string, error) { return "", errors.New("no home") }
		getenv := func(k string) string {
			if k == "HOME" {
				return "/container/home"
			}
			return ""
		}
		got := expandTildePrefix("~/.agentd/skills", userHome, getenv)
		want := filepath.Join("/container/home", ".agentd", "skills")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("fallback_USERPROFILE_when_HOME_empty", func(t *testing.T) {
		t.Parallel()
		userHome := func() (string, error) { return "", errors.New("no home") }
		getenv := func(k string) string {
			if k == "USERPROFILE" {
				return `C:\Users\runner`
			}
			return ""
		}
		got := expandTildePrefix("~/skills", userHome, getenv)
		want := filepath.Join(`C:\Users\runner`, "skills")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("unchanged_when_unresolvable", func(t *testing.T) {
		t.Parallel()
		userHome := func() (string, error) { return "", errors.New("no home") }
		getenv := func(string) string { return "" }
		in := "~/nowhere"
		if got := expandTildePrefix(in, userHome, getenv); got != in {
			t.Errorf("got %q, want %q", got, in)
		}
	})
}
