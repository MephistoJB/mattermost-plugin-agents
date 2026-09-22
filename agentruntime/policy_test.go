// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package agentruntime

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveEffectivePolicy(t *testing.T) {
	fallback := RuntimePolicy{
		ID:          "server-default",
		ScopeType:   PolicyScopeServer,
		ScopeID:     "server",
		RuntimeType: RuntimeTypeCodex,
		ProviderID:  "codex",
		Model:       "gpt-5-codex",
		AllowCloud:  true,
		AllowLocal:  true,
	}

	tests := []struct {
		name        string
		policies    []RuntimePolicy
		lookup      PolicyLookup
		expectedID  string
		expectedSrc PolicyScopeType
		expectedErr error
	}{
		{
			name: "thread overrides channel and server",
			policies: []RuntimePolicy{
				{ID: "server", ScopeType: PolicyScopeServer, ScopeID: "server", RuntimeType: RuntimeTypeCodex, AllowCloud: true},
				{ID: "channel", ScopeType: PolicyScopeChannel, ScopeID: "channel", RuntimeType: RuntimeTypeLocal, AllowCloud: true, AllowLocal: true},
				{ID: "thread", ScopeType: PolicyScopeThread, ScopeID: "thread", RuntimeType: RuntimeTypeOpenAI, AllowCloud: true},
			},
			lookup:      PolicyLookup{ServerID: "server", ChannelID: "channel", ThreadID: "thread"},
			expectedID:  "thread",
			expectedSrc: PolicyScopeThread,
		},
		{
			name: "channel overrides agent",
			policies: []RuntimePolicy{
				{ID: "agent", ScopeType: PolicyScopeAgent, ScopeID: "agent", RuntimeType: RuntimeTypeCodex, AllowCloud: true},
				{ID: "channel", ScopeType: PolicyScopeChannel, ScopeID: "channel", RuntimeType: RuntimeTypeLocal, AllowLocal: true},
			},
			lookup:      PolicyLookup{ChannelID: "channel", AgentID: "agent"},
			expectedID:  "channel",
			expectedSrc: PolicyScopeChannel,
		},
		{
			name: "user policy is only considered in DMs",
			policies: []RuntimePolicy{
				{ID: "user", ScopeType: PolicyScopeUser, ScopeID: "user", RuntimeType: RuntimeTypeLocal, AllowLocal: true},
				{ID: "team", ScopeType: PolicyScopeTeam, ScopeID: "team", RuntimeType: RuntimeTypeCodex, AllowCloud: true},
			},
			lookup:      PolicyLookup{TeamID: "team", UserID: "user", IsDM: false},
			expectedID:  "team",
			expectedSrc: PolicyScopeTeam,
		},
		{
			name:        "fallback is used when no scoped policy matches",
			lookup:      PolicyLookup{ChannelID: "unknown"},
			expectedID:  "server-default",
			expectedSrc: PolicyScopeServer,
		},
		{
			name: "local policy must allow local execution",
			policies: []RuntimePolicy{
				{ID: "channel", ScopeType: PolicyScopeChannel, ScopeID: "channel", RuntimeType: RuntimeTypeLocal, AllowLocal: false},
			},
			lookup:      PolicyLookup{ChannelID: "channel"},
			expectedErr: ErrLocalNotAllowed,
		},
		{
			name: "cloud policy must allow cloud execution",
			policies: []RuntimePolicy{
				{ID: "channel", ScopeType: PolicyScopeChannel, ScopeID: "channel", RuntimeType: RuntimeTypeOpenAI, AllowCloud: false},
			},
			lookup:      PolicyLookup{ChannelID: "channel"},
			expectedErr: ErrCloudNotAllowed,
		},
		{
			name: "inherit policy falls through to lower scope",
			policies: []RuntimePolicy{
				{ID: "thread-inherit", ScopeType: PolicyScopeThread, ScopeID: "thread", RuntimeType: RuntimeTypeInherit},
				{ID: "channel", ScopeType: PolicyScopeChannel, ScopeID: "channel", RuntimeType: RuntimeTypeLocal, AllowLocal: true},
			},
			lookup:      PolicyLookup{ThreadID: "thread", ChannelID: "channel"},
			expectedID:  "channel",
			expectedSrc: PolicyScopeChannel,
		},
		{
			name: "thread cloud escalation is blocked by local-only channel",
			policies: []RuntimePolicy{
				{ID: "server", ScopeType: PolicyScopeServer, ScopeID: "server", RuntimeType: RuntimeTypeCodex, AllowCloud: true},
				{ID: "channel", ScopeType: PolicyScopeChannel, ScopeID: "channel", RuntimeType: RuntimeTypeLocal, AllowCloud: false, AllowLocal: true},
				{ID: "thread", ScopeType: PolicyScopeThread, ScopeID: "thread", RuntimeType: RuntimeTypeCodex, AllowCloud: true},
			},
			lookup:      PolicyLookup{ServerID: "server", ChannelID: "channel", ThreadID: "thread"},
			expectedErr: ErrCloudNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveEffectivePolicy(tt.policies, tt.lookup, fallback)
			if tt.expectedErr != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tt.expectedErr))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectedID, got.Policy.ID)
			assert.Equal(t, tt.expectedSrc, got.Source)
		})
	}
}
