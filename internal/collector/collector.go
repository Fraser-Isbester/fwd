package collector

import (
	"context"
	"sync"

	"github.com/fraser-isbester/fwd/pkg/types"
)

// Collector interface defines the contract for all collectors
type Collector interface {
	Start(context.Context, chan<- *types.Event) error
	Stop() error
	Name() string
}

// RepoState tracks the last seen event for a repository
type RepoState struct {
	lastEventID string
	mu          sync.RWMutex
}

func (rs *RepoState) Update(id string) {
	rs.mu.Lock()
	rs.lastEventID = id
	rs.mu.Unlock()
}

func (rs *RepoState) Get() string {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	return rs.lastEventID
}

// EventTracker maintains state for all repositories
type EventTracker struct {
	repos map[string]*RepoState
	mu    sync.RWMutex
}

func NewEventTracker() *EventTracker {
	return &EventTracker{
		repos: make(map[string]*RepoState),
	}
}

func (et *EventTracker) GetLastEventID(repo string) string {
	et.mu.RLock()
	state, exists := et.repos[repo]
	et.mu.RUnlock()

	if !exists {
		et.mu.Lock()
		et.repos[repo] = &RepoState{}
		state = et.repos[repo]
		et.mu.Unlock()
	}
	return state.Get()
}

func (et *EventTracker) UpdateLastEventID(repo, id string) {
	et.mu.RLock()
	state, exists := et.repos[repo]
	et.mu.RUnlock()

	if !exists {
		et.mu.Lock()
		state = &RepoState{}
		et.repos[repo] = state
		et.mu.Unlock()
	}
	state.Update(id)
}
