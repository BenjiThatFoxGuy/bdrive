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

func TestAdmissionScopesIndependentOwners(t *testing.T) {
	g := New(8, 2, time.Now)
	finish, err := g.Begin(context.Background(), "owner1")
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	if _, err := g.Begin(context.Background(), "owner1"); err == nil {
		t.Fatal("owner1 not cooling down")
	}
	finish, err = g.Begin(context.Background(), "owner2")
	if err != nil {
		t.Fatalf("owner1 poisoned owner2: %v", err)
	}
	finish(false)
}

func TestCooldownAfterSetupRelease(t *testing.T) {
	now := time.Unix(100, 0)
	g := New(8, 2, func() time.Time { return now })
	finish, err := g.Begin(context.Background(), "file1")
	if err != nil {
		t.Fatal(err)
	}
	finish(false)
	g.Cooldown("file1")
	if _, err := g.Begin(context.Background(), "file1"); err == nil {
		t.Fatal("late read failure did not cool down reconnects")
	}
	finish, err = g.Begin(context.Background(), "file2")
	if err != nil {
		t.Fatalf("late failure poisoned another file: %v", err)
	}
	finish(false)
	now = now.Add(31 * time.Second)
	if _, err := g.Begin(context.Background(), "file1"); err != nil {
		t.Fatalf("cooldown never expired: %v", err)
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
