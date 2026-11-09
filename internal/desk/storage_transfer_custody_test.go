package desk

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestStorageTransferCustodyOriginalLiveHandles(t *testing.T) {
	s, request := dialectArtifactOwnerSetup(t)
	before, err := s.openChatData()
	assistantRunMust(t, err)
	revision := before.revision
	assistantRunMust(t, before.root.Close())
	target := filepath.Join(t.TempDir(), "target")
	writer := httptest.NewRecorder()
	var escaped *chatStorageTransferCustody
	s.changeChatStorageUsingCustody(writer, request, moveChatData{Path: target, Revision: revision}, func(ctx context.Context, custody *chatStorageTransferCustody, copied *[]string) error {
		escaped = custody
		if err := custody.check(ctx); err != nil {
			return err
		}
		copy := *custody
		if copy.check(ctx) == nil || custody.capture() == nil {
			return errors.New("copied/recaptured custody replaced original handles")
		}
		for _, name := range []string{".data-work.lock", ".data.lock"} {
			probe, err := s.assistant.root.OpenFile(name, os.O_RDWR|openNoFollow|openNonBlocking, 0)
			if err != nil {
				return err
			}
			lockErr := lockPrivateData(probe, true)
			closeErr := probe.Close()
			if !lockHeld(lockErr) || closeErr != nil {
				return errors.New("custody did not retain actual original work/private exclusion")
			}
		}
		return copyChatData(ctx, custody.source, custody.target, copied)
	})
	if writer.Code != 200 {
		t.Fatal("owning extraction changed normal move", writer.Code, writer.Body.String())
	}
	if escaped == nil || escaped.check(t.Context()) == nil {
		t.Fatal("escaped custody survived the copy continuation")
	}
	after, err := s.openChatData()
	assistantRunMust(t, err)
	defer after.root.Close()
	if after.location.Path != target || after.revision == revision {
		t.Fatal("normal pointer transition was lost")
	}
	if _, err := os.Stat(filepath.Join(target, s.conversationName())); err != nil {
		t.Fatal("normal copied history was lost", err)
	}
}

func TestStorageTransferCustodySubstitutionAndCancelRollback(t *testing.T) {
	for _, kind := range []string{"target-wrapper", "target-path", "work-inode", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			s, request := dialectArtifactOwnerSetup(t)
			before, err := s.openChatData()
			assistantRunMust(t, err)
			revision, path := before.revision, before.location.Path
			original, err := readPrivateData(before.root, s.conversationName(), storageFileLimit(s.conversationName()))
			assistantRunMust(t, err)
			assistantRunMust(t, before.root.Close())
			target := filepath.Join(t.TempDir(), "target")
			ctx, cancel := context.WithCancel(request.Context())
			defer cancel()
			request = request.WithContext(ctx)
			writer := httptest.NewRecorder()
			var escaped *chatStorageTransferCustody
			s.changeChatStorageUsingCustody(writer, request, moveChatData{Path: target, Revision: revision}, func(ctx context.Context, custody *chatStorageTransferCustody, copied *[]string) error {
				escaped = custody
				if err := copyChatData(ctx, custody.source, custody.target, copied); err != nil {
					return err
				}
				switch kind {
				case "target-wrapper":
					custody.target.root = custody.source.root
				case "target-path":
					if err := os.Rename(target, target+"-original"); err != nil {
						return err
					}
					if err := os.Mkdir(target, 0700); err != nil {
						return err
					}
				case "work-inode":
					lock := filepath.Join(s.configDir, ".data-work.lock")
					if err := os.Rename(lock, lock+".original"); err != nil {
						return err
					}
					if err := os.WriteFile(lock, nil, 0600); err != nil {
						return err
					}
				case "cancel":
					cancel()
				}
				return nil
			})
			if writer.Code == 200 {
				t.Fatal("substituted/cancelled custody activated a destination")
			}
			if escaped == nil || escaped.check(t.Context()) == nil {
				t.Fatal("failed copy retained live custody")
			}
			after, err := s.openChatData()
			assistantRunMust(t, err)
			defer after.root.Close()
			if after.location.Path != path || after.revision != revision {
				t.Fatal("failed owning copy changed the original pointer")
			}
			mustBytes(t, filepath.Join(path, s.conversationName()), string(original))
			copiedTarget := target
			if kind == "target-path" {
				copiedTarget += "-original"
			}
			if _, err := os.Stat(filepath.Join(copiedTarget, s.conversationName())); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed owning copy kept copied records or cleaned a substituted root", err)
			}
		})
	}
}
