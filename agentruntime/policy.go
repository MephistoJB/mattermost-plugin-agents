// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package agentruntime

type PolicyLookup struct {
	ServerID  string
	TeamID    string
	ChannelID string
	ThreadID  string
	UserID    string
	AgentID   string
	IsDM      bool
}

type EffectivePolicy struct {
	Policy RuntimePolicy
	Source PolicyScopeType
}

func ResolveEffectivePolicy(policies []RuntimePolicy, lookup PolicyLookup, fallback RuntimePolicy) (EffectivePolicy, error) {
	candidates := policyCandidates(lookup)
	for i, candidate := range candidates {
		for _, policy := range policies {
			if policy.ScopeType == candidate.scopeType && policy.ScopeID == candidate.scopeID && policy.RuntimeType != RuntimeTypeInherit {
				effective, err := ValidatePolicy(policy, candidate.scopeType)
				if err != nil {
					return EffectivePolicy{}, err
				}
				if cloudPolicy(policy) && broaderScopeDisallowsCloud(policies, candidates[i+1:]) {
					return EffectivePolicy{}, ErrCloudNotAllowed
				}
				return effective, nil
			}
		}
	}
	if fallback.RuntimeType == "" {
		fallback.RuntimeType = RuntimeTypeCodex
	}
	return ValidatePolicy(fallback, PolicyScopeServer)
}

func cloudPolicy(policy RuntimePolicy) bool {
	return policy.RuntimeType == RuntimeTypeCodex || policy.RuntimeType == RuntimeTypeOpenAI
}

func broaderScopeDisallowsCloud(policies []RuntimePolicy, broader []policyCandidate) bool {
	for _, candidate := range broader {
		for _, policy := range policies {
			if policy.ScopeType == candidate.scopeType && policy.ScopeID == candidate.scopeID && policy.RuntimeType != RuntimeTypeInherit {
				if !policy.AllowCloud {
					return true
				}
				break
			}
		}
	}
	return false
}

type policyCandidate struct {
	scopeType PolicyScopeType
	scopeID   string
}

func policyCandidates(lookup PolicyLookup) []policyCandidate {
	candidates := make([]policyCandidate, 0, 6)
	if lookup.ThreadID != "" {
		candidates = append(candidates, policyCandidate{PolicyScopeThread, lookup.ThreadID})
	}
	if lookup.ChannelID != "" {
		candidates = append(candidates, policyCandidate{PolicyScopeChannel, lookup.ChannelID})
	}
	if lookup.AgentID != "" {
		candidates = append(candidates, policyCandidate{PolicyScopeAgent, lookup.AgentID})
	}
	if lookup.IsDM && lookup.UserID != "" {
		candidates = append(candidates, policyCandidate{PolicyScopeUser, lookup.UserID})
	}
	if lookup.TeamID != "" {
		candidates = append(candidates, policyCandidate{PolicyScopeTeam, lookup.TeamID})
	}
	if lookup.ServerID != "" {
		candidates = append(candidates, policyCandidate{PolicyScopeServer, lookup.ServerID})
	}
	return candidates
}

func ValidatePolicy(policy RuntimePolicy, source PolicyScopeType) (EffectivePolicy, error) {
	switch policy.RuntimeType {
	case RuntimeTypeCodex, RuntimeTypeOpenAI:
		if !policy.AllowCloud {
			return EffectivePolicy{}, ErrCloudNotAllowed
		}
	case RuntimeTypeLocal:
		if !policy.AllowLocal {
			return EffectivePolicy{}, ErrLocalNotAllowed
		}
	case RuntimeTypeInherit, "":
		return EffectivePolicy{}, ErrPolicyNotFound
	}
	return EffectivePolicy{Policy: policy, Source: source}, nil
}
