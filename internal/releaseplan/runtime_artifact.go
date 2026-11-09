package releaseplan

// runtimeArtifactSHA256 is supplied only by the release builder after verified
// companion installation. It intentionally stays empty in source/custom builds:
// an installation path or descriptive build version cannot establish trust.
var runtimeArtifactSHA256 string

// RuntimeArtifactSHA256 returns the trusted bundled Runtime digest, if provided.
// A custom/development host must deliberately provision equivalent trust before
// enabling acting Runtime use; do not fill this from an unverified local file.
func RuntimeArtifactSHA256() string { return runtimeArtifactSHA256 }
