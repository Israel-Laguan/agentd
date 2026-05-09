package sandbox

import (
	"bytes"
	"fmt"
)

type headTailBuffer struct {
	limit      int
	head       bytes.Buffer
	tail       []byte
	totalBytes int
	truncated  bool
}

func newHeadTailBuffer(limit int) *headTailBuffer {
	if limit <= 0 {
		limit = 1
	}
	return &headTailBuffer{limit: limit}
}

func (b *headTailBuffer) WriteString(value string) {
	b.Write([]byte(value))
}

func (b *headTailBuffer) Write(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	b.totalBytes += len(chunk)
	if !b.truncated {
		remaining := b.limit - b.head.Len()
		if remaining > 0 {
			toWrite := remaining
			if toWrite > len(chunk) {
				toWrite = len(chunk)
			}
			_, _ = b.head.Write(chunk[:toWrite])
		}
		if b.head.Len() >= b.limit {
			b.truncated = true
		}
		if len(chunk) <= remaining {
			return
		}
		chunk = chunk[remaining:]
		b.truncated = true
	}
	b.pushTail(chunk)
}

func (b *headTailBuffer) pushTail(chunk []byte) {
	capacity := b.limit / 2
	if capacity < 256 {
		capacity = b.limit
	}
	if capacity <= 0 {
		capacity = len(chunk)
	}
	if len(chunk) >= capacity {
		b.tail = append(b.tail[:0], chunk[len(chunk)-capacity:]...)
		return
	}
	needed := len(b.tail) + len(chunk) - capacity
	if needed > 0 {
		b.tail = append(b.tail[:0], b.tail[needed:]...)
	}
	b.tail = append(b.tail, chunk...)
}

func (b *headTailBuffer) String() string {
	if !b.truncated {
		return b.head.String()
	}
	truncatedBytes := b.totalBytes - b.head.Len() - len(b.tail)
	if truncatedBytes < 0 {
		truncatedBytes = 0
	}
	var out bytes.Buffer
	out.Grow(b.head.Len() + len(b.tail) + 96)
	out.Write(b.head.Bytes())
	_, _ = fmt.Fprintf(&out, "\n[... %d bytes truncated ...]\n", truncatedBytes)
	out.Write(b.tail)
	return out.String()
}
