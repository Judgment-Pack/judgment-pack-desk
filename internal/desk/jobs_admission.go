package desk

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

const jobsAuthorityFrameLimit = 64 << 10
const jobsAuthorityTimeout = 5 * time.Second

type runnerExecutorAdmission struct {
	Version          int    `json:"version"`
	FD               int    `json:"fd"`
	InstallationID   string `json:"installationId"`
	MachineWorkspace string `json:"machineWorkspace"`
	RunnerWorkspace  string `json:"runnerWorkspace"`
	ExecutorID       string `json:"executorId"`
	Challenge        string `json:"challenge"`
	Profile          string `json:"profile"`
}
type jobsAuthorityRequest struct {
	Version  int             `json:"version"`
	Sequence string          `json:"sequence"`
	Method   string          `json:"method"`
	Body     json.RawMessage `json:"body"`
}
type jobsAuthorityReply struct {
	Version  int    `json:"version"`
	Sequence string `json:"sequence"`
	Method   string `json:"method"`
	Status   string `json:"status"`
	Body     any    `json:"body"`
}
type jobsAuthorityRefusal struct {
	Code string `json:"code"`
}
type jobsAuthorityChannel struct {
	conn   net.Conn
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	err    error
	done   chan struct{}
	once   sync.Once
}

func (c *jobsAuthorityChannel) failure() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}
func (c *jobsAuthorityChannel) fault(err error) {
	c.mu.Lock()
	if c.err == nil {
		c.err = err
	}
	c.mu.Unlock()
	c.cancel()
	c.once.Do(func() { c.conn.Close() })
}
func (c *jobsAuthorityChannel) close() {
	if c != nil {
		c.fault(errors.New("manual authority channel closed"))
		<-c.done
	}
}

func newJobsAuthorityChannel(core policyExecutionService, owned func() error) (*jobsAuthorityChannel, *os.File, runnerExecutorAdmission, error) {
	return newJobsAuthorityChannelWithSocket(core, owned, jobsAuthoritySocket)
}
func newJobsAuthorityChannelWithSocket(core policyExecutionService, owned func() error, socket func() (net.Conn, *os.File, error)) (*jobsAuthorityChannel, *os.File, runnerExecutorAdmission, error) {
	var boot runnerExecutorAdmission
	if owned == nil || !core.identity.valid() || core.repository.anchor == nil {
		return nil, nil, boot, policyMachineRefusal()
	}
	conn, child, err := socket()
	if err != nil {
		return nil, nil, boot, err
	}
	sessionID, err := executionRandomID()
	if err != nil {
		conn.Close()
		child.Close()
		return nil, nil, boot, err
	}
	challenge, err := executionRandomID()
	if err != nil {
		conn.Close()
		child.Close()
		return nil, nil, boot, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &jobsAuthorityChannel{conn: conn, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	// The private inherited channel is the authority context. Browser sessions
	// are independently checked at intent/registration capture, never retained
	// as a token or lifetime credential for this worker.
	core.authorize = func() bool { return ctx.Err() == nil }
	session := policyExecutionSession{core.identity, sessionID, challenge, owned}
	boot = runnerExecutorAdmission{1, 3, core.identity.InstallationID, core.identity.MachineWorkspace, core.identity.RunnerWorkspace, core.identity.ExecutorID, challenge, policyExecutionProfile}
	go c.serve(core, session)
	return c, child, boot, nil
}
func (c *jobsAuthorityChannel) serve(core policyExecutionService, session policyExecutionSession) {
	defer close(c.done)
	defer c.fault(errors.New("manual authority channel unavailable; Jobs is held"))
	reader := bufio.NewReaderSize(c.conn, jobsAuthorityFrameLimit)
	next := uint64(1)
	active := false
	for {
		if !active {
			if c.conn.SetReadDeadline(time.Now().Add(jobsAuthorityTimeout)) != nil {
				return
			}
		}
		// After activation, idle time is not an exchange timeout. Bound the
		// remainder of a frame once its first byte arrives.
		if _, err := reader.Peek(1); err != nil {
			return
		}
		if c.conn.SetReadDeadline(time.Now().Add(jobsAuthorityTimeout)) != nil {
			return
		}
		line, err := reader.ReadSlice('\n')
		if err != nil || len(line) > jobsAuthorityFrameLimit || len(line) < 2 || reader.Buffered() != 0 {
			return
		}
		var request jobsAuthorityRequest
		if executionDecode(line[:len(line)-1], &request) != nil || request.Version != 1 || request.Sequence != strconv.FormatUint(next, 10) || !executionPositive(request.Sequence) || !contains([]string{"activate", "inspect", "reserve", "commitStart", "history"}, request.Method) || (request.Method == "activate") == active {
			return
		}
		var body any
		var refusal error
		switch request.Method {
		case "activate":
			var q policyExecutionActivateRequest
			if executionDecode(request.Body, &q) != nil || !retainedArtifactDigest.MatchString(q.Challenge) || !q.Signing.valid() {
				return
			}
			var r policyExecutionActivateResult
			r, refusal = core.activate(c.ctx, session, q)
			body = r
			if refusal == nil && r.Delivery == "committed" {
				active = true
			} else {
				if refusal == nil {
					refusal = executionFailure("operation-held")
				}
			}
		case "inspect":
			var q struct {
				Resource policyExecutionResource `json:"resource"`
			}
			if executionDecode(request.Body, &q) != nil || !q.Resource.valid() {
				return
			}
			body, refusal = core.inspect(c.ctx, session, q.Resource)
		case "reserve":
			var q policyExecutionReserveRequest
			if executionDecode(request.Body, &q) != nil || !validJobsAuthorityReserve(q) {
				return
			}
			body, refusal = core.reserve(c.ctx, session, q)
		case "commitStart":
			var q policyExecutionStartRequest
			if executionDecode(request.Body, &q) != nil || !validJobsAuthorityReserve(q.reserve()) || !policyExecutionID.MatchString(q.OperationID) || !policyExecutionID.MatchString(q.AttemptID) || !executionPositive(q.Epoch) {
				return
			}
			body, refusal = core.commitStart(c.ctx, session, q)
		case "history":
			var q struct {
				IntentID string `json:"intentId"`
			}
			if executionDecode(request.Body, &q) != nil || !policyExecutionID.MatchString(q.IntentID) {
				return
			}
			body, refusal = core.history(c.ctx, session, q.IntentID)
		}
		status := "ok"
		if refusal != nil {
			status = "refused"
			body = jobsAuthorityRefusal{policyExecutionFailureCode(refusal)}
		}
		reply, err := json.Marshal(jobsAuthorityReply{1, request.Sequence, request.Method, status, body})
		if err != nil || len(reply)+1 > jobsAuthorityFrameLimit {
			return
		}
		if c.conn.SetWriteDeadline(time.Now().Add(jobsAuthorityTimeout)) != nil {
			return
		}
		if _, err = io.Copy(c.conn, bytes.NewReader(append(reply, '\n'))); err != nil {
			return
		}
		if !active || next == math.MaxUint64 {
			return
		}
		next++
		if c.conn.SetDeadline(time.Time{}) != nil {
			return
		}
	}
}
func validJobsAuthorityReserve(q policyExecutionReserveRequest) bool {
	return policyExecutionID.MatchString(q.IntentID) && q.Resource.valid() && q.Registration.valid() && executionPayloadShape(q.Payload) && policyExecutionDigest.MatchString(q.RawRequestDigest)
}

// Called inside an anchored transaction: no registry or ledger lock acquisition
// is allowed here. The repository verified the registry before taking anchor.
func verifyRunnerLeaseHeld(o *runnerExecutorOwnership) error {
	file, err := openPinnedExecutorLease(o)
	if err != nil {
		return err
	}
	defer file.Close()
	if err = lockPrivateData(file, false); !lockHeld(err) {
		return policyMachineRefusal()
	}
	return nil
}
func openPinnedExecutorLease(o *runnerExecutorOwnership) (*os.File, error) {
	if o == nil {
		return nil, policyMachineRefusal()
	}
	root, err := openPrivateDataRoot(o.Root, false)
	if err != nil {
		return nil, policyMachineRefusal()
	}
	defer root.Close()
	info, err := root.Stat(".")
	if err != nil || identityKey(info) != o.RootDevice+":"+o.RootInode {
		return nil, policyMachineRefusal()
	}
	file, err := root.OpenFile(o.MachineWorkspace+".lease", os.O_RDWR|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return nil, policyMachineRefusal()
	}
	success := false
	defer func() {
		if !success {
			file.Close()
		}
	}()
	held, err := file.Stat()
	if err != nil {
		return nil, policyMachineRefusal()
	}
	links, known := linkCount(held)
	if !known || links != 1 || held.Size() != 0 || identityKey(held) != o.FileDevice+":"+o.FileInode || ownerOnlyFile("executor lease", held.Mode()) != nil || ownedByUs("executor lease", held) != nil {
		return nil, policyMachineRefusal()
	}
	entry, err := root.Lstat(o.MachineWorkspace + ".lease")
	if err != nil || !os.SameFile(held, entry) {
		return nil, policyMachineRefusal()
	}
	success = true
	return file, nil
}
