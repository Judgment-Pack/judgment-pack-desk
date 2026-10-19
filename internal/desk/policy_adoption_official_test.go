package desk

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func adoptionOfficialMaterial(t *testing.T, service policyService, q policyAdoptionRequest) policyAdoptionMaterial {
	t.Helper()
	var material policyAdoptionMaterial
	err := service.repository.View(context.Background(), func(l policyLedger) error {
		pack, err := service.coveragePack(context.Background(), *q.Pack)
		if err != nil {
			return err
		}
		material, err = service.adoptionResolve(context.Background(), l, pack, q.Coverages)
		material.PortableID = "urn:desk:adoption-candidate:fixture"
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return material
}

func TestPolicyAdoptionOfficialDraftRetainsLegacyCandidateEvidence(t *testing.T) {
	_, _, service, _, q := adoptionFixture(t)
	material := adoptionOfficialMaterial(t, service, q)
	checked := adoptionAcceptedFixture(context.Background(), material)
	if !adoptionCheckAccepted(checked) {
		t.Fatal("official draft rejected")
	}
	var report policyCoverageRuntimeReport
	if err := coverageDecode(checked.Report, &report); err != nil {
		t.Fatal(err)
	}
	// This is the exact historical candidate metadata, not a promoted report.
	report.SpecRevision = legacyAdoptionRuntimeContract.SpecRevision
	report.Specification = legacyAdoptionRuntimeContract.Specification
	report.Maturity = legacyAdoptionRuntimeContract.Maturity
	report.Capabilities.Maturity = report.Maturity
	report.Capabilities.ValidationScope = legacyAdoptionRuntimeContract.ValidationScope
	report.Capabilities.CheckScope = legacyAdoptionRuntimeContract.CheckScope
	report.ValidationScope = legacyAdoptionRuntimeContract.CheckScope
	report.CandidateCapabilities = json.RawMessage(legacyAdoptionRuntimeCapabilities)
	report.CandidateResult.Maturity = report.Maturity
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := service.artifacts.Put(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	content := policyAdoptionContent{PortableID: material.PortableID}
	if err := archivedAdoptionReport(context.Background(), service.artifacts, ref, content, checked.prepared.Document); err != nil {
		t.Fatal("historical candidate evidence became unreadable", err)
	}
	if adoptionCheckAccepted(policyAdoptionCheck{Availability: "available", Report: raw}) {
		t.Fatal("legacy candidate report authorized a fresh official draft check")
	}
	retained, err := service.artifacts.Get(context.Background(), ref)
	if err != nil || string(retained) != string(raw) {
		t.Fatal("historical report changed", err)
	}
	for _, scenario := range []string{"hybrid-maturity", "hybrid-capabilities", "wrong-source", "missing-result", "authority"} {
		t.Run(scenario, func(t *testing.T) {
			var broken policyCoverageRuntimeReport
			if err := coverageDecode(raw, &broken); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "hybrid-maturity":
				broken.CandidateResult.Maturity = "draft"
			case "hybrid-capabilities":
				broken.CandidateCapabilities = json.RawMessage(adoptionRuntimeCapabilities)
			case "wrong-source":
				broken.Specification = adoptionRuntimeContract.Specification
			case "missing-result":
				broken.CandidateResult = nil
			case "authority":
				broken.Capabilities.ExecutionAuthority = true
			}
			bad, _ := json.Marshal(broken)
			badRef, err := service.artifacts.Put(context.Background(), bad)
			if err != nil {
				t.Fatal(err)
			}
			if archivedAdoptionReport(context.Background(), service.artifacts, badRef, content, checked.prepared.Document) == nil {
				t.Fatal("inconsistent historical evidence accepted")
			}
		})
	}
}

func TestPolicyAdoptionOfficialDraftRuntimeFaultsPreserveHistory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fault fixture")
	}
	s, _, service, actor, q := adoptionFixture(t)
	prepared, err := service.adoptionCommand(context.Background(), actor, q)
	if err != nil {
		t.Fatal(err)
	}
	material := adoptionOfficialMaterial(t, service, q)
	checked := adoptionAcceptedFixture(context.Background(), material)
	var base policyCoverageRuntimeReport
	if err := coverageDecode(checked.Report, &base); err != nil {
		t.Fatal(err)
	}
	base.CandidateResult = nil
	base.Status = "available"
	encoded, _ := json.Marshal(base)
	for _, scenario := range []string{"official", "old-runtime", "malformed", "wrong-capabilities", "missing-result", "wrong-result-maturity", "refused", "bad-refusal-code", "wrong-digest", "authority", "failed-process"} {
		t.Run(scenario, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "runtime-fixture")
			script := `#!/usr/bin/env python3
import hashlib,json,sys
r=json.loads(` + string(mustJSON(string(encoded))) + `)
scenario=` + string(mustJSON(scenario)) + `
op=sys.argv[3]
assert '--format-version' in sys.argv and sys.argv[sys.argv.index('--format-version')+1]=='1.0.0'
r['command']='experimental policy '+op
if op=='contract':
 if scenario=='old-runtime': r['specRevision']='0852c5fa7726e51b36e574a5b110e7f5e012e03d';r['maturity']='proposed-draft'
 if scenario=='malformed': print('{broken');sys.exit(0)
 if scenario=='wrong-capabilities': r['candidateCapabilities']['limits']['sections']=65
 if scenario=='authority': r['capabilities']['executionAuthority']=True
 print(json.dumps(r));sys.exit(0)
data=sys.stdin.buffer.read();v=json.loads(data)
r.update(status='encoded',kind=v['kind'],artifactId=v['id'],artifactVersion=v['version'],bytesDigest='sha256:'+hashlib.sha256(data).hexdigest(),canonicalDigest='sha256:'+hashlib.sha256(data).hexdigest())
if op=='canonicalize':
 r['document']=data.decode();print(json.dumps(r));sys.exit(0)
r.update(status='references-valid',validationScope='draft-supplied-exact-bytes-anchors-and-scoped-core-references',candidateResult=dict(status='valid',formatVersion='1.0.0',maturity='draft'))
if scenario=='missing-result': del r['candidateResult']
if scenario=='wrong-result-maturity': r['candidateResult']['maturity']='proposed-draft'
if scenario=='wrong-digest': r['bytesDigest']='sha256:'+'0'*64
if scenario in ('refused','bad-refusal-code'):
 r['status']='refused';r['candidateResult']=dict(status='refused',code='missing-material' if scenario=='refused' else 'unknown')
 r['diagnostics']=[dict(code='JPS-POLICY-MISSING-MATERIAL',codeStability='provisional',layer='policy',severity='error',instancePath='/consumer',message='Controlled refusal')]
 print(json.dumps(r));sys.exit(1)
print(json.dumps(r));sys.exit(1 if scenario=='failed-process' else 0)
`
			if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			s.cfg.JpackBin = bin
			got := s.checkPolicyAdoption(context.Background(), material)
			switch scenario {
			case "official":
				if !adoptionCheckAccepted(got) || got.prepared == nil {
					t.Fatal("official Runtime contract failed", got, string(got.Report))
				}
			case "refused":
				if got.Availability != "available" || got.prepared != nil || adoptionCheckAccepted(got) {
					t.Fatal("strict structured refusal lost", got)
				}
			default:
				if got.Availability != "unavailable" || got.prepared != nil || !strings.Contains(got.Reason, "policy-v1.0.0-draft.1") {
					t.Fatal("incompatible Runtime accepted", got)
				}
			}
			// Fresh failure cannot erase retained candidate/history or replay a write.
			history, err := service.adoptionGet(context.Background(), prepared.Adoption.Document.ID)
			if err != nil || history.Document.Candidate.Check != prepared.Adoption.Document.Candidate.Check {
				t.Fatal("saved history lost", err)
			}
		})
	}
}
