//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package desk

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInputPreparationRepositoryCancelsBeforeContendedWrite(t *testing.T) {
	s, _ := newTestServer(t, false)
	repo := s.preparationRepository()
	saved, err := repo.CompareAndSwap(context.Background(), repositoryDocument(1), "")
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.openInputPreparations(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	held, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err = lockPrivateData(held, true); err != nil {
		t.Fatal(err)
	}
	doc := saved.Document
	doc.Name = "Cancelled update"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err = repo.CompareAndSwap(ctx, doc, saved.SHA256); !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, errInputPreparationBusy) {
		t.Fatal("lock wait lost cancellation or storage-busy classification", err)
	}
	held.Close()
	current, err := repo.Get(context.Background(), doc.ID)
	if err != nil || current.SHA256 != saved.SHA256 {
		t.Fatal("cancelled operation changed stored data", current, err)
	}
}

func TestInputPreparationTryReadDoesNotWaitForWriter(t *testing.T) {
	s, _ := newTestServer(t, false)
	repo := s.preparationRepository()
	if _, err := repo.CompareAndSwap(context.Background(), repositoryDocument(1), ""); err != nil {
		t.Fatal(err)
	}
	root, err := s.openInputPreparations(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	held, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err = lockPrivateData(held, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	view, err := repo.TryRead(ctx)
	if view != nil {
		view.Close()
	}
	if !errors.Is(err, errInputPreparationBusy) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		t.Fatal("nonblocking admission waited or acquired a view", err)
	}
	held.Close()
	view, err = repo.TryRead(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	view.Close()
}
