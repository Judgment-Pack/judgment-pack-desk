
## Platforms

This release contains complete Linux/amd64, macOS Apple Silicon (arm64), and
macOS Intel (amd64) archives. All three archives were built and smoke-tested on
native GitHub-hosted runners before publication; neither macOS archive is merely
cross-compiled. Component versions come from the same release lock.

macOS executables are not Developer ID signed or notarized. Gatekeeper may block
downloaded executables; verify the release and checksums, then use Apple's
[Privacy & Security → Open Anyway procedure](https://support.apple.com/en-us/102445)
for the blocked executable. Native CI does not exercise these dialogs. The Codex
subscription subprocess bridge remains Linux-only.

No Windows archive is published. Windows execution is untested, and the managed
installer supports only Linux and macOS.
