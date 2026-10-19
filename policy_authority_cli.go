package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/desk"
)

type policyAuthorityLocalCall func(context.Context, string, desk.PolicyAuthorityOptions, string) (desk.PolicyAuthorityStatus, error)

// Local authority operations finish before the normal Desk startup path. The
// injected call lets argument rejection and help be checked without any storage,
// Runtime resolution, adapters or listeners.
func runPolicyAuthorityCommand(ctx context.Context, args []string, output io.Writer, local policyAuthorityLocalCall) error {
	usage := "usage: jpack-desk policy-authority <init|status|classify-legacy|enroll|enroll-executor|enable-execution> --root <absolute> [--installation <id>] [--runner <absolute>] [projectDir]\n"
	if len(args) == 0 {
		return errors.New(strings.TrimSpace(usage))
	}
	if args[0] == "--help" || args[0] == "-h" {
		if len(args) != 1 {
			return errors.New("policy-authority help accepts no additional arguments")
		}
		_, err := io.WriteString(output, usage)
		return err
	}
	action := args[0]
	switch action {
	case "init", "status", "classify-legacy", "enroll", "enroll-executor", "enable-execution":
	default:
		return fmt.Errorf("unknown policy-authority action %q", action)
	}
	flags := flag.NewFlagSet("policy-authority "+action, flag.ContinueOnError)
	flags.SetOutput(output)
	root := flags.String("root", "", "absolute operator-chosen machine Policy authority root")
	installation := flags.String("installation", "", "expected installation identity (required except for init)")
	runner := flags.String("runner", "", "explicit absolute Runner helper binary (enable-execution only)")
	flags.Usage = func() {
		fmt.Fprint(output, usage)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if err := validateDeskPositionals(flags.Args()); err != nil {
		return err
	}
	if !filepath.IsAbs(*root) || filepath.Clean(*root) != *root {
		return errors.New("policy-authority --root must be an explicit, clean absolute path")
	}
	if action == "enable-execution" {
		if !filepath.IsAbs(*runner) || filepath.Clean(*runner) != *runner {
			return errors.New("enable-execution requires --runner with a clean absolute Runner binary path")
		}
	} else if flagWasSet(flags, "runner") {
		return errors.New("--runner is supported only for enable-execution")
	}
	project := flags.Arg(0)
	if flags.NArg() == 1 && project == "" {
		return errors.New("policy-authority projectDir must not be empty")
	}
	if action == "init" {
		if flagWasSet(flags, "installation") || flags.NArg() != 0 {
			return errors.New("policy-authority init accepts only --root; it generates the installation identity")
		}
	} else if strings.TrimSpace(*installation) == "" {
		return errors.New("policy-authority requires --installation with the expected installation identity")
	}
	if (action == "enroll" || action == "enroll-executor" || action == "classify-legacy" || action == "enable-execution") && flags.NArg() != 1 {
		return fmt.Errorf("policy-authority %s requires an explicit projectDir", action)
	}
	status, err := local(ctx, action, desk.PolicyAuthorityOptions{Root: *root, InstallationID: *installation, RunnerBin: *runner}, project)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(status)
}

func flagWasSet(flags *flag.FlagSet, name string) bool {
	found := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func validateDeskPositionals(args []string) error {
	if len(args) > 1 {
		return errors.New("expected at most one projectDir; flags must precede projectDir")
	}
	if len(args) == 1 && strings.HasPrefix(args[0], "-") {
		return errors.New("unexpected flag after projectDir or --; flags must precede projectDir")
	}
	return nil
}

func servePolicyAuthorityOptions(flags *flag.FlagSet, root, installation string) (*desk.PolicyAuthorityOptions, error) {
	if !flagWasSet(flags, "policy-authority-root") && !flagWasSet(flags, "policy-authority-installation") {
		return nil, nil
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || strings.TrimSpace(installation) == "" {
		return nil, errors.New("--policy-authority-root (clean absolute path) and --policy-authority-installation (expected identity) must be supplied together")
	}
	return &desk.PolicyAuthorityOptions{Root: root, InstallationID: installation}, nil
}

// Configured custody checks pending migration without repairing it. Legacy
// launches retain their existing automatic Desk-home migration recovery.
func prepareDeskHomeForPolicyAuthority(options *desk.PolicyAuthorityOptions, check func(*desk.PolicyAuthorityOptions) error, recoverHome func() error) error {
	if err := check(options); err != nil {
		return err
	}
	if options == nil {
		return recoverHome()
	}
	return nil
}
