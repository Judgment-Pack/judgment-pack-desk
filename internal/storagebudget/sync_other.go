//go:build !darwin

package storagebudget

import "os"

// Preserve the original platform persistence operation outside Darwin.
func syncFile(f *os.File) error { return f.Sync() }
