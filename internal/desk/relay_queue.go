package desk

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	// maxRelayQueued is how many model-relay requests may wait for a slot at
	// once, behind the maxRelayInFlight that are being carried.
	//
	// **A queue, and a bounded one.** A session that delegates runs its
	// children beside the parent, up to the connection's capacity (sixteen),
	// and a fifth request at the relay is then the ordinary case, not a fault:
	// refusing it made the harness report a failure for work the desk could
	// have done a moment later. Waiting is the answer, and the wait is bounded
	// twice, because an unbounded queue is the other thing the relay's four
	// slots were chosen to avoid, a desk holding a hundred sockets and a
	// hundred deadlines for a page that fired a hundred requests: by how many
	// may wait (this), and by how long one may (`relayQueueWait`). One more
	// than this many is refused at once, 429 with `Retry-After`, which the
	// harness reads and sends again.
	maxRelayQueued = 16
)

// relayQueueWait is how long one request waits for a slot before it is
// answered busy. A var for exactly one reason: a test shortens it, so that the
// bound can be shown to apply against slots that never come back. A slot is
// itself bounded (the overall and idle deadlines of the relay), so this is
// what stops a request waiting behind a slot those would take ten minutes to
// end, and it is shorter than the harness's own turn clock.
var relayQueueWait = 60 * time.Second

// relayLine orders the requests waiting for a turn: each waits until every one
// that joined before it has been let through or has left, so a request is
// never kept waiting behind ones that came after it. Only the request at the
// head of the line competes for the connection's room and for a slot.
type relayLine struct {
	mu    sync.Mutex
	queue []chan struct{}
}

// empty says whether nobody is waiting.
func (l *relayLine) empty() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.queue) == 0
}

// join puts a request at the end of the line. Its turn arrives on the returned
// channel, and leave gives the place up, whether the request was served or not.
func (l *relayLine) join() (turn <-chan struct{}, leave func()) {
	mine := make(chan struct{})
	l.mu.Lock()
	l.queue = append(l.queue, mine)
	if len(l.queue) == 1 {
		close(mine)
	}
	l.mu.Unlock()
	var once sync.Once
	return mine, func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			for at, held := range l.queue {
				if held != mine {
					continue
				}
				l.queue = append(l.queue[:at], l.queue[at+1:]...)
				if at == 0 && len(l.queue) > 0 {
					close(l.queue[0])
				}
				return
			}
		})
	}
}

// relayAdmission is a request let through: the slot and the connection's room
// it holds, which it gives back with release, and what getting them cost.
type relayAdmission struct {
	release func()
	// waited is how long the request waited in the queue, zero where it was
	// let through at once, and depth is how many were waiting when it joined,
	// itself among them. Both go back to the page, which shows them in the
	// run's report.
	waited time.Duration
	depth  int
}

// relayRefusal is a request not let through, and what to tell the page.
type relayRefusal struct {
	status     int
	code       string
	message    string
	retryAfter string
}

func (r *relayRefusal) write(w http.ResponseWriter) {
	if r.retryAfter != "" {
		w.Header().Set("Retry-After", r.retryAfter)
	}
	writeJSONCoded(w, r.status, r.code, r.message)
}

// Headers the relay's answer carries when its request waited.
const (
	relayQueueWaitHeader  = "X-Relay-Queue-Wait-Ms"
	relayQueueDepthHeader = "X-Relay-Queue-Depth"
)

// admitRelay gives a request a slot and its connection's room, waiting for
// both in a bounded queue where they are not there at once.
//
// Taken without waiting where both are free, which is the whole of the path a
// desk that is not busy takes. Otherwise the request takes a place in the
// queue (a full queue is the one refusal that is an answer, 429 with
// `Retry-After: 1`), waits for the connection's room (the number of requests
// its endpoint takes at once, shorter while the endpoint is rate-limiting) and
// then for a slot, which hands them out in the order the requests arrived. The
// wait ends with the request, or after `relayQueueWait`, when it is answered
// busy: nothing was sent, and the same request a moment later is fine.
func (s *Server) admitRelay(ctx context.Context, capacityKey string) (relayAdmission, *relayRefusal) {
	// Only where nobody is waiting: a slot that frees while others wait is theirs, in their order.
	if room, ok := modelCapacity.Try(capacityKey); ok && s.relayLine.empty() {
		select {
		case s.relaySlots <- struct{}{}:
			return relayAdmission{release: s.relayRelease(room)}, nil
		default:
			room()
		}
	} else if ok {
		room()
	}
	// Joined before the place is counted, so a request that is seen to be
	// waiting is already in the line: the order the queue serves is the order
	// the counts show.
	turn, leave := s.relayLine.join()
	defer leave()
	select {
	case s.relayQueue <- struct{}{}:
	default:
		return relayAdmission{}, &relayRefusal{
			status: http.StatusTooManyRequests, code: CodeAssistantRelayQueueFull, retryAfter: "1",
			message: fmt.Sprintf("this desk is carrying %d requests to the endpoint and %d are waiting for a turn; "+
				"nothing was sent, and the same request will be taken a moment later", maxRelayInFlight, maxRelayQueued),
		}
	}
	defer func() { <-s.relayQueue }()
	depth := len(s.relayQueue)
	started := time.Now()
	waiting, cancel := context.WithTimeout(ctx, relayQueueWait)
	defer cancel()
	busy := func() (relayAdmission, *relayRefusal) {
		return relayAdmission{}, &relayRefusal{
			status: http.StatusServiceUnavailable, code: CodeAssistantRelayBusy,
			message: fmt.Sprintf("this desk is already carrying %d requests to the endpoint, and this one waited %d seconds "+
				"for a turn; nothing was sent", maxRelayInFlight, int(relayQueueWait/time.Second)),
		}
	}
	select {
	case <-turn:
	case <-waiting.Done():
		return refusedWhileWaiting(ctx, busy)
	}
	room, err := modelCapacity.Wait(waiting, capacityKey)
	if err != nil {
		return refusedWhileWaiting(ctx, busy)
	}
	select {
	case s.relaySlots <- struct{}{}:
		return relayAdmission{release: s.relayRelease(room), waited: time.Since(started), depth: depth}, nil
	case <-waiting.Done():
		room()
		return refusedWhileWaiting(ctx, busy)
	}
}

// refusedWhileWaiting is the answer to a request whose wait ended without a
// turn: busy where the wait bound ran out, and, where the page had already
// gone, a refusal nobody reads.
func refusedWhileWaiting(ctx context.Context, busy func() (relayAdmission, *relayRefusal)) (relayAdmission, *relayRefusal) {
	if ctx.Err() != nil {
		return relayAdmission{}, &relayRefusal{status: http.StatusServiceUnavailable, code: CodeAssistantRelayBusy, message: "the request ended before it was taken; nothing was sent"}
	}
	return busy()
}

// relayRelease gives back a slot and the connection's room, once.
func (s *Server) relayRelease(room func()) func() {
	released := false
	return func() {
		if released {
			return
		}
		released = true
		<-s.relaySlots
		room()
	}
}

// markQueued puts the wait on an answer the relay forwards, where there was
// one: how long the request waited for a turn, in milliseconds, and how many
// were waiting when it joined.
func (a relayAdmission) markQueued(header http.Header) {
	// Whatever the endpoint said under these names goes, so the page reads this
	// desk's account of the wait and never an endpoint's.
	header.Del(relayQueueWaitHeader)
	header.Del(relayQueueDepthHeader)
	if a.waited <= 0 {
		return
	}
	header.Set(relayQueueWaitHeader, strconv.FormatInt(a.waited.Milliseconds(), 10))
	header.Set(relayQueueDepthHeader, strconv.Itoa(a.depth))
}
