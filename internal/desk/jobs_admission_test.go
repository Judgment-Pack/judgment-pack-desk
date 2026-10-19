package desk

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

func authorityTestClient(t *testing.T, core policyExecutionService) (*jobsAuthorityChannel, net.Conn, runnerExecutorAdmission) {
	t.Helper()
	channel, child, boot, err := newJobsAuthorityChannel(core, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.FileConn(child)
	child.Close()
	if err != nil {
		channel.close()
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(); channel.close() })
	return channel, conn, boot
}
func authorityTestExchange(t *testing.T, conn net.Conn, sequence, method string, body any) jobsAuthorityReply {
	t.Helper()
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"version": 1, "sequence": sequence, "method": method, "body": body})
	if _, err := conn.Write(append(raw, '\n')); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var result jobsAuthorityReply
	if executionDecode(line[:len(line)-1], &result) != nil {
		t.Fatal("invalid reply", string(line))
	}
	return result
}
func TestJobsAuthorityInheritedServerAndPermanentProtocolHold(t *testing.T) {
	f := newExecutionFixture(t)
	channel, client, boot := authorityTestClient(t, f.core)
	activate := authorityTestExchange(t, client, "1", "activate", policyExecutionActivateRequest{boot.Challenge, policyExecutionSigning{Mode: "off"}})
	if activate.Status != "ok" {
		t.Fatal(activate)
	}
	inspect := authorityTestExchange(t, client, "2", "inspect", struct {
		Resource policyExecutionResource `json:"resource"`
	}{f.registration.Resource})
	if inspect.Status != "ok" {
		t.Fatal(inspect)
	}
	denied := authorityTestExchange(t, client, "3", "history", struct {
		IntentID string `json:"intentId"`
	}{"intent_missing"})
	if denied.Status != "refused" {
		t.Fatal("history absence granted result", denied)
	}
	if got := authorityTestExchange(t, client, "4", "inspect", struct {
		Resource policyExecutionResource `json:"resource"`
	}{f.registration.Resource}); got.Status != "ok" {
		t.Fatal("domain refusal closed channel", got)
	}
	// A second activation is a permanent protocol fault, without another epoch.
	line, _ := json.Marshal(map[string]any{"version": 1, "sequence": "5", "method": "activate", "body": policyExecutionActivateRequest{boot.Challenge, policyExecutionSigning{Mode: "off"}}})
	_, _ = client.Write(append(line, '\n'))
	var one [1]byte
	if _, err := client.Read(one[:]); err == nil {
		t.Fatal("replayed activation answered")
	}
	select {
	case <-channel.done:
	case <-time.After(time.Second):
		t.Fatal("fault did not close server")
	}
	if channel.failure() == nil || len(f.ledger(t).Execution.Epochs) != 2 {
		t.Fatal("fault reopened or lost epoch")
	}
}

func TestJobsAuthorityClosedFrameRefusals(t *testing.T) {
	f := newExecutionFixture(t)
	for name, frame := range map[string][]byte{
		"phase":     []byte("{\"version\":1,\"sequence\":\"1\",\"method\":\"history\",\"body\":{\"intentId\":\"intent_missing\"}}\n"),
		"sequence":  []byte("{\"version\":1,\"sequence\":\"01\",\"method\":\"activate\",\"body\":{}}\n"),
		"case":      []byte("{\"Version\":1,\"sequence\":\"1\",\"method\":\"activate\",\"body\":{}}\n"),
		"null":      []byte("{\"version\":1,\"sequence\":\"1\",\"method\":\"activate\",\"body\":null}\n"),
		"duplicate": []byte("{\"version\":1,\"version\":1,\"sequence\":\"1\",\"method\":\"activate\",\"body\":{}}\n"),
		"oversize":  append(bytes.Repeat([]byte{' '}, jobsAuthorityFrameLimit), byte('\n')),
	} {
		t.Run(name, func(t *testing.T) {
			channel, conn, _ := authorityTestClient(t, f.core)
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			_, _ = conn.Write(frame)
			if _, err := io.Copy(io.Discard, conn); err != nil {
				if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
					t.Fatal("malformed frame did not close channel")
				}
			}
			select {
			case <-channel.done:
			case <-time.After(time.Second):
				t.Fatal("server leaked")
			}
			if channel.failure() == nil {
				t.Fatal("protocol failure was not sticky")
			}
		})
	}
	if len(f.ledger(t).Execution.Epochs) != 1 {
		t.Fatal("invalid frame activated authority")
	}
}

func TestJobsAuthorityOwnershipVerificationCannotUseAnIdleLease(t *testing.T) {
	f := newPolicyMachineFixture(t)
	f.command(t, "enroll")
	f.command(t, "enroll-executor")
	m, err := openPolicyMachine(f.options, false)
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	o, err := m.executorForProject(f.s.project, "runner-distinct")
	if err != nil {
		t.Fatal(err)
	}
	if verifyRunnerLeaseHeld(o) == nil {
		t.Fatal("idle lease proved active executor")
	}
	root, err := openPrivateDataRoot(o.Root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	file, err := root.OpenFile(o.MachineWorkspace+".lease", os.O_RDWR|openNoFollow, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if lockPrivateData(file, true) != nil {
		t.Fatal("cannot hold fixture lease")
	}
	if err = verifyRunnerLeaseHeld(o); err != nil {
		t.Fatal("held exact lease did not prove ownership", err)
	}
}
