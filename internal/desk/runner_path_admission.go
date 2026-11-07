package desk

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// Runner's JSON path fields must retain their exact spelling across encoding.
// Local filesystem journals use their own lossless codec instead.
var errRunnerPathEncoding = errors.New("Runner paths and workspace identity require valid UTF-8 without NUL.")

func validateRunnerPathFields(fields ...string) error {
	for _, field := range fields {
		if !utf8.ValidString(field) || strings.ContainsRune(field, 0) {
			return errRunnerPathEncoding
		}
	}
	return nil
}

func (j *jobsCompanion) validateRunnerPaths() error {
	if err := validateRunnerPathFields(j.bin, j.runtime, j.dir, j.workspace, j.owner, j.inputRoot); err != nil {
		return err
	}
	if j.ownership != nil {
		if err := validateRunnerPathFields(j.ownership.Root); err != nil {
			return err
		}
	}
	// Key preparation may create private state and invoke Runtime before start.
	if j.key != nil {
		return validateRunnerPathFields(j.key.s.configDir, j.key.name)
	}
	return nil
}

func validateHomeRunnerRelocation(binary string, request homeRunnerRelocation) error {
	return validateRunnerPathFields(binary, request.Dir, request.Workspace, request.Owner, request.PreviousInputRoot, request.InputRoot)
}
