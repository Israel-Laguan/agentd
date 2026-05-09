package safety

import (
	"context"
	"fmt"

	"github.com/shirou/gopsutil/v3/process"
)

type PIDProbe interface {
	AlivePIDs(ctx context.Context) ([]int, error)
}

type OSProcessChecker = PIDProbe

type GopsutilProbe struct{}

func (GopsutilProbe) AlivePIDs(ctx context.Context) ([]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pids, err := process.PidsWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("list process ids: %w", err)
	}
	alive := make([]int, 0, len(pids))
	for _, pid := range pids {
		alive = append(alive, int(pid))
	}
	return alive, nil
}

type StaticPIDProbe struct {
	PIDs []int
	Err  error
}

func (p StaticPIDProbe) AlivePIDs(ctx context.Context) ([]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.Err != nil {
		return nil, p.Err
	}
	return append([]int(nil), p.PIDs...), nil
}
