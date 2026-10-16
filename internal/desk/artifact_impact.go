package desk

import (
	"context"
	"encoding/json"
	"errors"
)

// Runtime owns grammar, comparison and arbitrary-size arithmetic. Desk checks
// the human classification against its exact choices; tests never classify it.
func (s *Server) checkArtifactImpact(ctx context.Context, dir heldDir, base, version string, impact artifactImpact) error {
	if !impact.valid() || impact.Classification == "unknown" {
		return errors.New("Review the compatibility impact and explain the version before publishing.")
	}
	if base == "" {
		if impact.Classification != "initial" {
			return errors.New("Review this artifact as a first publication.")
		}
		return nil
	}
	if impact.Classification == "initial" {
		return errors.New("This artifact already has a published version. Review the compatibility of this change.")
	}
	answer, err := s.artifactVersionComparison(ctx, dir, base, version)
	if err != nil {
		return err
	}

	if answer.Choices[impact.Classification] != version {
		return errors.New("The proposed version does not match the reviewed compatibility impact. Choose the corresponding next version.")
	}
	return nil
}

type artifactVersionReply struct {
	OutputVersion, Command, Status, Scope, Base, Candidate, Comparison string
	CompatibilityChecked                                               bool
	Choices                                                            map[string]string
}

func (s *Server) artifactVersionComparison(ctx context.Context, dir heldDir, base, version string) (artifactVersionReply, error) {
	raw, err := runRuntime(ctx, s.cfg.JpackBin, dir, "artifact", "version", "--base", base, "--candidate", version, "--format", "json")
	var answer artifactVersionReply
	if err != nil || json.Unmarshal(raw, &answer) != nil || answer.OutputVersion != "2" || answer.Command != "artifact version" || answer.Status != "available" || answer.Scope != "version-arithmetic" || answer.Base != base || answer.Candidate != version || answer.CompatibilityChecked || !contains([]string{"lower", "equal", "greater"}, answer.Comparison) {
		return answer, errors.New("Runtime could not check the reviewed version change.")
	}
	return answer, nil
}
