package sandbox

import (
	"bytes"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHeadTailBuffer_ZeroLimit(t *testing.T) {
	buf := newHeadTailBuffer(0)
	if buf.limit != 1 {
		t.Errorf("limit = %v, want 1", buf.limit)
	}
}

func TestHeadTailBuffer_NegativeLimit(t *testing.T) {
	buf := newHeadTailBuffer(-5)
	if buf.limit != 1 {
		t.Errorf("limit = %v, want 1", buf.limit)
	}
}

func TestHeadTailBuffer_UnderLimit(t *testing.T) {
	buf := newHeadTailBuffer(100)
	buf.WriteString("hello")
	if buf.String() != "hello" {
		t.Errorf("String() = %v, want hello", buf.String())
	}
	if buf.truncated {
		t.Error("should not be truncated")
	}
}

func TestHeadTailBuffer_ExactlyAtLimit(t *testing.T) {
	buf := newHeadTailBuffer(10)
	buf.WriteString("hello")
	result := buf.String()
	if result != "hello" {
		t.Errorf("String() = %q, want %q", result, "hello")
	}
}

func TestHeadTailBuffer_OverLimit(t *testing.T) {
	buf := newHeadTailBuffer(5)
	buf.WriteString("hello world")
	result := buf.String()
	if !strings.Contains(result, "truncated") {
		t.Errorf("String() should contain truncated, got %q", result)
	}
}

func TestHeadTailBuffer_WriteEmpty(t *testing.T) {
	buf := newHeadTailBuffer(10)
	buf.Write(nil)
	buf.Write([]byte{})
	// Total bytes should be 0
	if buf.totalBytes != 0 {
		t.Errorf("totalBytes = %v, want 0", buf.totalBytes)
	}
}

func TestHeadTailBuffer_String_NotTruncated(t *testing.T) {
	buf := newHeadTailBuffer(100)
	buf.WriteString("test")
	if buf.String() != "test" {
		t.Errorf("String() = %q, want %q", buf.String(), "test")
	}
}

func TestInactivityReader(t *testing.T) {
	reader := strings.NewReader("hello world")
	var timedOut bool
	ir := newInactivityReader(reader, time.Hour, func() { timedOut = true })
	defer ir.stop()

	p := make([]byte, 5)
	n, err := ir.Read(p)
	if err != nil {
		t.Errorf("Read() error = %v", err)
	}
	if n != 5 {
		t.Errorf("n = %v, want 5", n)
	}
	if timedOut {
		t.Error("should not have timed out")
	}
}

func TestInactivityReader_EOFStopsTimer(t *testing.T) {
	reader := strings.NewReader("hi")
	var timedOut bool
	ir := newInactivityReader(reader, time.Hour, func() { timedOut = true })

	p := make([]byte, 10)
	n, err := ir.Read(p)
	if n != 2 {
		t.Errorf("n = %v, want 2", n)
	}
	if err == nil {
		// Second read should give EOF
		_, err = ir.Read(p)
	}
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
	ir.stop()
	if timedOut {
		t.Error("timer should be stopped after EOF")
	}
}

func TestInactivityReader_ResetsOnRead(t *testing.T) {
	reader := strings.NewReader("hello")
	var timeoutCount atomic.Int32
	ir := newInactivityReader(reader, time.Millisecond*50, func() { timeoutCount.Add(1) })

	if _, err := ir.Read(make([]byte, 1)); err != nil {
		t.Fatalf("first Read() error = %v", err)
	}
	time.Sleep(time.Millisecond * 10)
	if _, err := ir.Read(make([]byte, 1)); err != nil {
		t.Fatalf("second Read() error = %v", err)
	}
	time.Sleep(time.Millisecond * 10)
	if _, err := ir.Read(make([]byte, 1)); err != nil {
		t.Fatalf("third Read() error = %v", err)
	}
	time.Sleep(time.Millisecond * 60)
	ir.stop()
	if timeoutCount.Load() == 0 {
		t.Error("should have timed out after inactivity")
	}
}

func TestInactivityReader_MultipleSmallReads(t *testing.T) {
	reader := bytes.NewReader([]byte("hello"))
	ir := newInactivityReader(reader, time.Hour, func() {})

	buf := make([]byte, 2)
	for i := 0; i < 3; i++ {
		n, err := ir.Read(buf)
		if n == 0 && err == nil {
			break
		}
	}
	ir.stop()
}
