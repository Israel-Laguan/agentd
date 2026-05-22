package safety

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestStaticPIDProbe_ReturnsPIDs(t *testing.T) {
	p := StaticPIDProbe{PIDs: []int{42, 99}}
	got, err := p.AlivePIDs(context.Background())
	if err != nil {
		t.Fatalf("AlivePIDs: %v", err)
	}
	if len(got) != 2 || got[0] != 42 || got[1] != 99 {
		t.Fatalf("AlivePIDs = %v", got)
	}
}

func TestStaticPIDProbe_PropagatesErr(t *testing.T) {
	want := errors.New("probe failed")
	p := StaticPIDProbe{Err: want}
	_, err := p.AlivePIDs(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestStaticPIDProbe_RespectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := StaticPIDProbe{PIDs: []int{1}}
	_, err := p.AlivePIDs(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestGopsutilProbe_AlivePIDs(t *testing.T) {
	p := GopsutilProbe{}
	got, err := p.AlivePIDs(context.Background())
	if err != nil {
		if strings.Contains(err.Error(), "not permitted") || strings.Contains(err.Error(), "permission denied") {
			t.Skipf("gopsutil unavailable in this environment: %v", err)
		}
		t.Fatalf("AlivePIDs: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected at least one alive pid on this host")
	}
}
