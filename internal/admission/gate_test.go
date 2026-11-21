package admission

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestStartupStormUnavailableThenReady(t *testing.T) {
	now := time.Unix(100, 0)
	g := New(8, 2, func() time.Time { return now })
	finish, err := g.Begin(context.Background(), "auth")
	if err != nil {
		t.Fatal(err)
	}
	second, err := g.Begin(context.Background(), "auth")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := g.Begin(context.Background(), "auth"); err == nil {
				t.Error("unbounded startup admission")
			}
		}()
	}
	wg.Wait()
	finish(true)
	second(false)
	// A success already in flight must not erase the concurrent outage cooldown.
	if _, err := g.Begin(context.Background(), "auth"); err == nil {
		t.Fatal("missing cooldown")
	}
	now = now.Add(31 * time.Second)
	finish, err = g.Begin(context.Background(), "auth")
	if err != nil {
		t.Fatal(err)
	}
	finish(false)
	if _, err = g.Begin(context.Background(), "auth"); err != nil {
		t.Fatal("did not recover", err)
	}
}

func TestAdmissionBoundedAndCancelled(t *testing.T) {
	g := New(2, 1, time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Begin(ctx, "auth"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation %v", err)
	}
	for _, key := range []string{"owner1", "owner2"} {
		finish, err := g.Begin(context.Background(), key)
		if err != nil {
			t.Fatal(err)
		}
		finish(true)
	}
	if _, err := g.Begin(context.Background(), "owner3"); err == nil {
		t.Fatal("unbounded gate table")
	}
}
