package desk

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func coverageMaterialFixture(t *testing.T, s *Server, token string, p policyReply, pack policyCoveragePackSelector) (policyService, policyActor, policyCoverageReply, policyCoverageMaterial) {
	t.Helper()
	service, actor := coverageServiceFixture(s, token)
	saved, err := service.coverageCommand(context.Background(), p.Document.ID, actor, coverageSaveBody(t, p, pack))
	if err != nil {
		t.Fatal(err)
	}
	var material policyCoverageMaterial
	err = service.repository.View(context.Background(), func(l policyLedger) error {
		var err error
		record := l.Coverages[saved.Coverage.Document.ID]
		material, err = service.coverageMaterial(context.Background(), l.Policies[p.Document.ID], coverageHead(record))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, actor, *saved.Coverage, material
}
func TestPolicyCoverageRealRuntimeExactMaterialAndRefusal(t *testing.T) {
	bin := os.Getenv("JPACK_POLICY_TEST_BIN")
	if bin == "" {
		t.Skip("set JPACK_POLICY_TEST_BIN to a Runtime with experimental Policy prototype 0.0.2 tools")
	}
	s, token, p, pack := coverageFixture(t)
	s.cfg.JpackBin = bin
	service, actor, saved, material := coverageMaterialFixture(t, s, token, p, pack)
	checked := s.checkPolicyCoverage(context.Background(), material)
	var report policyCoverageRuntimeReport
	if checked.Availability != "available" || json.Unmarshal(checked.Report, &report) != nil || report.Status != "references-valid" || report.Document != "" || checked.prepared == nil {
		t.Fatalf("real Runtime check: %+v %s", checked, checked.Report)
	}
	approved, err := service.coverageCommand(context.Background(), p.Document.ID, actor, coverageReviewBody(t, saved))
	if err != nil || approved.Coverage.Document.ReviewState != "reviewed" {
		t.Fatal("real Runtime approval", err)
	}
	_ = archiveTestCut(t, s)
	changed := material
	changed.Content.Statement.ConsumerElements = []policyCoverageElement{{Kind: "rule", ID: "missing-rule"}}
	refused := s.checkPolicyCoverage(context.Background(), changed)
	if refused.Availability != "available" || json.Unmarshal(refused.Report, &report) != nil || report.Status != "refused" || len(report.Diagnostics) == 0 || report.Diagnostics[0].Code != "JPS-POLICY-ELEMENT-REFERENCE" {
		t.Fatalf("real reference refusal: %+v %s", refused, refused.Report)
	}
	changed = material
	changed.Pack = append(append([]byte{}, material.Pack...), []byte(strings.Repeat(" ", 262145))...)
	if r := s.checkPolicyCoverage(context.Background(), changed); r.Availability != "unsupported" {
		t.Fatal("oversize Pack supported", r)
	}
	// Raw uploaded original, private candidate envelope and narrative remain distinct.
	original := []byte("Exact retained upload 原始\r\n")
	created := policyMutation(t, policyCall(s, token, "POST", "/api/policies", policyEditRequest{OperationID: policyOp(t), Title: "Imported policy", Narrative: "Separate authored narrative 中文\r\n", ArtifactVersion: "1.0.0", Import: &policyImport{Name: "original.txt", MediaType: "text/plain", Data: original}})).Policy
	created = policyApprove(t, s, token, created)
	created = policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+created.Document.ID+"/publish", policyPublishBody(t, created))).Policy
	_, _, _, imported := coverageMaterialFixture(t, s, token, created, pack)
	if imported.OriginalSource == nil || string(imported.Original) != string(original) || string(imported.Original) == imported.Content.Source.Narrative {
		t.Fatal("uploaded original substituted narrative")
	}
	if r := s.checkPolicyCoverage(context.Background(), imported); !coverageCheckAccepted(r) {
		t.Fatalf("real imported check: %+v %s", r, r.Report)
	}
}
func TestPolicyCoverageRuntimeFaultsAndSnapshotCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fault fixture")
	}
	s, token, p, pack := coverageFixture(t)
	_, _, _, material := coverageMaterialFixture(t, s, token, p, pack)
	contract := coverageRuntimeFixtureReport("contract")
	contract.Status = "available"
	encoded, _ := json.Marshal(contract)
	for _, scenario := range []string{"wrong-contract", "wrong-profile", "wrong-provenance", "unknown-field", "malformed-json", "canonical-substitution", "wrong-digest", "wrong-scope", "successful-output-failed-process", "refused", "overflow", "interrupted"} {
		t.Run(scenario, func(t *testing.T) {
			scratch := t.TempDir()
			t.Setenv("TMPDIR", scratch)
			bin := filepath.Join(t.TempDir(), "runtime-fixture")
			script := `#!/usr/bin/env python3
import hashlib,json,sys,time
base=json.loads(` + string(mustJSON(string(encoded))) + `)
scenario=` + string(mustJSON(scenario)) + `
op=sys.argv[3]
r=dict(base);r['command']='experimental policy '+op
if op=='contract':
 if scenario=='wrong-contract': r['contractVersion']='99'
 if scenario=='wrong-profile': r['profileVersion']='0.0.3'
 if scenario=='wrong-provenance': r['specRevision']='0'*40
 if scenario=='unknown-field': r['unexpectedAuthority']=True
 if scenario=='malformed-json': print('{broken');sys.exit(0)
 print(json.dumps(r));sys.exit(0)
data=sys.stdin.buffer.read();value=json.loads(data)
canonical=json.dumps(value,ensure_ascii=False,sort_keys=True,separators=(',',':')).encode()
r.update(status='encoded',kind=value['kind'],artifactId=value['id'],artifactVersion=value['version'],bytesDigest='sha256:'+hashlib.sha256(data).hexdigest(),canonicalDigest='sha256:'+hashlib.sha256(canonical).hexdigest())
if op=='canonicalize':
 if scenario=='canonical-substitution': value['title']='changed';canonical=json.dumps(value,separators=(',',':')).encode();r['canonicalDigest']='sha256:'+hashlib.sha256(canonical).hexdigest()
 r['document']=canonical.decode();print(json.dumps(r));sys.exit(0)
r.update(status='references-valid',validationScope='supplied-exact-bytes-anchors-and-scoped-core-references')
if scenario=='wrong-digest': r['bytesDigest']='sha256:'+'0'*64
if scenario=='wrong-scope': r['validationScope']='full-semantic-approval'
if scenario=='overflow': print('x'*2097153);sys.exit(0)
if scenario=='interrupted': time.sleep(2)
if scenario=='refused':
 r['status']='refused';r['diagnostics']=[dict(code='JPS-POLICY-ELEMENT-REFERENCE',codeStability='provisional',layer='policy',severity='error',instancePath='/governingPolicies/0/coverage/0',message='Controlled refusal')]
 print(json.dumps(r));sys.exit(1)
print(json.dumps(r));sys.exit(1 if scenario=='successful-output-failed-process' else 0)
`
			if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			s.cfg.JpackBin = bin
			ctx := context.Background()
			if scenario == "interrupted" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 150*time.Millisecond)
				defer cancel()
			}
			check := s.checkPolicyCoverage(ctx, material)
			if scenario == "refused" {
				var report policyCoverageRuntimeReport
				if check.Availability != "available" || json.Unmarshal(check.Report, &report) != nil || report.Status != "refused" {
					t.Fatal("useful refusal lost", check)
				}
			} else if check.Availability != "unavailable" {
				t.Fatal("broken Runtime accepted", scenario, check)
			}
			entries, err := os.ReadDir(scratch)
			if err != nil || len(entries) != 0 {
				t.Fatal("temporary snapshot not removed", entries, err)
			}
		})
	}
}
