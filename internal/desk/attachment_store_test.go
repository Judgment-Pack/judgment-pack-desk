package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func attachmentProofBody(t *testing.T, original string) []byte {
	t.Helper()
	var object attachmentObject
	if err := json.Unmarshal([]byte(original), &object); err != nil {
		t.Fatal(err)
	}
	object.Proof = &attachmentProof{Session: "fixture", Source: "documents", Authority: "gateway:test", PublicKey: strings.Repeat("ab", 32), Response: `{"result":{},"receipt":{}}`, Registry: "fixture registry"}
	data, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Both entrypoints must enforce the same byte-level CAS and immutable custody.
func TestAttachmentStoreSharedCAS(t *testing.T) {
	for _, entrypoint := range []string{"typed", "http"} {
		t.Run(entrypoint, func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			enableTestDocuments(t, s)
			write := func(match string, data []byte) (attachmentReceipt, string, string) {
				t.Helper()
				if entrypoint == "typed" {
					receipt, err := s.attachmentStorage().write(t.Context(), attachmentWriteRequest{id: testAttachmentID, ifMatch: match, data: data})
					if err != nil {
						if receipt != (attachmentReceipt{}) {
							t.Fatal("refusal returned a receipt", receipt)
						}
						return receipt, codeOf(err), err.Error()
					}
					return receipt, "", ""
				}
				status, _, reply := documentRequest(t, ts, "PUT", testAttachmentID, match, string(data))
				if status != http.StatusOK {
					var failure struct{ Code, Error string }
					if err := json.Unmarshal(reply, &failure); err != nil || status != statusForRefusal(withCode(failure.Code, errors.New(failure.Error))) {
						t.Fatalf("bad refusal: %d %s %v", status, reply, err)
					}
					return attachmentReceipt{}, failure.Code, failure.Error
				}
				var receipt attachmentReceipt
				if err := json.Unmarshal(reply, &receipt); err != nil {
					t.Fatal(err)
				}
				return receipt, "", ""
			}
			original := []byte(documentBody(t, "retained bytes") + "\n")
			proof := attachmentProofBody(t, string(original))
			if _, code, message := write("absent", proof); code != CodeBadRequest || message != "retain the original before adding an acquisition packet" {
				t.Fatalf("proof before original: %s %s", code, message)
			}
			receipt, code, message := write("absent", original)
			if code != "" || receipt.ID != testAttachmentID || receipt.SHA256 != digestOf(original) {
				t.Fatalf("exact receipt: %+v %s %s", receipt, code, message)
			}
			for _, match := range []string{"", "absent", digestOf(bytes.TrimSpace(original))} {
				if _, code, message := write(match, original); code != CodeStale || message != "attachment changed; reload before saving" {
					t.Fatalf("CAS %q: %s %s", match, code, message)
				}
			}
			if _, code, _ := write(receipt.SHA256, original); code != "" {
				t.Fatal("unchanged original refused", code)
			}
			if _, code, message := write(receipt.SHA256, []byte(documentBody(t, "changed original"))); code != CodeStale || message != "an original or completed acquisition cannot be replaced" {
				t.Fatalf("original replacement: %s %s", code, message)
			}
			receipt, code, message = write(receipt.SHA256, proof)
			if code != "" || receipt.SHA256 != digestOf(proof) {
				t.Fatalf("proof append: %+v %s %s", receipt, code, message)
			}
			if _, code, message := write(receipt.SHA256, proof); code != CodeStale || message != "an original or completed acquisition cannot be replaced" {
				t.Fatalf("completed proof replacement: %s %s", code, message)
			}
			// Retrieval must remain available even after Gateway configuration is removed.
			writeDeskConfig(t, s, `{"deskConfigVersion":1}`)
			status, _, read := documentRequest(t, ts, "GET", testAttachmentID, "", "")
			if status != http.StatusOK || !bytes.Equal(read, proof) {
				t.Fatalf("exact retained bytes: %d %s", status, read)
			}
		})
	}
}

func TestAttachmentStoreHTTPHeadReadsWithoutWriting(t *testing.T) {
	s, ts, _ := assistantServer(t)
	enableTestDocuments(t, s)
	body := documentBody(t, "exact retained HEAD fixture") + "\n"
	status, digest, _ := documentRequest(t, ts, http.MethodPut, testAttachmentID, "absent", body)
	if status != http.StatusOK || digest != digestOf([]byte(body)) {
		t.Fatalf("retain HEAD fixture: %d %s", status, digest)
	}
	store, err := s.openChatData()
	if err != nil {
		t.Fatal(err)
	}
	defer store.root.Close()
	snapshot := func(t *testing.T) map[string][]byte {
		t.Helper()
		entries, err := storageEntries(store.root)
		if err != nil {
			t.Fatal(err)
		}
		files := make(map[string][]byte, len(entries))
		for _, entry := range entries {
			data, err := store.root.ReadFile(entry.Name())
			if err != nil {
				t.Fatal(err)
			}
			files[entry.Name()] = data
		}
		return files
	}
	before := snapshot(t)
	for _, fixture := range []struct {
		name, id string
		status   int
	}{
		{"existing", testAttachmentID, http.StatusOK},
		{"absent", "22345678-1234-1234-1234-123456789abc", http.StatusNotFound},
	} {
		for _, match := range []string{"absent", digest, digestOf([]byte("stale fixture"))} {
			t.Run(fixture.name+"/"+match, func(t *testing.T) {
				req, err := http.NewRequest(http.MethodHead, ts.URL+"/api/attachments/"+fixture.id, nil)
				if err != nil {
					t.Fatal(err)
				}
				bearer(req)
				req.Header.Set("If-Match", match)
				req.Header.Set("If-None-Match", digest)
				res, err := ts.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(res.Body)
				res.Body.Close()
				if err != nil || len(data) != 0 || res.StatusCode != fixture.status {
					t.Fatalf("HEAD response: status=%d bytes=%d error=%v", res.StatusCode, len(data), err)
				}
				if res.Header.Get("Content-Type") != "application/json" || res.Header.Get("Cache-Control") != "no-store" {
					t.Fatalf("HEAD metadata: %v", res.Header)
				}
				expectedETag := ""
				if fixture.status == http.StatusOK {
					expectedETag = digest
					if res.ContentLength != int64(len(body)) {
						t.Fatalf("HEAD retained length: %d", res.ContentLength)
					}
				}
				if res.Header.Get("ETag") != expectedETag {
					t.Fatalf("HEAD retained ETag: %q", res.Header.Get("ETag"))
				}
				after := snapshot(t)
				if len(after) != len(before) {
					t.Fatalf("HEAD changed storage file count: %d -> %d", len(before), len(after))
				}
				for name, exact := range before {
					if retained, exists := after[name]; !exists || !bytes.Equal(retained, exact) {
						t.Fatalf("HEAD changed retained storage file %q", name)
					}
				}
			})
		}
	}
}

func TestAttachmentStoreValidationAndConfiguration(t *testing.T) {
	s, _, _ := assistantServer(t)
	write := func(id, match string, data []byte, code string) {
		t.Helper()
		receipt, err := s.attachmentStorage().write(t.Context(), attachmentWriteRequest{id: id, ifMatch: match, data: data})
		if err == nil || codeOf(err) != code || receipt != (attachmentReceipt{}) {
			t.Fatalf("expected %s, got %+v %v", code, receipt, err)
		}
	}
	body := []byte(documentBody(t, "original"))
	write("../other", "absent", body, CodeBadRequest)
	write(testAttachmentID, "absent", []byte(`{"version":1}`), CodeBadRequest)
	write(testAttachmentID, "absent", body, CodeResearchUnconfigured)
	enableTestDocuments(t, s)
	_, config, err := s.readDeskFile()
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(config, &settings); err != nil {
		t.Fatal(err)
	}
	documents := settings["research"].(map[string]any)["documents"].(map[string]any)
	documents["maxFileBytes"] = 1
	config, _ = json.Marshal(settings)
	writeDeskConfig(t, s, string(config))
	write(testAttachmentID, "absent", body, CodeTooLarge)
	documents["enabled"] = false
	config, _ = json.Marshal(settings)
	writeDeskConfig(t, s, string(config))
	write(testAttachmentID, "absent", body, CodeResearchUnconfigured)
	// An exact digest cannot create an object in place of an absent CAS.
	write(testAttachmentID, digestOf(body), body, CodeStale)
}

func TestAttachmentStoreNamespaceAndCustody(t *testing.T) {
	shared := t.TempDir()
	a, ats, _ := assistantServerIn(t, shared)
	b, _, _ := assistantServerIn(t, shared)
	enableTestDocuments(t, a)
	body := []byte(documentBody(t, "private original"))
	receipt, err := a.attachmentStorage().write(t.Context(), attachmentWriteRequest{id: testAttachmentID, ifMatch: "absent", data: body})
	if err != nil {
		t.Fatal(err)
	}
	proof := attachmentProofBody(t, string(body))
	if _, err := b.attachmentStorage().write(t.Context(), attachmentWriteRequest{id: testAttachmentID, ifMatch: receipt.SHA256, data: proof}); codeOf(err) != CodeStale {
		t.Fatal("another project's digest accepted", err)
	}
	store, err := a.openChatData()
	if err != nil {
		t.Fatal(err)
	}
	defer store.root.Close()
	marker, err := json.Marshal(dataMarker{Version: 1, Owner: "another-owner"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writePrivateData(store.root, dataMarkerName, marker); err != nil {
		t.Fatal(err)
	}
	if _, err := a.attachmentStorage().write(t.Context(), attachmentWriteRequest{id: testAttachmentID, ifMatch: receipt.SHA256, data: proof}); err == nil {
		t.Fatal("changed store identity accepted")
	}
	status, _, _ := documentRequest(t, ats, "GET", testAttachmentID, "", "")
	if status == http.StatusOK {
		t.Fatal("HTTP bypassed store ownership")
	}
}

func TestAttachmentStoreQuota(t *testing.T) {
	for _, quota := range []string{"bytes", "files"} {
		t.Run(quota, func(t *testing.T) {
			s, _, _ := assistantServer(t)
			enableTestDocuments(t, s)
			store, err := s.openChatData()
			if err != nil {
				t.Fatal(err)
			}
			defer store.root.Close()
			if quota == "bytes" {
				// Sparse fixture exercises accounting without allocating the quota.
				file, err := store.root.OpenFile(fmt.Sprintf("conversations-%064x.json", 1), os.O_CREATE|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				err = file.Truncate(maxMoveBytes)
				file.Close()
				if err != nil {
					t.Fatal(err)
				}
			} else {
				entries, err := storageEntries(store.root)
				if err != nil {
					t.Fatal(err)
				}
				for i := len(entries); i < maxStorageFiles-4; i++ {
					file, err := store.root.OpenFile(fmt.Sprintf("conversations-%064x.json", i), os.O_CREATE|os.O_WRONLY, 0600)
					if err != nil {
						t.Fatal(err)
					}
					file.Close()
				}
			}
			receipt, err := s.attachmentStorage().write(t.Context(), attachmentWriteRequest{id: testAttachmentID, ifMatch: "absent", data: []byte(documentBody(t, "quota"))})
			if err == nil || codeOf(err) != CodeTooLarge || receipt != (attachmentReceipt{}) {
				t.Fatalf("%s quota: %+v %v", quota, receipt, err)
			}
			entries, err := storageEntries(store.root)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if attachmentFileName.MatchString(entry.Name()) {
					t.Fatal("quota refusal retained an attachment")
				}
			}
		})
	}
}

func TestAttachmentStoreCurrentCheckAndOwnedBytes(t *testing.T) {
	for _, refuseAt := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("check-%d", refuseAt), func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			enableTestDocuments(t, s)
			body := []byte(documentBody(t, "original"))
			exact := bytes.Clone(body)
			calls := 0
			receipt, err := s.attachmentStorage().write(t.Context(), attachmentWriteRequest{id: testAttachmentID, ifMatch: "absent", data: body, checkCurrent: func() error {
				calls++
				// A caller changing its input after validation must not change the
				// validated storage bytes, even while admission is rechecked.
				body[0] = '!'
				if calls == refuseAt {
					return withCode(CodeStale, errors.New("fixture authority changed"))
				}
				return nil
			}})
			if refuseAt != 0 {
				if err == nil || codeOf(err) != CodeStale || calls != refuseAt || receipt != (attachmentReceipt{}) {
					t.Fatalf("authority refusal: %d %+v %v", calls, receipt, err)
				}
			} else if err != nil || calls != 2 || receipt.SHA256 != digestOf(exact) {
				t.Fatalf("owned bytes: %d %+v %v", calls, receipt, err)
			}
			status, _, retained := documentRequest(t, ts, "GET", testAttachmentID, "", "")
			if refuseAt != 0 && status != http.StatusNotFound || refuseAt == 0 && (status != http.StatusOK || !bytes.Equal(retained, exact)) {
				t.Fatalf("retained after authority check: %d %s", status, retained)
			}
		})
	}
}

func TestAttachmentStoreAdmissionBeforeStorageAccess(t *testing.T) {
	s, _, _ := assistantServer(t)
	enableTestDocuments(t, s)
	stale := withCode(CodeStale, errors.New("fixture authority changed while queued"))
	reads := 0
	testHookPrivateRead = func(name string) error {
		if name == dataLocationName {
			reads++
			return errors.New("fixture storage pointer must not be inspected")
		}
		return nil
	}
	t.Cleanup(func() { testHookPrivateRead = nil })
	receipt, err := s.attachmentStorage().write(t.Context(), attachmentWriteRequest{
		id: testAttachmentID, ifMatch: "absent", data: []byte(documentBody(t, "original")),
		checkCurrent: func() error { return stale },
	})
	if !errors.Is(err, stale) || receipt != (attachmentReceipt{}) || reads != 0 {
		t.Fatalf("stale admission reached private storage: %+v %v reads=%d", receipt, err, reads)
	}
}

func TestAttachmentStoreCancellation(t *testing.T) {
	for _, cancelAt := range []string{"queued", "before-write"} {
		t.Run(cancelAt, func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			enableTestDocuments(t, s)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			request := attachmentWriteRequest{id: testAttachmentID, ifMatch: "absent", data: []byte(documentBody(t, "original"))}
			if cancelAt == "before-write" {
				calls := 0
				request.checkCurrent = func() error {
					calls++
					if calls == 2 {
						cancel()
					}
					return nil
				}
			} else {
				s.writes.Lock()
				cancel()
			}
			result := make(chan error, 1)
			go func() {
				receipt, err := s.attachmentStorage().write(ctx, request)
				if receipt != (attachmentReceipt{}) {
					result <- errors.New("cancelled write returned a receipt")
					return
				}
				result <- err
			}()
			if cancelAt == "queued" {
				s.writes.Unlock()
			}
			if err := <-result; !errors.Is(err, context.Canceled) {
				t.Fatal("cancelled storage write", err)
			}
			status, _, _ := documentRequest(t, ts, "GET", testAttachmentID, "", "")
			if status != http.StatusNotFound {
				t.Fatal("cancelled write retained attachment", status)
			}
		})
	}
}

func TestAttachmentStoreUncertainReadback(t *testing.T) {
	s, ts, _ := assistantServer(t)
	enableTestDocuments(t, s)
	body := []byte(documentBody(t, "original"))
	reads := 0
	testHookPrivateRead = func(name string) error {
		if attachmentFileName.MatchString(name) {
			reads++
			return errors.New("fixture readback unavailable")
		}
		return nil
	}
	t.Cleanup(func() { testHookPrivateRead = nil })
	receipt, err := s.attachmentStorage().write(t.Context(), attachmentWriteRequest{id: testAttachmentID, ifMatch: "absent", data: body})
	testHookPrivateRead = nil
	if err == nil || receipt != (attachmentReceipt{}) || reads != 1 {
		t.Fatalf("uncertain readback returned success or retried: %+v %v reads=%d", receipt, err, reads)
	}
	status, _, retained := documentRequest(t, ts, "GET", testAttachmentID, "", "")
	if status != http.StatusOK || !bytes.Equal(retained, body) {
		t.Fatalf("uncertain write lost reconciliation bytes: %d %s", status, retained)
	}
	if _, err := s.attachmentStorage().write(t.Context(), attachmentWriteRequest{id: testAttachmentID, ifMatch: "absent", data: body}); codeOf(err) != CodeStale {
		t.Fatal("uncertain write could be replayed as absent", err)
	}
}
