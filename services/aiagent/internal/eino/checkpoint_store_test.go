package eino

import (
	"context"
	"testing"
	"time"

	aiagentruns "github.com/leventsg/e-commerce-AI-system/dal/model/ai/agent_runs"
)

func TestPersistentCheckpointStoreFallsBackToModelBlob(t *testing.T) {
	model := &fakeAgentRunsModel{
		rows: map[string]*aiagentruns.AiAgentRuns{
			"checkpoint-1": {CheckpointId: "checkpoint-1", CheckpointBlob: []byte("checkpoint-data")},
		},
	}
	store := NewPersistentCheckpointStore(nil, model, time.Minute)

	data, ok, err := store.Get(context.Background(), "checkpoint-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || string(data) != "checkpoint-data" {
		t.Fatalf("checkpoint = %q ok=%v, want model blob", string(data), ok)
	}
}

func TestPersistentCheckpointStoreSetPersistsModelBlob(t *testing.T) {
	model := &fakeAgentRunsModel{rows: make(map[string]*aiagentruns.AiAgentRuns)}
	store := NewPersistentCheckpointStore(nil, model, time.Minute)

	if err := store.Set(context.Background(), "checkpoint-1", []byte("checkpoint-data")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	row := model.rows["checkpoint-1"]
	if row == nil || row.RunId != "checkpoint-1" || string(row.CheckpointBlob) != "checkpoint-data" {
		t.Fatalf("stored row = %#v", row)
	}
}

type fakeAgentRunsModel struct {
	rows map[string]*aiagentruns.AiAgentRuns
}

func (m *fakeAgentRunsModel) FindOneByCheckpointID(_ context.Context, checkpointID string) (*aiagentruns.AiAgentRuns, error) {
	row, ok := m.rows[checkpointID]
	if !ok {
		return nil, aiagentruns.ErrNotFound
	}
	copied := *row
	copied.CheckpointBlob = append([]byte(nil), row.CheckpointBlob...)
	return &copied, nil
}

func (m *fakeAgentRunsModel) UpsertCheckpoint(_ context.Context, row *aiagentruns.AiAgentRuns) error {
	if m.rows == nil {
		m.rows = make(map[string]*aiagentruns.AiAgentRuns)
	}
	copied := *row
	copied.CheckpointBlob = append([]byte(nil), row.CheckpointBlob...)
	m.rows[row.CheckpointId] = &copied
	return nil
}

func (m *fakeAgentRunsModel) DeleteByCheckpointID(_ context.Context, checkpointID string) error {
	delete(m.rows, checkpointID)
	return nil
}
