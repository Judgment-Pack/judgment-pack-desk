package desk

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func policySequenceFixture(t *testing.T) (policyService, policyReply, policyStoredRecord, policyCommand) {
	t.Helper()
	s, token := policyTestServer(t)
	r := httptest.NewRequest("POST", "/api/policies", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	held, _ := s.sessionOf(r)
	actor := policyActorOf(held)
	service := s.localPolicyService(r, actor)
	service.clock = func() time.Time { return time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC) }
	run := func(command policyCommand) (policyReply, policyCommand) {
		command.Actor = actor
		switch {
		case command.Edit != nil:
			command.OperationID = command.Edit.OperationID
		case command.Review != nil:
			command.OperationID = command.Review.OperationID
		default:
			command.OperationID = command.Publish.OperationID
		}
		raw, _ := json.Marshal(command)
		command.RequestDigest = digestOf(raw)
		result, err := service.execute(t.Context(), command)
		if err != nil {
			t.Fatal(err)
		}
		return result.Policy, command
	}
	current, _ := run(policyCommand{Kind: "create", Edit: &policyEditRequest{OperationID: policyOp(t), Title: "Policy", Narrative: "Retained narrative", ArtifactVersion: "1.0.0"}})
	review := policyReviewBody(t, current, "approve")
	current, _ = run(policyCommand{Kind: "review", PolicyID: current.Document.ID, Review: &review})
	publication := policyPublishBody(t, current)
	current, firstPublication := run(policyCommand{Kind: "publish", PolicyID: current.Document.ID, Publish: &publication})
	edit := policyEditBody(t, current)
	edit.Narrative = "Later narrative"
	edit.ArtifactVersion = "1.1.0"
	current, _ = run(policyCommand{Kind: "save-draft", PolicyID: current.Document.ID, Edit: &edit})
	review = policyReviewBody(t, current, "request-changes")
	current, _ = run(policyCommand{Kind: "review", PolicyID: current.Document.ID, Review: &review})
	review = policyReviewBody(t, current, "approve")
	current, _ = run(policyCommand{Kind: "review", PolicyID: current.Document.ID, Review: &review})
	publication = policyPublishBody(t, current)
	current, _ = run(policyCommand{Kind: "publish", PolicyID: current.Document.ID, Publish: &publication})
	beforeNoop := current.Revision
	edit = policyEditBody(t, current)
	current, _ = run(policyCommand{Kind: "save-draft", PolicyID: current.Document.ID, Edit: &edit})
	if current.Revision != beforeNoop {
		t.Fatal("no-op save allocated a history event")
	}
	var record policyStoredRecord
	if err := service.repository.View(t.Context(), func(ledger policyLedger) error { record = ledger.Policies[current.Document.ID]; return nil }); err != nil {
		t.Fatal(err)
	}
	return service, current, record, firstPublication
}

func TestPolicyEventSequenceRecordsCommitOrderWithTiedTimes(t *testing.T) {
	service, current, record, firstPublication := policySequenceFixture(t)
	if policyEventCount(record) != 7 {
		t.Fatal("unexpected event count", record)
	}
	if record.Revisions[0].Sequence != 1 || record.Reviews[0].Sequence != 2 || record.Publications[0].Sequence != 3 || record.Revisions[1].Sequence != 4 || record.Reviews[1].Sequence != 5 || record.Reviews[2].Sequence != 6 || record.Publications[1].Sequence != 7 {
		t.Fatal("sequence does not follow actual commits", record)
	}
	for _, revision := range record.Revisions {
		if revision.At != "2026-10-17T12:00:00Z" {
			t.Fatal("fixture timestamps must tie")
		}
	}
	for _, review := range record.Reviews {
		if review.At != "2026-10-17T12:00:00Z" {
			t.Fatal("fixture timestamps must tie")
		}
	}
	for _, publication := range record.Publications {
		if publication.At != "2026-10-17T12:00:00Z" {
			t.Fatal("fixture timestamps must tie")
		}
	}
	reloaded, err := service.get(t.Context(), current.Document.ID)
	if err != nil || reloaded.Revision != current.Revision || reloaded.Document.Publications[1].Sequence != 7 {
		t.Fatal("reload lost persisted sequence", err, reloaded)
	}
	recovered, err := service.execute(t.Context(), firstPublication)
	if err != nil || recovered.Delivery != "recovered" || recovered.Policy.Document.Publications[0].Sequence != 3 || len(recovered.Policy.Document.Revisions) != 1 {
		t.Fatal("recovery substituted later event order", err, recovered)
	}
	after, err := service.get(t.Context(), current.Document.ID)
	if err != nil || after.Revision != current.Revision || len(after.Document.Publications) != 2 {
		t.Fatal("recovery allocated a sequence", err, after)
	}
}

func TestPolicyEventSequenceIntegrityRefusesGapsDuplicatesAndInvalidReferences(t *testing.T) {
	_, _, original, _ := policySequenceFixture(t)
	cases := []struct {
		name   string
		change func(*policyStoredRecord)
	}{
		{"missing sequence", func(record *policyStoredRecord) { record.Revisions[0].Sequence = 0 }},
		{"duplicate across kinds", func(record *policyStoredRecord) { record.Reviews[0].Sequence = record.Revisions[0].Sequence }},
		{"gap", func(record *policyStoredRecord) { record.Publications[1].Sequence = 8 }},
		{"revision array out of order", func(record *policyStoredRecord) {
			record.Revisions[0], record.Revisions[1] = record.Revisions[1], record.Revisions[0]
			record.CreatedAt = record.Revisions[0].At
		}},
		{"review array out of order", func(record *policyStoredRecord) {
			record.Reviews[1], record.Reviews[2] = record.Reviews[2], record.Reviews[1]
		}},
		{"review references future candidate", func(record *policyStoredRecord) {
			revision := record.Revisions[1]
			review := &record.Reviews[0]
			review.CandidateRevisionID, review.CandidateDigest, review.WorkflowDigest = revision.ID, revision.Digest, revision.WorkflowDigest
		}},
		{"publication references future approval", func(record *policyStoredRecord) {
			publication := &record.Publications[0]
			revision, review := record.Revisions[1], record.Reviews[2]
			publication.CandidateRevisionID, publication.CandidateDigest, publication.WorkflowDigest, publication.Content, publication.ArtifactVersion, publication.ReviewID = revision.ID, revision.Digest, revision.WorkflowDigest, revision.Content, revision.ArtifactVersion, review.ID
			record.Publications = record.Publications[:1]
		}},
		{"wrong current revision", func(record *policyStoredRecord) {
			record.HeadRevisionID = record.Revisions[0].ID
			record.CurrentReviewID = record.Reviews[0].ID
		}},
		{"wrong current review", func(record *policyStoredRecord) { record.CurrentReviewID = record.Reviews[1].ID }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			raw, _ := json.Marshal(original)
			var record policyStoredRecord
			if err := json.Unmarshal(raw, &record); err != nil {
				t.Fatal(err)
			}
			test.change(&record)
			ledger := emptyPolicyLedger()
			ledger.Policies[record.ID] = record
			if validatePolicyLedger(ledger) == nil {
				t.Fatal("accepted inconsistent event history")
			}
		})
	}
}
