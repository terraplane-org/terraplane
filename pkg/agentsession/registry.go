package agentsession

import (
	"context"
	"maps"
	"slices"
	"sync"

	"github.com/xyzjace/terraplane/pkg/log"
)

//go:generate mockgen -source=registry.go -destination=mock_agentsession/mock_registry.go -package=mock_agentsession

type Registry interface {
	Register(ctx context.Context, session Session) error
	Unregister(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (Session, error)
	GetAllAgents() []string
}

type registry struct {
	logger   log.Logger
	mu       sync.Mutex
	sessions map[string]Session
}

func (r *registry) Register(ctx context.Context, session Session) error {
	// TODO: Handle duplicate registration
	r.logger.Debug("Registering agent session", "id", session.ID())
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sessions[session.ID()] = session
	return nil
}

func (r *registry) Unregister(ctx context.Context, id string) error {
	r.logger.Debug("Unregistering agent session", "id", id)
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.sessions, id)
	return nil
}

func (r *registry) Get(ctx context.Context, id string) (Session, error) {
	r.logger.Debug("Getting agent session", "id", id)
	r.mu.Lock()
	defer r.mu.Unlock()

	session, ok := r.sessions[id]
	if !ok {
		return nil, nil
	}
	return session, nil
}

func (r *registry) GetAllAgents() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Collect(maps.Keys(r.sessions))
}

func NewRegistry(logger log.Logger) Registry {
	return &registry{
		logger:   logger,
		sessions: make(map[string]Session),
	}
}
