package bus

import (
	"context"
	"testing"
)

func TestInProcessPublishSubscribeUnsubscribe(t *testing.T) {
	b := NewInProcess()
	ch, unsubscribe := b.Subscribe("task:1", 1)
	b.Publish(context.Background(), Signal{Topic: "task:1", Type: "LOG", Payload: "hello"})

	got := <-ch
	if got.Type != "LOG" || got.Payload != "hello" {
		t.Fatalf("event = %#v, want LOG hello", got)
	}

	unsubscribe()
	if _, ok := <-ch; ok {
		t.Fatal("expected channel to close after unsubscribe")
	}
}

func TestInProcessDropsSlowSubscriber(t *testing.T) {
	b := NewInProcess()
	slow, _ := b.Subscribe("task:1", 1)
	fast, _ := b.Subscribe("task:1", 2)

	b.Publish(context.Background(), Signal{Topic: "task:1", Payload: "first"})
	b.Publish(context.Background(), Signal{Topic: "task:1", Payload: "second"})

	if got := <-slow; got.Payload != "first" {
		t.Fatalf("slow subscriber first event = %#v", got)
	}
	if got := <-fast; got.Payload != "first" {
		t.Fatalf("fast subscriber first event = %#v", got)
	}
	if got := <-fast; got.Payload != "second" {
		t.Fatalf("fast subscriber second event = %#v", got)
	}
}
