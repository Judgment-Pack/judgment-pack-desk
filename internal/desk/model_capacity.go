package desk

import "github.com/Judgment-Pack/judgment-pack-desk/internal/delegation"

var modelCapacity delegation.Capacity

// Configured endpoint and credential reference identify the binding. No key
// material enters the scheduler, journal, activity response or error message.
func (s *Server) modelCapacityKey(endpoint, keyName string) string {
	owner := s
	for owner.cfg.parent != nil {
		owner = owner.cfg.parent
	}
	return digestOf([]byte(owner.configDir + "\n" + endpoint + "\n" + keyName))
}
