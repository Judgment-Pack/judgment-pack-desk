package desk

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"time"
)

type runnerExecutionBinding struct {
	Version        int    `json:"version"`
	Workspace      string `json:"workspace"`
	JobID          string `json:"jobId"`
	JobRevision    string `json:"jobRevision"`
	ReleaseID      string `json:"releaseId"`
	ReleaseProfile string `json:"releaseProfile"`
	ReleaseDigest  string `json:"releaseDigest"`
	ReleaseBytes   string `json:"releaseBytes"`
}

// Direct owner-only read, deliberately absent from the generic browser route
// allowlist. No caller endpoint, header, redirect, proxy or release hash parser.
func (j *jobsCompanion) executionBinding(ctx context.Context, job string) (policyExecutionResolvedRelease, error) {
	var result policyExecutionResolvedRelease
	if !policyExecutionID.MatchString(job) {
		return result, executionFailure("invalid-request")
	}
	endpoint, token, err := j.endpoint()
	if err != nil {
		return result, executionFailure("authority-unavailable")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/v1/jobs/"+url.PathEscape(job)+"/execution-binding", nil)
	if err != nil {
		return result, executionFailure("authority-unavailable")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	client := http.Client{Timeout: jobsAuthorityTimeout, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return result, executionFailure("authority-unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return result, executionFailure("resource-unregistered")
	}
	const limit = (8<<20)*4/3 + 8192
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(raw) > limit {
		return result, executionFailure("authority-unavailable")
	}
	var binding runnerExecutionBinding
	if executionDecode(raw, &binding) != nil || binding.Version != 1 || binding.Workspace != j.workspace || binding.JobID != job {
		return result, executionFailure("resource-changed")
	}
	resource := policyExecutionResource{binding.JobID, binding.JobRevision, binding.ReleaseID, binding.ReleaseProfile, binding.ReleaseDigest}
	if !resource.valid() || len(binding.ReleaseBytes) > ((8<<20)+2)/3*4 {
		return result, executionFailure("resource-changed")
	}
	preimage, err := base64.StdEncoding.Strict().DecodeString(binding.ReleaseBytes)
	if err != nil || len(preimage) > 8<<20 || len(preimage) == 0 || base64.StdEncoding.EncodeToString(preimage) != binding.ReleaseBytes || "sha256:"+digestOf(preimage) != resource.ReleaseDigest {
		return result, executionFailure("resource-changed")
	}
	return policyExecutionResolvedRelease{binding.Workspace, resource, preimage}, nil
}

func (s *Server) executionCore() (policyExecutionService, bool, error) {
	var core policyExecutionService
	if s.policyAuthorityMachine == nil {
		return core, false, nil
	}
	r, err := s.policyAuthorityMachine.registry()
	if err != nil {
		return core, false, err
	}
	w, found, err := policyMachineWorkspaceFor(r, s.project)
	if err != nil || !found {
		return core, false, policyMachineRefusal()
	}
	if w.Execution == nil {
		return core, false, nil
	}
	if w.Execution.State != "enabled" || s.jobs == nil || w.Execution.RunnerWorkspace != s.jobs.workspace || s.policyAuthorityAnchor == nil {
		return core, false, policyMachineRefusal()
	}
	repository, ok := s.policyRepository().(filePolicyRepository)
	if !ok {
		return core, false, policyMachineRefusal()
	}
	core = policyExecutionService{repository: repository, artifacts: s.artifactRepository(), identity: executionWorkspaceDemand(r.InstallationID, w).identity(), authorize: func() bool { return false }, clock: time.Now, resolveRelease: s.jobs.executionBinding, resolveAdoption: resolvePolicyExecutionAdoption(s.artifactRepository())}
	return core, true, nil
}
