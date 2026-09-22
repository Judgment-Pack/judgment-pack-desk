package desk

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestConnectionDrainHelper(t *testing.T) {
	if os.Getenv("DESK_TEST_DRAIN_HELPER") != "1" {
		return
	}
	var request map[string]any
	if json.NewDecoder(os.Stdin).Decode(&request) != nil {
		os.Exit(3)
	}
	dir := os.Getenv("DESK_TEST_DRAIN_DIR")
	if os.WriteFile(filepath.Join(dir, "started"), []byte("started"), 0600) != nil {
		os.Exit(4)
	}
	time.Sleep(2300 * time.Millisecond) // Longer than the old two-second SIGKILL.
	if os.WriteFile(filepath.Join(dir, "committed"), []byte("synthetic commit"), 0600) != nil {
		os.Exit(5)
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"id": request["id"], "result": map[string]bool{"committed": true}})
	io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

func TestCanceledConnectionRequestDrainsBeforeStoppingCompanion(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestConnectionDrainHelper$")
	cmd.Env = append(os.Environ(), "DESK_TEST_DRAIN_HELPER=1", "DESK_TEST_DRAIN_DIR="+dir)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &connectionCompanion{cmd: cmd, input: input, output: bufio.NewReaderSize(output, 64<<10), done: make(chan struct{})}
	doneProcess := c.done
	go func() { cmd.Wait(); close(doneProcess) }()
	t.Cleanup(c.close)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.call(ctx, "", "", "search", json.RawMessage(`{}`), "notion", false); done <- err }()
	until := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "started")); err == nil {
			break
		}
		if time.Now().After(until) {
			t.Fatal("helper did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled request accepted")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("drain did not complete")
	}
	if _, err := os.Stat(filepath.Join(dir, "committed")); err != nil {
		t.Fatal("companion killed before its durable commit", err)
	}
	if c.cmd != nil {
		t.Fatal("canceled companion not retired")
	}
	// No stale reply or process may survive into a later cleanup RPC.
	reply, err := c.call(context.Background(), "", "", "cancel", json.RawMessage(`{}`), "notion", true)
	if err != nil || string(reply) != `{"state":"canceled"}` {
		t.Fatal(string(reply), err)
	}
}
