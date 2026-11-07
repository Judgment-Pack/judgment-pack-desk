package desk

import (
	"context"
	"os"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

// The existing AI registry admits at most32 current connections. The account
// owner may also retain removed connections in this process. Observe at most33
// distinct profiles plus one unknown-overflow row; never cap a child route.
func (s *Server) storageChildNative(ctx context.Context, w *storageChildWalk) []storageChildRootStatus {
	if ctx.Err() != nil {
		return []storageChildRootStatus{storageChildUnknown("native", "profile")}
	}
	root := s.deskOwner()
	if root == nil || !root.aiAccounts.mu.TryLock() {
		return []storageChildRootStatus{storageChildUnknown("native", "profile")}
	}
	defer root.aiAccounts.mu.Unlock()
	if root.aiAccounts.closed {
		return []storageChildRootStatus{storageChildUnknown("native", "profile")}
	}
	rows := []storageChildRootStatus{}
	seen := map[string]bool{}
	add := func(id string, manager providerAccountManager, dir string) {
		if seen[id] {
			return
		}
		seen[id] = true
		row := storageChildUnknown("native", "profile")
		visit := func(held *os.Root) error { row = w.heldPath("native", "profile", held, held.Name()); return nil }
		var err error
		if observer, ok := manager.(interface {
			ObserveStorage(context.Context, func(*os.Root) error) error
		}); ok {
			err = observer.ObserveStorage(ctx, visit)
		} else if manager == nil && dir != "" {
			err = codexbridge.ObserveRetainedStorage(ctx, dir, visit)
		} else if manager == nil {
			row.State, row.Reason = "inactive", "none"
		}
		if err != nil {
			row = storageChildUnknown("native", "profile")
		}
		rows = append(rows, row)
	}
	add("legacy-codex", root.codex, "")
	// Public configuration only: no connection key, account status, get() or
	// native operation. readAIRegistry is bounded, read-only and shared with Admin.
	if root.assistant == nil || root.assistant.root == nil {
		return rows
	}
	configInfo, err := root.assistant.root.Stat(".")
	if err != nil || !sameDirectory(root.configDir, configInfo) {
		return append(rows, storageChildUnknown("native", "profile"))
	}
	registry, err := root.readAIRegistry()
	if err != nil {
		return append(rows, storageChildUnknown("native", "profile"))
	}
	for _, connection := range registry.Connections {
		if connection.Assistant.Engine != "codex" || connection.ID == "legacy-codex" {
			continue
		}
		add(connection.ID, root.aiAccounts.values[connection.ID], platformPath(root.configDir, "codex-"+connection.ID))
	}
	// Removed but still retained native managers remain observable. The bounded
	// prefix is not a complete inventory when this lifetime has more old owners.
	inspected := 0
	overflow := false
	for id, manager := range root.aiAccounts.values {
		if inspected == 33 || ctx.Err() != nil {
			overflow = true
			break
		}
		inspected++
		if seen[id] {
			continue
		}
		if len(seen) == 33 {
			overflow = true
			break
		}
		add(id, manager, "")
	}
	if ctx.Err() != nil {
		return []storageChildRootStatus{storageChildUnknown("native", "profile")}
	}
	after, err := root.readAIRegistry()
	if err != nil || registry.SHA256 != after.SHA256 || !sameDirectory(root.configDir, configInfo) {
		return []storageChildRootStatus{storageChildUnknown("native", "profile")}
	}
	if overflow {
		rows = append(rows, storageChildUnknown("native", "profile"))
	}
	return rows
}
