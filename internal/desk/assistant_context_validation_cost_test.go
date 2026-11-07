package desk

// Temporary, test-only diagnostic. Keep the actual two-turn production path,
// all assertions, exact receipts and the existing settlement deadline unchanged.
import (
	"context"
	"runtime/pprof"
	"testing"
	"time"
)

func assistantContextMeasureSettledLedgerDecode(t *testing.T, s *Server) {
	t.Helper()
	store := s.assistantRuns()
	if store.problem != nil {
		t.Fatal("validation cost diagnostic: namespace unavailable")
	}
	// Capture independent bytes with the same rooted custody and scope checks
	// as production, then release custody before doing any diagnostic decoding.
	captured, err := func() (assistantRunCapturedRead, error) {
		lock, err := s.privateDataLockContext(t.Context(), true)
		if err != nil {
			return assistantRunCapturedRead{}, err
		}
		defer lock.Close()
		data, err := s.openChatData()
		if err != nil {
			return assistantRunCapturedRead{}, err
		}
		defer data.root.Close()
		return store.captureLocked(data)
	}()
	if err != nil || captured.missing {
		t.Fatal("validation cost diagnostic: settled capture unavailable")
	}
	// Exactly one full production decode. The fixed label lets pprof exclude
	// this measurement from application CPU, or focus on its finite cost.
	var ledger assistantRunLedger
	var elapsed time.Duration
	pprof.Do(t.Context(), pprof.Labels("context_validation", "settled_ledger_measurement"), func(ctx context.Context) {
		started := time.Now()
		ledger, err = captured.decode(ctx)
		elapsed = time.Since(started)
	})
	if err != nil {
		t.Fatal("validation cost diagnostic: settled decode refused")
	}
	events, eventBytes, commits, headBytes, dispatches := 0, 0, 0, 0, 0
	for _, run := range ledger.Runs {
		if journal := run.Canonical; journal != nil {
			events += len(journal.Events)
			commits += len(journal.Commits)
			headBytes += len(journal.Head.Checkpoint)
			for _, event := range journal.Events {
				eventBytes += len(event.Bytes)
			}
		}
		if run.Checkpoint != nil && run.Checkpoint.Durable != nil {
			dispatches += len(run.Checkpoint.Durable.Dispatches)
		}
	}
	t.Logf("settled ledger validation cost: decode_ns=%d bytes=%d runs=%d captures=%d canonical_events=%d canonical_event_bytes=%d canonical_commits=%d canonical_head_bytes=%d durable_dispatches=%d", elapsed.Nanoseconds(), len(captured.raw), len(ledger.Runs), len(ledger.AuthoringCaptures), events, eventBytes, commits, headBytes, dispatches)
}
