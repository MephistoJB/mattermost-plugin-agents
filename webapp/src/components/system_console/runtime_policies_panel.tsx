// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React, {useCallback, useEffect, useMemo, useState} from 'react';
import styled from 'styled-components';
import {RefreshIcon} from '@mattermost/compass-icons/components';
import {FormattedMessage, useIntl} from 'react-intl';

import {getRuntimePolicies, upsertRuntimePolicy} from '@/client';
import type {RuntimePolicy, RuntimeType} from '@/types/runtime';

import {ButtonIcon} from '../assets/buttons';

import Panel from './panel';

const runtimeTypes: RuntimeType[] = ['codex', 'openai', 'local', 'inherit'];
const scopeTypes = ['server', 'team', 'channel', 'thread', 'user', 'agent'];
const budgetWindows = ['', 'daily', 'weekly', 'monthly'];

type DraftPolicy = {
    id: string;
    scopeType: string;
    scopeID: string;
    runtimeType: RuntimeType;
    providerID: string;
    model: string;
    allowCloud: boolean;
    allowLocal: boolean;
    cloudBudgetCents: number;
    cloudBudgetWindow: string;
    cloudEscalationConfirmed: boolean;
};

const emptyDraft: DraftPolicy = {
    id: '',
    scopeType: 'server',
    scopeID: '',
    runtimeType: 'codex',
    providerID: '',
    model: '',
    allowCloud: true,
    allowLocal: false,
    cloudBudgetCents: 0,
    cloudBudgetWindow: '',
    cloudEscalationConfirmed: false,
};

export default function RuntimePoliciesPanel() {
    const intl = useIntl();
    const [policies, setPolicies] = useState<RuntimePolicy[]>([]);
    const [drafts, setDrafts] = useState<Record<string, DraftPolicy>>({});
    const [newDraft, setNewDraft] = useState<DraftPolicy>(emptyDraft);
    const [loading, setLoading] = useState(true);
    const [savingID, setSavingID] = useState('');
    const [error, setError] = useState('');

    const loadPolicies = useCallback(async () => {
        setLoading(true);
        try {
            const nextPolicies = await getRuntimePolicies();
            setPolicies(nextPolicies);
            setDrafts(Object.fromEntries(nextPolicies.map((policy) => [policy.id, draftFromPolicy(policy)])));
            setError('');
        } catch {
            setError(intl.formatMessage({id: 'runtime_policies_panel.load_error', defaultMessage: 'Failed to load runtime policies.'}));
        } finally {
            setLoading(false);
        }
    }, [intl]);

    useEffect(() => {
        loadPolicies();
    }, [loadPolicies]);

    const sortedPolicies = useMemo(() => [...policies].sort((a, b) => {
        const scopeCompare = a.scopeType.localeCompare(b.scopeType);
        if (scopeCompare !== 0) {
            return scopeCompare;
        }
        return a.scopeID.localeCompare(b.scopeID);
    }), [policies]);

    const saveDraft = useCallback(async (draft: DraftPolicy) => {
        if (!draft.scopeID.trim()) {
            setError(intl.formatMessage({id: 'runtime_policies_panel.scope_required', defaultMessage: 'Scope ID is required.'}));
            return;
        }
        if (runtimePolicyCanUseCloud(draft) && !draft.cloudEscalationConfirmed) {
            setError(intl.formatMessage({id: 'runtime_policies_panel.cloud_confirmation_required', defaultMessage: 'Confirm cloud execution before saving this policy.'}));
            return;
        }
        setSavingID(draft.id || 'new');
        try {
            await upsertRuntimePolicy(draft.scopeType, draft.scopeID.trim(), {
                runtimeType: draft.runtimeType,
                providerID: draft.providerID.trim(),
                model: draft.model.trim(),
                allowCloud: draft.allowCloud,
                allowLocal: draft.allowLocal,
                cloudBudgetCents: Math.max(0, Math.round(draft.cloudBudgetCents || 0)),
                cloudBudgetWindow: draft.cloudBudgetWindow,
                cloudEscalationConfirmed: runtimePolicyCanUseCloud(draft),
            });
            if (!draft.id) {
                setNewDraft(emptyDraft);
            }
            await loadPolicies();
        } catch {
            setError(intl.formatMessage({id: 'runtime_policies_panel.save_error', defaultMessage: 'Failed to save runtime policy.'}));
        } finally {
            setSavingID('');
        }
    }, [intl, loadPolicies]);

    return (
        <Panel
            title={intl.formatMessage({id: 'runtime_policies_panel.title', defaultMessage: 'Runtime Policies'})}
            subtitle={intl.formatMessage({id: 'runtime_policies_panel.subtitle', defaultMessage: 'Control cloud/local routing, models, and cloud budgets by scope.'})}
        >
            <HeaderRow>
                <PolicyCount>
                    <FormattedMessage
                        id='runtime_policies_panel.policy_count'
                        defaultMessage='{count, plural, one {# policy} other {# policies}}'
                        values={{count: policies.length}}
                    />
                </PolicyCount>
                <ButtonIcon
                    type='button'
                    aria-label={intl.formatMessage({id: 'runtime_policies_panel.refresh', defaultMessage: 'Refresh runtime policies'})}
                    onClick={loadPolicies}
                    disabled={loading}
                >
                    <RefreshIcon size={18}/>
                </ButtonIcon>
            </HeaderRow>
            {error && <ErrorText>{error}</ErrorText>}
            <PolicyTable>
                <PolicyHeader>
                    <span>
                        <FormattedMessage
                            id='runtime_policies_panel.scope'
                            defaultMessage='Scope'
                        />
                    </span>
                    <span>
                        <FormattedMessage
                            id='runtime_policies_panel.runtime'
                            defaultMessage='Runtime'
                        />
                    </span>
                    <span>
                        <FormattedMessage
                            id='runtime_policies_panel.routing'
                            defaultMessage='Routing'
                        />
                    </span>
                    <span>
                        <FormattedMessage
                            id='runtime_policies_panel.budget'
                            defaultMessage='Budget'
                        />
                    </span>
                    <span/>
                </PolicyHeader>
                {renderPolicyRows({
                    loading,
                    sortedPolicies,
                    drafts,
                    savingID,
                    intl,
                    saveDraft,
                    setDrafts,
                })}
                <PolicyRow>
                    <ScopeGroup>
                        <SmallSelect
                            aria-label={intl.formatMessage({id: 'runtime_policies_panel.new_scope_type', defaultMessage: 'New policy scope type'})}
                            value={newDraft.scopeType}
                            onChange={(event) => setNewDraft({...newDraft, scopeType: event.currentTarget.value})}
                        >
                            {scopeTypes.map((scopeType) => (
                                <option
                                    key={scopeType}
                                    value={scopeType}
                                >
                                    {scopeType}
                                </option>
                            ))}
                        </SmallSelect>
                        <TextInput
                            aria-label={intl.formatMessage({id: 'runtime_policies_panel.new_scope_id', defaultMessage: 'New policy scope ID'})}
                            placeholder={intl.formatMessage({id: 'runtime_policies_panel.scope_id_placeholder', defaultMessage: 'scope id'})}
                            value={newDraft.scopeID}
                            onChange={(event) => setNewDraft({...newDraft, scopeID: event.currentTarget.value})}
                        />
                    </ScopeGroup>
                    <RuntimeGroup>
                        <SmallSelect
                            aria-label={intl.formatMessage({id: 'runtime_policies_panel.new_runtime_type', defaultMessage: 'New policy runtime type'})}
                            value={newDraft.runtimeType}
                            onChange={(event) => setNewDraft({...newDraft, runtimeType: event.currentTarget.value as RuntimeType})}
                        >
                            {runtimeTypes.map((runtimeType) => (
                                <option
                                    key={runtimeType}
                                    value={runtimeType}
                                >
                                    {runtimeType}
                                </option>
                            ))}
                        </SmallSelect>
                        <TextInput
                            aria-label={intl.formatMessage({id: 'runtime_policies_panel.new_model', defaultMessage: 'New policy model'})}
                            placeholder={intl.formatMessage({id: 'runtime_policies_panel.model_placeholder', defaultMessage: 'model'})}
                            value={newDraft.model}
                            onChange={(event) => setNewDraft({...newDraft, model: event.currentTarget.value})}
                        />
                    </RuntimeGroup>
                    <RoutingGroup>
                        <TextInput
                            aria-label={intl.formatMessage({id: 'runtime_policies_panel.new_provider', defaultMessage: 'New policy provider ID'})}
                            placeholder={intl.formatMessage({id: 'runtime_policies_panel.provider_placeholder', defaultMessage: 'provider'})}
                            value={newDraft.providerID}
                            onChange={(event) => setNewDraft({...newDraft, providerID: event.currentTarget.value})}
                        />
                        <ToggleLabel>
                            <input
                                type='checkbox'
                                checked={newDraft.allowCloud}
                                onChange={(event) => setNewDraft({...newDraft, allowCloud: event.currentTarget.checked})}
                            />
                            <FormattedMessage
                                id='runtime_policies_panel.cloud'
                                defaultMessage='Cloud'
                            />
                        </ToggleLabel>
                        <ToggleLabel>
                            <input
                                type='checkbox'
                                checked={newDraft.allowLocal}
                                onChange={(event) => setNewDraft({...newDraft, allowLocal: event.currentTarget.checked})}
                            />
                            <FormattedMessage
                                id='runtime_policies_panel.local'
                                defaultMessage='Local'
                            />
                        </ToggleLabel>
                        {runtimePolicyCanUseCloud(newDraft) && (
                            <ToggleLabel>
                                <input
                                    type='checkbox'
                                    aria-label={intl.formatMessage({id: 'runtime_policies_panel.new_cloud_escalation_confirmed', defaultMessage: 'Confirm new policy cloud execution'})}
                                    checked={newDraft.cloudEscalationConfirmed}
                                    onChange={(event) => setNewDraft({...newDraft, cloudEscalationConfirmed: event.currentTarget.checked})}
                                />
                                <FormattedMessage
                                    id='runtime_policies_panel.confirm_cloud'
                                    defaultMessage='Confirm cloud'
                                />
                            </ToggleLabel>
                        )}
                    </RoutingGroup>
                    <BudgetGroup>
                        <NumberInput
                            aria-label={intl.formatMessage({id: 'runtime_policies_panel.new_cloud_budget_cents', defaultMessage: 'New policy cloud budget cents'})}
                            type='number'
                            min={0}
                            step={1}
                            value={newDraft.cloudBudgetCents}
                            onChange={(event) => setNewDraft({...newDraft, cloudBudgetCents: Number(event.currentTarget.value) || 0})}
                        />
                        <Unit>
                            <FormattedMessage
                                id='runtime_policies_panel.cents'
                                defaultMessage='cents'
                            />
                        </Unit>
                        <SmallSelect
                            aria-label={intl.formatMessage({id: 'runtime_policies_panel.new_budget_window', defaultMessage: 'New policy budget window'})}
                            value={newDraft.cloudBudgetWindow}
                            onChange={(event) => setNewDraft({...newDraft, cloudBudgetWindow: event.currentTarget.value})}
                        >
                            {budgetWindows.map((window) => (
                                <option
                                    key={window || 'none'}
                                    value={window}
                                >
                                    {window || 'none'}
                                </option>
                            ))}
                        </SmallSelect>
                    </BudgetGroup>
                    <ActionButton
                        type='button'
                        onClick={() => saveDraft(newDraft)}
                        disabled={savingID === 'new'}
                    >
                        <FormattedMessage
                            id='runtime_policies_panel.add'
                            defaultMessage='Add'
                        />
                    </ActionButton>
                </PolicyRow>
            </PolicyTable>
        </Panel>
    );
}

function renderPolicyRows(props: {
    loading: boolean;
    sortedPolicies: RuntimePolicy[];
    drafts: Record<string, DraftPolicy>;
    savingID: string;
    intl: ReturnType<typeof useIntl>;
    saveDraft: (draft: DraftPolicy) => void;
    setDrafts: React.Dispatch<React.SetStateAction<Record<string, DraftPolicy>>>;
}) {
    if (props.loading && props.sortedPolicies.length === 0) {
        return (
            <EmptyState>
                <FormattedMessage
                    id='runtime_policies_panel.loading'
                    defaultMessage='Loading policies...'
                />
            </EmptyState>
        );
    }

    if (props.sortedPolicies.length === 0) {
        return (
            <EmptyState>
                <FormattedMessage
                    id='runtime_policies_panel.empty'
                    defaultMessage='No runtime policies found.'
                />
            </EmptyState>
        );
    }

    return props.sortedPolicies.map((policy) => {
        const draft = props.drafts[policy.id] || draftFromPolicy(policy);
        return (
            <PolicyRow key={policy.id}>
                <ScopeGroup>
                    <SmallSelect
                        aria-label={props.intl.formatMessage({id: 'runtime_policies_panel.scope_type', defaultMessage: 'Scope type'})}
                        value={draft.scopeType}
                        onChange={(event) => props.setDrafts({...props.drafts, [policy.id]: {...draft, scopeType: event.currentTarget.value}})}
                    >
                        {scopeTypes.map((scopeType) => (
                            <option
                                key={scopeType}
                                value={scopeType}
                            >
                                {scopeType}
                            </option>
                        ))}
                    </SmallSelect>
                    <TextInput
                        aria-label={props.intl.formatMessage({id: 'runtime_policies_panel.scope_id', defaultMessage: 'Scope ID'})}
                        value={draft.scopeID}
                        onChange={(event) => props.setDrafts({...props.drafts, [policy.id]: {...draft, scopeID: event.currentTarget.value}})}
                    />
                </ScopeGroup>
                <RuntimeGroup>
                    <SmallSelect
                        aria-label={props.intl.formatMessage({id: 'runtime_policies_panel.runtime_type', defaultMessage: 'Runtime type'})}
                        value={draft.runtimeType}
                        onChange={(event) => props.setDrafts({...props.drafts, [policy.id]: {...draft, runtimeType: event.currentTarget.value as RuntimeType}})}
                    >
                        {runtimeTypes.map((runtimeType) => (
                            <option
                                key={runtimeType}
                                value={runtimeType}
                            >
                                {runtimeType}
                            </option>
                        ))}
                    </SmallSelect>
                    <TextInput
                        aria-label={props.intl.formatMessage({id: 'runtime_policies_panel.model', defaultMessage: 'Model'})}
                        placeholder={props.intl.formatMessage({id: 'runtime_policies_panel.model_placeholder', defaultMessage: 'model'})}
                        value={draft.model}
                        onChange={(event) => props.setDrafts({...props.drafts, [policy.id]: {...draft, model: event.currentTarget.value}})}
                    />
                </RuntimeGroup>
                <RoutingGroup>
                    <TextInput
                        aria-label={props.intl.formatMessage({id: 'runtime_policies_panel.provider', defaultMessage: 'Provider ID'})}
                        placeholder={props.intl.formatMessage({id: 'runtime_policies_panel.provider_placeholder', defaultMessage: 'provider'})}
                        value={draft.providerID}
                        onChange={(event) => props.setDrafts({...props.drafts, [policy.id]: {...draft, providerID: event.currentTarget.value}})}
                    />
                    <ToggleLabel>
                        <input
                            type='checkbox'
                            checked={draft.allowCloud}
                            onChange={(event) => props.setDrafts({...props.drafts, [policy.id]: {...draft, allowCloud: event.currentTarget.checked}})}
                        />
                        <FormattedMessage
                            id='runtime_policies_panel.cloud'
                            defaultMessage='Cloud'
                        />
                    </ToggleLabel>
                    <ToggleLabel>
                        <input
                            type='checkbox'
                            checked={draft.allowLocal}
                            onChange={(event) => props.setDrafts({...props.drafts, [policy.id]: {...draft, allowLocal: event.currentTarget.checked}})}
                        />
                        <FormattedMessage
                            id='runtime_policies_panel.local'
                            defaultMessage='Local'
                        />
                    </ToggleLabel>
                    {runtimePolicyCanUseCloud(draft) && (
                        <ToggleLabel>
                            <input
                                type='checkbox'
                                aria-label={props.intl.formatMessage({id: 'runtime_policies_panel.cloud_escalation_confirmed', defaultMessage: 'Confirm policy cloud execution'})}
                                checked={draft.cloudEscalationConfirmed}
                                onChange={(event) => props.setDrafts({...props.drafts, [policy.id]: {...draft, cloudEscalationConfirmed: event.currentTarget.checked}})}
                            />
                            <FormattedMessage
                                id='runtime_policies_panel.confirm_cloud'
                                defaultMessage='Confirm cloud'
                            />
                        </ToggleLabel>
                    )}
                </RoutingGroup>
                <BudgetGroup>
                    <NumberInput
                        aria-label={props.intl.formatMessage({id: 'runtime_policies_panel.cloud_budget_cents', defaultMessage: 'Cloud budget cents'})}
                        type='number'
                        min={0}
                        step={1}
                        value={draft.cloudBudgetCents}
                        onChange={(event) => props.setDrafts({...props.drafts, [policy.id]: {...draft, cloudBudgetCents: Number(event.currentTarget.value) || 0}})}
                    />
                    <Unit>
                        <FormattedMessage
                            id='runtime_policies_panel.cents'
                            defaultMessage='cents'
                        />
                    </Unit>
                    <SmallSelect
                        aria-label={props.intl.formatMessage({id: 'runtime_policies_panel.budget_window', defaultMessage: 'Budget window'})}
                        value={draft.cloudBudgetWindow}
                        onChange={(event) => props.setDrafts({...props.drafts, [policy.id]: {...draft, cloudBudgetWindow: event.currentTarget.value}})}
                    >
                        {budgetWindows.map((window) => (
                            <option
                                key={window || 'none'}
                                value={window}
                            >
                                {window || 'none'}
                            </option>
                        ))}
                    </SmallSelect>
                </BudgetGroup>
                <ActionButton
                    type='button'
                    onClick={() => props.saveDraft(draft)}
                    disabled={props.savingID === policy.id}
                >
                    <FormattedMessage
                        id='runtime_policies_panel.save'
                        defaultMessage='Save'
                    />
                </ActionButton>
            </PolicyRow>
        );
    });
}

function draftFromPolicy(policy: RuntimePolicy): DraftPolicy {
    return {
        id: policy.id,
        scopeType: policy.scopeType,
        scopeID: policy.scopeID,
        runtimeType: policy.runtimeType,
        providerID: policy.providerID,
        model: policy.model,
        allowCloud: policy.allowCloud,
        allowLocal: policy.allowLocal,
        cloudBudgetCents: policy.cloudBudgetCents || 0,
        cloudBudgetWindow: policy.cloudBudgetWindow || '',
        cloudEscalationConfirmed: false,
    };
}

function runtimePolicyCanUseCloud(draft: Pick<DraftPolicy, 'runtimeType' | 'allowCloud'>): boolean {
    return draft.runtimeType === 'codex' || draft.runtimeType === 'openai' || draft.allowCloud;
}

const HeaderRow = styled.div`
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 12px;
`;

const PolicyCount = styled.div`
    color: rgba(var(--center-channel-color-rgb), 0.72);
    font-size: 13px;
`;

const PolicyTable = styled.div`
    display: grid;
    gap: 8px;
`;

const PolicyHeader = styled.div`
    display: grid;
    align-items: center;
    grid-template-columns: minmax(190px, 1.1fr) minmax(180px, 1fr) minmax(210px, 1.1fr) minmax(130px, 0.7fr) 72px;
    gap: 8px;
    color: rgba(var(--center-channel-color-rgb), 0.64);
    font-size: 12px;
    font-weight: 600;
`;

const PolicyRow = styled.div`
    display: grid;
    align-items: center;
    grid-template-columns: minmax(190px, 1.1fr) minmax(180px, 1fr) minmax(210px, 1.1fr) minmax(130px, 0.7fr) 72px;
    gap: 8px;
    padding: 8px 0;
    border-top: 1px solid rgba(var(--center-channel-color-rgb), 0.08);
`;

const ScopeGroup = styled.div`
    display: grid;
    grid-template-columns: 88px minmax(0, 1fr);
    gap: 6px;
`;

const RuntimeGroup = styled.div`
    display: grid;
    grid-template-columns: 90px minmax(0, 1fr);
    gap: 6px;
`;

const RoutingGroup = styled.div`
    display: grid;
    align-items: center;
    grid-template-columns: minmax(0, 1fr) auto auto;
    gap: 6px;
`;

const BudgetGroup = styled.div`
    display: grid;
    align-items: center;
    grid-template-columns: minmax(70px, 0.7fr) auto minmax(84px, 0.9fr);
    gap: 6px;
`;

const SmallSelect = styled.select`
    height: 32px;
    min-width: 0;
    border: 1px solid rgba(var(--center-channel-color-rgb), 0.16);
    border-radius: 4px;
    background: var(--center-channel-bg);
    color: var(--center-channel-color);
    font-size: 13px;
`;

const TextInput = styled.input`
    height: 32px;
    min-width: 0;
    border: 1px solid rgba(var(--center-channel-color-rgb), 0.16);
    border-radius: 4px;
    background: var(--center-channel-bg);
    color: var(--center-channel-color);
    font-size: 13px;
`;

const NumberInput = styled(TextInput)`
    width: 100%;
`;

const ToggleLabel = styled.label`
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: rgba(var(--center-channel-color-rgb), 0.8);
    font-size: 12px;
    white-space: nowrap;
`;

const Unit = styled.span`
    color: rgba(var(--center-channel-color-rgb), 0.64);
    font-size: 12px;
`;

const ActionButton = styled.button`
    height: 32px;
    border: 1px solid rgba(var(--button-bg-rgb), 0.28);
    border-radius: 4px;
    background: rgba(var(--button-bg-rgb), 0.08);
    color: var(--button-bg);
    font-size: 13px;
    font-weight: 600;
`;

const EmptyState = styled.div`
    padding: 12px 0;
    border-top: 1px solid rgba(var(--center-channel-color-rgb), 0.08);
    color: rgba(var(--center-channel-color-rgb), 0.64);
    font-size: 13px;
`;

const ErrorText = styled.div`
    margin-bottom: 8px;
    color: var(--error-text);
    font-size: 13px;
`;
