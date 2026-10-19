package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/desk"
)

func TestPolicyAuthorityCommandValidatesBeforeLocalCall(t *testing.T) {
	root := filepath.Join(t.TempDir(), "authority")
	for _, args := range [][]string{
		nil,
		{"repair", "--root", root},
		{"init"},
		{"init", "--root", "relative"},
		{"init", "--root", root + string(filepath.Separator)},
		{"init", "--root", root, "--installation", ""},
		{"init", "--root", root, "project"},
		{"status", "--root", root},
		{"status", "--root", root, "--installation", ""},
		{"status", "--root", root, "--installation", "expected", "first", "second"},
		{"status", "--root", root, "--installation", "expected", "project", "--unknown"},
		{"status", "--root", root, "--installation", "expected", "--unknown"},
		{"status", "--root", root, "--installation", "expected", "--", "--unknown"},
		{"status", "--root", root, "--installation", "expected", ""},
		{"enroll", "--root", root, "--installation", "expected"},
		{"enroll-executor", "--root", root, "--installation", "expected"},
		{"classify-legacy", "--root", root, "--installation", "expected"},
		{"--help", "extra"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			called := false
			err := runPolicyAuthorityCommand(context.Background(), args, io.Discard, func(context.Context, string, desk.PolicyAuthorityOptions, string) (desk.PolicyAuthorityStatus, error) {
				called = true
				return desk.PolicyAuthorityStatus{}, nil
			})
			if err == nil || called {
				t.Fatalf("err=%v, local backend called=%v", err, called)
			}
		})
	}
}

func TestPolicyAuthorityCommandDispatchAndBoundedOutput(t *testing.T) {
	root := filepath.Join(t.TempDir(), "authority")
	for _, row := range []struct {
		action, project string
	}{
		{"init", ""}, {"status", ""}, {"status", "project"},
		{"classify-legacy", "project"}, {"enroll", "project"}, {"enroll-executor", "project"},
	} {
		t.Run(row.action+"/"+row.project, func(t *testing.T) {
			args := []string{row.action, "--root", root}
			installation := ""
			if row.action != "init" {
				installation = "expected"
				args = append(args, "--installation", installation)
			}
			if row.project != "" {
				args = append(args, row.project)
			}
			ctx := context.Background()
			var output bytes.Buffer
			calls := 0
			want := desk.PolicyAuthorityStatus{Version: 1, InstallationID: "generated-or-expected", State: "ready"}
			err := runPolicyAuthorityCommand(ctx, args, &output, func(gotCtx context.Context, action string, options desk.PolicyAuthorityOptions, project string) (desk.PolicyAuthorityStatus, error) {
				calls++
				if gotCtx != ctx || action != row.action || options.Root != root || options.InstallationID != installation || project != row.project {
					t.Fatalf("unexpected call: action=%q options=%+v project=%q", action, options, project)
				}
				return want, nil
			})
			var got desk.PolicyAuthorityStatus
			if err != nil || calls != 1 || json.Unmarshal(output.Bytes(), &got) != nil || got != want {
				t.Fatalf("calls=%d output=%q err=%v", calls, output.String(), err)
			}
			if strings.Contains(output.String(), root) || strings.Contains(output.String(), "projectDir") {
				t.Fatalf("output includes local path: %q", output.String())
			}
		})
	}
}

func TestPolicyAuthorityCommandHelpAndUncertainFailure(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"enroll", "--help"}, {"init", "-h"}} {
		var output bytes.Buffer
		err := runPolicyAuthorityCommand(context.Background(), args, &output, func(context.Context, string, desk.PolicyAuthorityOptions, string) (desk.PolicyAuthorityStatus, error) {
			t.Fatal("help invoked backend")
			return desk.PolicyAuthorityStatus{}, nil
		})
		if err != nil || !strings.Contains(output.String(), "usage:") {
			t.Fatalf("help output=%q err=%v", output.String(), err)
		}
	}
	wantErr := errors.New("uncertain enrollment")
	calls := 0
	var output bytes.Buffer
	err := runPolicyAuthorityCommand(context.Background(), []string{"enroll", "--root", t.TempDir(), "--installation", "expected", "project"}, &output, func(context.Context, string, desk.PolicyAuthorityOptions, string) (desk.PolicyAuthorityStatus, error) {
		calls++
		return desk.PolicyAuthorityStatus{}, wantErr
	})
	if !errors.Is(err, wantErr) || calls != 1 || output.Len() != 0 {
		t.Fatalf("calls=%d output=%q err=%v", calls, output.String(), err)
	}
}

func TestServePolicyAuthorityOptions(t *testing.T) {
	root := t.TempDir()
	for _, row := range []struct {
		args                      []string
		wantConfigured, wantError bool
	}{
		{nil, false, false},
		{[]string{"--policy-authority-root", root}, false, true},
		{[]string{"--policy-authority-installation", "expected"}, false, true},
		{[]string{"--policy-authority-root", "", "--policy-authority-installation", ""}, false, true},
		{[]string{"--policy-authority-root", "relative", "--policy-authority-installation", "expected"}, false, true},
		{[]string{"--policy-authority-root", root, "--policy-authority-installation", "expected"}, true, false},
	} {
		flags := flag.NewFlagSet("test", flag.ContinueOnError)
		rawRoot := flags.String("policy-authority-root", "", "")
		installation := flags.String("policy-authority-installation", "", "")
		if err := flags.Parse(row.args); err != nil {
			t.Fatal(err)
		}
		got, err := servePolicyAuthorityOptions(flags, *rawRoot, *installation)
		if (err != nil) != row.wantError || (got != nil) != row.wantConfigured {
			t.Fatalf("args=%q options=%+v err=%v", row.args, got, err)
		}
		if got != nil && (got.Root != root || got.InstallationID != "expected") {
			t.Fatalf("unexpected options: %+v", got)
		}
	}
}

func TestPolicyAuthorityStartupDoesNotRecoverConfiguredHome(t *testing.T) {
	for _, configured := range []bool{false, true} {
		for _, refused := range []bool{false, true} {
			var options *desk.PolicyAuthorityOptions
			if configured {
				options = &desk.PolicyAuthorityOptions{Root: t.TempDir(), InstallationID: "expected"}
			}
			checked, recovered := false, false
			blocked := errors.New("pending migration")
			err := prepareDeskHomeForPolicyAuthority(options, func(got *desk.PolicyAuthorityOptions) error {
				checked = true
				if got != options || recovered {
					t.Fatal("startup checked wrong options or recovered before checking")
				}
				if refused {
					return blocked
				}
				return nil
			}, func() error { recovered = true; return nil })
			if !checked || recovered != (!configured && !refused) || errors.Is(err, blocked) != refused {
				t.Fatalf("configured=%v refused=%v checked=%v recovered=%v err=%v", configured, refused, checked, recovered, err)
			}
		}
	}
}

func TestRunRejectsInvalidArgumentsWithoutGlobalFlagRegistration(t *testing.T) {
	previous := os.Args
	t.Cleanup(func() { os.Args = previous })
	for attempt := 0; attempt < 2; attempt++ {
		for _, args := range [][]string{
			{"policy-authority", "unknown"},
			{"policy-authority", "enroll"},
			{"--policy-authority-root", t.TempDir()},
			{"--policy-authority-installation", "expected"},
			{"project", "--unknown"},
			{"first", "second"},
		} {
			os.Args = append([]string{"jpack-desk"}, args...)
			if err := run(); err == nil {
				t.Fatalf("accepted invalid arguments: %q", args)
			}
		}
	}
}
