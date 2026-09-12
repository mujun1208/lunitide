package agenthub

import (
	"fmt"
	"strings"
	"sync"
)

type MemoryStore struct {
	mu     sync.Mutex
	tasks  map[string]TaskRecord
	keys   map[string]string
	events map[string][]AgentEvent
	arts   map[string]map[string]Artifact
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{tasks: map[string]TaskRecord{}, keys: map[string]string{}, events: map[string][]AgentEvent{}, arts: map[string]map[string]Artifact{}}
}

func (s *MemoryStore) InsertTask(task TaskRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.keys[task.IdempotencyKey]; ok {
		return fmt.Errorf("duplicate key")
	}
	s.tasks[task.ID] = task
	s.keys[task.IdempotencyKey] = task.ID
	return nil
}

func (s *MemoryStore) GetTask(id string) (TaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return TaskRecord{}, fmt.Errorf("not found")
	}
	return task, nil
}

func (s *MemoryStore) GetTaskByKey(key string) (TaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.keys[key]
	if !ok {
		return TaskRecord{}, fmt.Errorf("not found")
	}
	return s.tasks[id], nil
}

func (s *MemoryStore) UpdateTask(task TaskRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.ID] = task
	return nil
}

func (s *MemoryStore) ListTasks(filter ListFilter) ([]TaskRecord, TaskCounts, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []TaskRecord
	var counts TaskCounts
	for _, task := range s.tasks {
		switch task.Status {
		case "queued":
			counts.Queued++
		case "running":
			counts.Running++
		case "success":
			counts.Success++
		case "failed", "timeout":
			counts.Failed++
		}
		if filter.Agent != "" && task.Agent != filter.Agent {
			continue
		}
		if filter.Status != "" && task.Status != filter.Status {
			continue
		}
		if filter.DateFrom != "" && task.CreatedAt < filter.DateFrom {
			continue
		}
		if filter.DateTo != "" && !strings.HasPrefix(task.CreatedAt, filter.DateTo) && task.CreatedAt > filter.DateTo+"T23:59:59Z" {
			continue
		}
		items = append(items, task)
	}
	return items, counts, nil
}

func (s *MemoryStore) NextEventSeq(taskID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events[taskID]) + 1, nil
}

func (s *MemoryStore) InsertEvent(taskID string, event AgentEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events[taskID] = append(s.events[taskID], event)
	return nil
}

func (s *MemoryStore) ListEvents(taskID string) ([]AgentEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]AgentEvent(nil), s.events[taskID]...), nil
}

func (s *MemoryStore) UpsertArtifact(taskID string, art Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.arts[taskID] == nil {
		s.arts[taskID] = map[string]Artifact{}
	}
	s.arts[taskID][art.Path] = art
	return nil
}

func (s *MemoryStore) ListArtifacts(taskID string) ([]Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Artifact, 0, len(s.arts[taskID]))
	for _, art := range s.arts[taskID] {
		out = append(out, art)
	}
	return out, nil
}

func (s *MemoryStore) ListAllArtifacts(filter ListFilter) ([]Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Artifact
	for taskID, arts := range s.arts {
		task := s.tasks[taskID]
		if filter.Agent != "" && task.Agent != filter.Agent {
			continue
		}
		for _, art := range arts {
			art.TaskID = taskID
			art.Agent = task.Agent
			art.CreatedAt = task.CreatedAt
			if filter.Ext != "" && !strings.HasSuffix(strings.ToLower(art.Name), strings.ToLower(filter.Ext)) {
				continue
			}
			out = append(out, art)
		}
	}
	return out, nil
}
