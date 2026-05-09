package sse

import "sync/atomic"

type Hub struct {
	active atomic.Int64
}

func (h *Hub) Add() {
	h.active.Add(1)
}

func (h *Hub) Done() {
	h.active.Add(-1)
}

func (h *Hub) Active() int64 {
	return h.active.Load()
}
