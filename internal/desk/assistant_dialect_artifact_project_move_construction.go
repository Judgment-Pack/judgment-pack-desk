package desk

import (
	"context"
	"errors"
)

// Trusted pre-New inputs only. Providers have already been CREATEd or OPENed
// through trusted root descriptors and registered; no journal can install one.
// An explicit empty selection means native, whereas a missing configured
// project is an error. A child receives the same sealed table and registry.
type retainedArtifactServerConstruction struct {
	self     *retainedArtifactServerConstruction
	registry *retainedArtifactProviderRegistry
	projects map[string]string
}

func newRetainedArtifactServerConstruction(registry *retainedArtifactProviderRegistry, projects map[string]string) (*retainedArtifactServerConstruction, error) {
	if registry == nil || len(projects) == 0 || len(projects) > 1024 {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	c := &retainedArtifactServerConstruction{registry: registry, projects: make(map[string]string, len(projects))}
	c.self = c
	for project, provider := range projects {
		if !dialectArtifactOwnerFileIdentity(project) {
			return nil, errRetainedArtifactAuthorityUnavailable
		}
		if provider != "" {
			r := registry.byID[provider]
			if r == nil || r.registry != registry || r.provider == nil || r.provider.id != provider {
				return nil, errRetainedArtifactAuthorityUnavailable
			}
			if err := r.current(); err != nil {
				return nil, err
			}
		}
		c.projects[project] = provider
	}
	return c, nil
}

// Called before identity recovery and before any worker can start. Only the
// actual parent adopts/closes the registry. Failure before the self/parent
// checks does not adopt somebody else's copied construction object.
func (s *Server) bindRetainedArtifactConstruction() error {
	c := s.cfg.retainedArtifactConstruction
	if parent := s.cfg.parent; parent != nil {
		c = parent.cfg.retainedArtifactConstruction
		if s.cfg.retainedArtifactConstruction != c {
			return errRetainedArtifactAuthorityUnavailable
		}
	}
	if c == nil {
		return nil
	}
	if c.self != c || c.registry == nil {
		return errRetainedArtifactAuthorityUnavailable
	}
	if parent := s.cfg.parent; parent != nil && parent.retainedArtifactProviders != c.registry {
		return errRetainedArtifactAuthorityUnavailable
	}
	s.retainedArtifactProviders = c.registry
	info, err := s.root.Stat(".")
	if err != nil {
		return err
	}
	provider, exists := c.projects[identityKey(info)]
	if !exists {
		return errRetainedArtifactAuthorityUnavailable
	}
	if provider == "" {
		if s.cfg.parent == nil {
			ids := make([]string, 0, len(c.projects))
			for _, id := range c.projects {
				if id != "" {
					ids = append(ids, id)
				}
			}
			pins, e := pinDialectArtifactSelectedProviders(context.Background(), s, ids)
			if e != nil {
				return e
			}
			s.storageStartupProviders = pins
		}
		return nil
	}
	r := c.registry.byID[provider]
	if r == nil || r.registry != c.registry || r.provider == nil || r.provider.id != provider {
		return errRetainedArtifactAuthorityUnavailable
	}
	if err = r.current(); err != nil {
		return err
	}
	op, err := r.provider.pin()
	if err != nil {
		return err
	}
	err = r.provider.check(context.Background())
	if err != nil {
		return errors.Join(err, op.finish())
	}
	if s.cfg.parent == nil {
		ids := make([]string, 0, len(c.projects))
		for _, id := range c.projects {
			if id != "" {
				ids = append(ids, id)
			}
		}
		pins, e := pinDialectArtifactSelectedProviders(context.Background(), s, ids)
		if e != nil {
			return errors.Join(e, op.finish())
		}
		if e = op.finish(); e != nil {
			return errors.Join(e, pins.Close())
		}
		s.storageStartupProviders = pins
	} else {
		err = op.finish()
	}
	if err != nil {
		return err
	}
	s.retainedArtifacts = r.provider
	return nil
}
