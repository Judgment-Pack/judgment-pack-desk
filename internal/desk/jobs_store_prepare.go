package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"time"
)

type runnerStorePrepareRequest struct {
	Version           int                     `json:"version"`
	PrepareID         string                  `json:"prepareId"`
	Dir               string                  `json:"dir"`
	Workspace         string                  `json:"workspace"`
	Owner             string                  `json:"owner"`
	LeaseFD           int                     `json:"leaseFd"`
	ExecutorOwnership runnerExecutorOwnership `json:"executorOwnership"`
	ExecutionDemand   policyExecutionDemand   `json:"executionDemand"`
}
type runnerStorePrepareResult struct {
	Version       int    `json:"version"`
	Status        string `json:"status"`
	PrepareID     string `json:"prepareId"`
	RequestDigest string `json:"requestDigest"`
	Delivery      string `json:"delivery"`
	StoreSchema   string `json:"storeSchema"`
}

// Caller retains the same EX lease continuously. ExtraFiles duplicates that
// held open description at child fd3; neither process explicitly unlocks it.
// Only a fresh committed reply permits the caller's next activation step.
func prepareRunnerManualStore(ctx context.Context, bin string, lease *os.File, q runnerStorePrepareRequest) error {
	if lease == nil || q.Version != 1 || q.LeaseFD != 3 || !retainedArtifactDigest.MatchString(q.PrepareID) || !q.ExecutionDemand.valid() || q.Workspace != q.ExecutionDemand.RunnerWorkspace || q.ExecutorOwnership.RunnerWorkspace != q.Workspace || q.ExecutorOwnership.MachineWorkspace != q.ExecutionDemand.MachineWorkspace {
		return policyMachineRefusal()
	}
	raw, err := json.Marshal(q)
	if err != nil || len(raw)+1 > 16<<10 {
		return policyMachineRefusal()
	}
	call, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(call, bin, "prepare-manual-store")
	cmd.Dir = q.Dir
	cmd.Env = []string{"LANG=C", "LC_ALL=C"}
	cmd.Stdin = bytes.NewReader(append(bytes.Clone(raw), '\n'))
	cmd.ExtraFiles = []*os.File{lease}
	cmd.WaitDelay = 100 * time.Millisecond
	out, said := &cappedBuffer{limit: 4096}, &cappedBuffer{limit: 4096}
	cmd.Stdout = out
	cmd.Stderr = said
	if cmd.Run() != nil || call.Err() != nil || out.exceeded() || said.exceeded() || len(said.Bytes()) != 0 {
		return policyMachineRefusal()
	}
	line := out.Bytes()
	if len(line) < 2 || line[len(line)-1] != '\n' || bytes.Count(line, []byte{'\n'}) != 1 {
		return policyMachineRefusal()
	}
	var result runnerStorePrepareResult
	if executionDecode(line[:len(line)-1], &result) != nil || result.Version != 1 || result.Status != "ok" || result.PrepareID != q.PrepareID || result.RequestDigest != "sha256:"+digestOf(raw) || result.Delivery != "committed" || result.StoreSchema != "4" {
		return policyMachineRefusal()
	}
	return nil
}
