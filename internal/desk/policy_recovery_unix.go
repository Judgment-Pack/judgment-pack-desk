//go:build linux || darwin

package desk

const policyRecoveryCanPublish = true

func publishPolicyRecoveryDirectory(parent *ProjectRoot, stage, destination string) error {
	return renameDirectoryNoReplace(parent.own.dirFile, stage, parent.own.dirFile, destination)
}
