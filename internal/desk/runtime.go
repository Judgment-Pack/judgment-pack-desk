package desk

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ResolveRuntime turns the runtime named on the command line into an absolute
// path this process can execute, or says why it cannot — at launch, in one
// sentence, rather than on every relay connection.
//
// **The chassis used to accept any string here.** A path that did not exist
// was printed under `runtime:` as if it were a fact, the desk started, and
// the first WebSocket connection failed to spawn the runtime — which the page
// reported as the connection closing and retried forever. A name with no path
// separator is looked up on PATH, as `exec.Command` would; a name with one is
// taken as a path and must exist, be a file, and carry an execute bit.
func ResolveRuntime(name string) (string, error) {
	if name == "" {
		return "", errors.New("no runtime binary was named: pass --jpack")
	}
	if !strings.ContainsRune(name, os.PathSeparator) {
		found, err := exec.LookPath(name)
		if err != nil {
			return "", fmt.Errorf("the runtime binary %q is not on PATH: %w", name, err)
		}
		return filepath.Abs(found)
	}
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("the runtime binary %s does not exist", abs)
		}
		return "", fmt.Errorf("the runtime binary %s cannot be read: %w", abs, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("the runtime binary %s is a directory", abs)
	}
	if info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("the runtime binary %s is not executable", abs)
	}
	return abs, nil
}
