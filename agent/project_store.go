package agent

import (
	"context"
	"strings"
	"time"
)

// ProjectStore provides CRUD operations for project records.
type ProjectStore interface {
	FindByID(ctx context.Context, id string) (*Project, error)
	FindByDirectory(ctx context.Context, dir string) (*Project, error)
	FindByName(ctx context.Context, name string) (*Project, error)
	Upsert(ctx context.Context, project Project) error
	List(ctx context.Context) ([]Project, error)
	Delete(ctx context.Context, id string) error
}

// InMemoryProjectStore is an in-memory implementation of ProjectStore for testing.
type InMemoryProjectStore struct {
	projects map[string]*Project // keyed by ID
}

func NewInMemoryProjectStore() *InMemoryProjectStore {
	return &InMemoryProjectStore{projects: make(map[string]*Project)}
}

func (s *InMemoryProjectStore) FindByID(_ context.Context, id string) (*Project, error) {
	p := s.projects[id]
	if p == nil {
		return nil, nil
	}
	clone := *p
	return &clone, nil
}

func (s *InMemoryProjectStore) FindByDirectory(_ context.Context, dir string) (*Project, error) {
	for _, p := range s.projects {
		if p.Directory == dir {
			clone := *p
			return &clone, nil
		}
	}
	return nil, nil
}

func (s *InMemoryProjectStore) FindByName(_ context.Context, name string) (*Project, error) {
	for _, p := range s.projects {
		if strings.EqualFold(p.Name, name) {
			clone := *p
			return &clone, nil
		}
	}
	return nil, nil
}

func (s *InMemoryProjectStore) Upsert(_ context.Context, project Project) error {
	if project.CreatedAt.IsZero() {
		project.CreatedAt = time.Now().UTC()
	}
	project.LastUsed = time.Now().UTC()
	s.projects[project.ID] = &project
	return nil
}

func (s *InMemoryProjectStore) List(_ context.Context) ([]Project, error) {
	result := make([]Project, 0, len(s.projects))
	for _, p := range s.projects {
		result = append(result, *p)
	}
	return result, nil
}

func (s *InMemoryProjectStore) Delete(_ context.Context, id string) error {
	delete(s.projects, id)
	return nil
}
