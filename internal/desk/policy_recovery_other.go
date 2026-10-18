//go:build !linux && !darwin

package desk

import "errors"

const policyRecoveryCanPublish = false

func publishPolicyRecoveryDirectory(*ProjectRoot, string, string) error {
	return errors.New("safe Policy restoration is not supported on this platform")
}
