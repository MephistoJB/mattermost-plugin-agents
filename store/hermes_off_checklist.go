// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package store

import (
	"fmt"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost/server/public/model"
)

var hermesOffChecklistColumns = []string{
	"Key", "Status", "Detail", "UpdatedBy", "UpdatedAt",
}

func (s *Store) ListHermesOffChecklistStates() ([]agentruntime.HermesOffChecklistItemState, error) {
	query, args, err := s.builder.
		Select(hermesOffChecklistColumns...).
		From("Agents_HermesOffChecklist").
		OrderBy("Key ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build list Hermes-off checklist states query: %w", err)
	}
	var states []agentruntime.HermesOffChecklistItemState
	if err := s.db.Select(&states, query, args...); err != nil {
		return nil, fmt.Errorf("failed to list Hermes-off checklist states: %w", err)
	}
	if states == nil {
		states = []agentruntime.HermesOffChecklistItemState{}
	}
	return states, nil
}

func (s *Store) UpsertHermesOffChecklistState(state *agentruntime.HermesOffChecklistItemState) error {
	now := model.GetMillis()
	state.UpdatedAt = now

	query, args, err := s.builder.
		Insert("Agents_HermesOffChecklist").
		Columns(hermesOffChecklistColumns...).
		Values(state.Key, string(state.Status), state.Detail, state.UpdatedBy, state.UpdatedAt).
		Suffix(`
ON CONFLICT (Key) DO UPDATE SET
	Status = EXCLUDED.Status,
	Detail = EXCLUDED.Detail,
	UpdatedBy = EXCLUDED.UpdatedBy,
	UpdatedAt = EXCLUDED.UpdatedAt`).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build upsert Hermes-off checklist state query: %w", err)
	}
	if _, err := s.db.Exec(query, args...); err != nil {
		return fmt.Errorf("failed to upsert Hermes-off checklist state: %w", err)
	}
	return nil
}

func HermesOffChecklistStatesByKey(states []agentruntime.HermesOffChecklistItemState) map[string]agentruntime.HermesOffChecklistItemState {
	out := make(map[string]agentruntime.HermesOffChecklistItemState, len(states))
	for _, state := range states {
		out[state.Key] = state
	}
	return out
}
