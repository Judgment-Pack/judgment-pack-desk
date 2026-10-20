package delegation

import (
	"context"
	"testing"
	"time"
)

func TestCapacityWaitTakesRoomWhenItAppearsAndEndsWithItsContext(t *testing.T) {
	var c Capacity
	var releases []func()
	for i := 0; i < 4; i++ {
		release, ok := c.Try("connection")
		if !ok {
			t.Fatalf("take %d refused", i)
		}
		releases = append(releases, release)
	}
	if _, ok := c.Try("connection"); ok {
		t.Fatal("a fifth was admitted past four")
	}
	// A wait that is cut by its context returns the context's error and holds nothing.
	short, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if release, err := c.Wait(short, "connection"); err == nil || release != nil {
		t.Fatalf("a wait with no room returned %v", err)
	}
	// A wait is given the room the moment a holder gives it back.
	got := make(chan error, 1)
	go func() {
		release, err := c.Wait(context.Background(), "connection")
		if err == nil {
			defer release()
		}
		got <- err
	}()
	select {
	case err := <-got:
		t.Fatalf("a wait returned with no room: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	releases[0]()
	select {
	case err := <-got:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a wait was not given the room that was released")
	}
	// Another connection's room is its own.
	if release, ok := c.Try("other"); !ok {
		t.Fatal("another connection was held to this one's limit")
	} else {
		release()
	}
	for _, release := range releases[1:] {
		release()
	}
}

func TestCapacityWaitIsNotBackgroundWork(t *testing.T) {
	// Background work is held to two at once beside the interactive requests;
	// a wait is an interactive request, so it is not.
	var c Capacity
	var releases []func()
	for i := 0; i < 4; i++ {
		release, err := c.Wait(context.Background(), "connection")
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	for _, release := range releases {
		release()
	}
}
