package codexbridge

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestAccountLookupFailuresAcrossHostOperations(t *testing.T) {
	for _, mode := range []string{"backend", "deadline", "protocol"} {
		for _, operation := range []string{"status", "models", "run"} {
			t.Run(mode+"/"+operation, func(t *testing.T) {
				nativeMode := "account-lookup-fail"
				if mode == "protocol" {
					nativeMode = "account-protocol-fail"
				}
				m, _ := accountManager(t, nativeMode)
				if mode == "deadline" {
					launch := m.launch
					m.launch = func(ctx context.Context, cmd *exec.Cmd) (*client, error) {
						c, err := launch(ctx, cmd)
						if c != nil {
							c.accountReadBudget = -time.Second
						}
						return c, err
					}
				}
				var err error
				switch operation {
				case "status":
					_, err = m.Status(context.Background(), "owner", false)
				case "models":
					_, err = m.Models(context.Background())
				case "run":
					owner, endSession, endPolicy := ownerFixture()
					defer endSession()
					defer endPolicy()
					err = m.Run(context.Background(), owner, runRequest(), func(RunEvent) error { t.Error("run began before account lookup"); return nil }, func(context.Context, ToolCall) (ToolAnswer, error) {
						t.Error("tool executed")
						return ToolAnswer{}, nil
					})
				}
				if mode == "protocol" {
					if !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrAccountLookup) {
						t.Fatalf("protocol classification: %v", err)
					}
				} else {
					if !errors.Is(err, ErrAccountLookup) || errors.Is(err, ErrUnavailable) || errors.Is(err, ErrLimit) {
						t.Fatalf("lookup classification: %v", err)
					}
					if mode == "deadline" && !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("deadline cause lost: %v", err)
					}
				}
				if strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
					t.Fatal("native prose escaped")
				}
			})
		}
	}
}

func TestAccountLookupBudgetAndCancellation(t *testing.T) {
	if accountLookupBudget <= 15*time.Second || accountLookupBudget > 30*time.Second {
		t.Fatal("account budget must allow the bounded backend lookup")
	}
	m, _ := accountManager(t, "pending")
	if err := m.spawn(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := m.readAccount(ctx, false)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrAccountLookup) {
		t.Fatalf("cancellation classification: %v", err)
	}
}
