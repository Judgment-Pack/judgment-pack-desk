package desk

import (
	"context"
	"errors"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/aifailure"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
	"net"
)

func aiAgentFailure(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	var known *aifailure.Failure
	if errors.As(err, &known) {
		return known
	}
	if code, detail := codexHostFailure(err); code != "" {
		outcome := "unknown"
		if code == "connection-lost" {
			outcome = "not-started"
		}
		return aifailure.Authored(code, "agent", outcome, detail)
	}
	code, outcome := "unknown", "unknown"
	switch {
	case errors.Is(err, codexbridge.ErrBusy):
		code, outcome = "busy", "not-started"
	case errors.Is(err, codexbridge.ErrSignIn):
		code, outcome = "authentication", "not-started"
	case errors.Is(err, codexbridge.ErrUnavailable):
		code, outcome = "unavailable", "not-started"
	case errors.Is(err, codexbridge.ErrLimit):
		code = "provider-limit"
	case errors.Is(err, context.DeadlineExceeded):
		code = "timeout"
	case errors.Is(err, codexbridge.ErrRetries):
		code = "retries-exhausted"
	}
	return aifailure.New(code, "agent", outcome)
}

// Model boundaries retain typed failures and cancellation. Unknown implementation
// errors receive a safe diagnostic; caught provider text never becomes UI copy.
func aiModelFailure(err error) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	var known *aifailure.Failure
	if errors.As(err, &known) {
		return known
	}
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
		return aifailure.New("timeout", "model", "unknown")
	}
	return aifailure.FromCode(codeOf(err), "model", "unknown")
}

// Closed host-authored guidance shared by legacy and canonical agent boundaries.
// Raw filesystem paths and native/provider errors must never become detail.
func codexHostFailure(err error) (code, detail string) {
	if errors.Is(err, context.Canceled) {
		return "", ""
	}
	if codexbridge.DiskFull(err) {
		return "storage", "Disk is full. Free space on the drive holding Desk's configuration and try again."
	}
	if errors.Is(err, codexbridge.ErrAccountLookup) {
		return "connection-lost", "Desk could not reach ChatGPT to check the account. Check your internet connection and try again."
	}
	return "", ""
}
