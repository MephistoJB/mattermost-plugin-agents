// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React, {useCallback, useEffect, useMemo, useState} from 'react';
import styled from 'styled-components';
import {RefreshIcon} from '@mattermost/compass-icons/components';
import {FormattedMessage, useIntl} from 'react-intl';

import {createWorkspacePolicy, getWorkspacePolicies, updateWorkspacePolicy} from '@/client';
import type {WorkspacePolicy, WorkspacePolicyRequest} from '@/types/runtime';

import {ButtonIcon} from '../assets/buttons';

import Panel from './panel';

const workspaceModes = ['read_only', 'ask_write', 'full_access'];
const networkModes = ['none', 'ask', 'allowed'];
const shellModes = ['disabled', 'ask', 'allowed'];

type WorkspacePolicyDraft = {
    id: string;
    name: string;
    allowedRootsText: string;
    defaultWorkspacePath: string;
    mode: string;
    networkMode: string;
    shellMode: string;
};

const emptyDraft: WorkspacePolicyDraft = {
    id: '',
    name: '',
    allowedRootsText: '',
    defaultWorkspacePath: '',
    mode: 'ask_write',
    networkMode: 'ask',
    shellMode: 'ask',
};

export default function WorkspacePoliciesPanel() {
    const intl = useIntl();
    const [policies, setPolicies] = useState<WorkspacePolicy[]>([]);
    const [drafts, setDrafts] = useState<Record<string, WorkspacePolicyDraft>>({});
    const [newDraft, setNewDraft] = useState<WorkspacePolicyDraft>(emptyDraft);
    const [loading, setLoading] = useState(true);
    const [savingID, setSavingID] = useState('');
    const [error, setError] = useState('');

    const loadPolicies = useCallback(async () => {
        setLoading(true);
        try {
            const nextPolicies = await getWorkspacePolicies();
            setPolicies(nextPolicies);
            setDrafts(Object.fromEntries(nextPolicies.map((policy) => [policy.id, draftFromPolicy(policy)])));
            setError('');
        } catch {
            setError(intl.formatMessage({id: 'workspace_policies_panel.load_error', defaultMessage: 'Failed to load workspace policies.'}));
        } finally {
            setLoading(false);
        }
    }, [intl]);

    useEffect(() => {
        loadPolicies();
    }, [loadPolicies]);

    const sortedPolicies = useMemo(() => [...policies].sort((a, b) => a.name.localeCompare(b.name)), [policies]);

    const saveDraft = useCallback(async (draft: WorkspacePolicyDraft) => {
        if (!draft.name.trim()) {
            setError(intl.formatMessage({id: 'workspace_policies_panel.name_required', defaultMessage: 'Policy name is required.'}));
            return;
        }
        setSavingID(draft.id || 'new');
        try {
            const request = requestFromDraft(draft);
            if (draft.id) {
                await updateWorkspacePolicy(draft.id, request);
            } else {
                await createWorkspacePolicy(request);
                setNewDraft(emptyDraft);
            }
            await loadPolicies();
        } catch {
            setError(intl.formatMessage({id: 'workspace_policies_panel.save_error', defaultMessage: 'Failed to save workspace policy.'}));
        } finally {
            setSavingID('');
        }
    }, [intl, loadPolicies]);

    return (
        <Panel
            title={intl.formatMessage({id: 'workspace_policies_panel.title', defaultMessage: 'Workspace Policies'})}
            subtitle={intl.formatMessage({id: 'workspace_policies_panel.subtitle', defaultMessage: 'Define allowed roots and workspace access modes for Codex/local runtime sessions.'})}
        >
            <HeaderRow>
                <PolicyCount>
                    <FormattedMessage
                        id='workspace_policies_panel.policy_count'
                        defaultMessage='{count, plural, one {# policy} other {# policies}}'
                        values={{count: policies.length}}
                    />
                </PolicyCount>
                <ButtonIcon
                    type='button'
                    aria-label={intl.formatMessage({id: 'workspace_policies_panel.refresh', defaultMessage: 'Refresh workspace policies'})}
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
                            id='workspace_policies_panel.name'
                            defaultMessage='Name'
                        />
                    </span>
                    <span>
                        <FormattedMessage
                            id='workspace_policies_panel.roots'
                            defaultMessage='Roots'
                        />
                    </span>
                    <span>
                        <FormattedMessage
                            id='workspace_policies_panel.default_workspace'
                            defaultMessage='Default workspace'
                        />
                    </span>
                    <span>
                        <FormattedMessage
                            id='workspace_policies_panel.modes'
                            defaultMessage='Modes'
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
                    setDrafts,
                    saveDraft,
                })}
                <PolicyRow>
                    <TextInput
                        aria-label={intl.formatMessage({id: 'workspace_policies_panel.new_name', defaultMessage: 'New workspace policy name'})}
                        placeholder={intl.formatMessage({id: 'workspace_policies_panel.name_placeholder', defaultMessage: 'Policy name'})}
                        value={newDraft.name}
                        onChange={(event) => setNewDraft({...newDraft, name: event.currentTarget.value})}
                    />
                    <TextArea
                        aria-label={intl.formatMessage({id: 'workspace_policies_panel.new_roots', defaultMessage: 'New workspace policy allowed roots'})}
                        placeholder={intl.formatMessage({id: 'workspace_policies_panel.roots_placeholder', defaultMessage: '/workspace/project'})}
                        value={newDraft.allowedRootsText}
                        onChange={(event) => setNewDraft({...newDraft, allowedRootsText: event.currentTarget.value})}
                    />
                    <TextInput
                        aria-label={intl.formatMessage({id: 'workspace_policies_panel.new_default_workspace', defaultMessage: 'New workspace policy default workspace'})}
                        placeholder={intl.formatMessage({id: 'workspace_policies_panel.default_workspace_placeholder', defaultMessage: '/workspace/project'})}
                        value={newDraft.defaultWorkspacePath}
                        onChange={(event) => setNewDraft({...newDraft, defaultWorkspacePath: event.currentTarget.value})}
                    />
                    <ModesGroup>
                        <ModeSelects
                            draft={newDraft}
                            setDraft={setNewDraft}
                            labelPrefix='New workspace policy'
                        />
                    </ModesGroup>
                    <ActionButton
                        type='button'
                        onClick={() => saveDraft(newDraft)}
                        disabled={savingID === 'new'}
                    >
                        <FormattedMessage
                            id='workspace_policies_panel.add'
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
    sortedPolicies: WorkspacePolicy[];
    drafts: Record<string, WorkspacePolicyDraft>;
    savingID: string;
    intl: ReturnType<typeof useIntl>;
    setDrafts: React.Dispatch<React.SetStateAction<Record<string, WorkspacePolicyDraft>>>;
    saveDraft: (draft: WorkspacePolicyDraft) => void;
}) {
    if (props.loading && props.sortedPolicies.length === 0) {
        return (
            <EmptyState>
                <FormattedMessage
                    id='workspace_policies_panel.loading'
                    defaultMessage='Loading workspace policies...'
                />
            </EmptyState>
        );
    }
    if (props.sortedPolicies.length === 0) {
        return (
            <EmptyState>
                <FormattedMessage
                    id='workspace_policies_panel.empty'
                    defaultMessage='No workspace policies found.'
                />
            </EmptyState>
        );
    }

    return props.sortedPolicies.map((policy) => {
        const draft = props.drafts[policy.id] || draftFromPolicy(policy);
        return (
            <PolicyRow key={policy.id}>
                <TextInput
                    aria-label={props.intl.formatMessage({id: 'workspace_policies_panel.name_input', defaultMessage: 'Workspace policy name'})}
                    value={draft.name}
                    onChange={(event) => props.setDrafts({...props.drafts, [policy.id]: {...draft, name: event.currentTarget.value}})}
                />
                <TextArea
                    aria-label={props.intl.formatMessage({id: 'workspace_policies_panel.roots_input', defaultMessage: 'Workspace policy allowed roots'})}
                    value={draft.allowedRootsText}
                    onChange={(event) => props.setDrafts({...props.drafts, [policy.id]: {...draft, allowedRootsText: event.currentTarget.value}})}
                />
                <TextInput
                    aria-label={props.intl.formatMessage({id: 'workspace_policies_panel.default_workspace_input', defaultMessage: 'Workspace policy default workspace'})}
                    value={draft.defaultWorkspacePath}
                    onChange={(event) => props.setDrafts({...props.drafts, [policy.id]: {...draft, defaultWorkspacePath: event.currentTarget.value}})}
                />
                <ModesGroup>
                    <ModeSelects
                        draft={draft}
                        setDraft={(nextDraft) => props.setDrafts({...props.drafts, [policy.id]: nextDraft})}
                        labelPrefix='Workspace policy'
                    />
                </ModesGroup>
                <ActionButton
                    type='button'
                    onClick={() => props.saveDraft(draft)}
                    disabled={props.savingID === policy.id}
                >
                    <FormattedMessage
                        id='workspace_policies_panel.save'
                        defaultMessage='Save'
                    />
                </ActionButton>
            </PolicyRow>
        );
    });
}

function ModeSelects(props: {
    draft: WorkspacePolicyDraft;
    setDraft: (draft: WorkspacePolicyDraft) => void;
    labelPrefix: string;
}) {
    return (
        <>
            <SmallSelect
                aria-label={`${props.labelPrefix} mode`}
                value={props.draft.mode}
                onChange={(event) => props.setDraft({...props.draft, mode: event.currentTarget.value})}
            >
                {workspaceModes.map((mode) => (
                    <option
                        key={mode}
                        value={mode}
                    >
                        {mode}
                    </option>
                ))}
            </SmallSelect>
            <SmallSelect
                aria-label={`${props.labelPrefix} network mode`}
                value={props.draft.networkMode}
                onChange={(event) => props.setDraft({...props.draft, networkMode: event.currentTarget.value})}
            >
                {networkModes.map((mode) => (
                    <option
                        key={mode}
                        value={mode}
                    >
                        {mode}
                    </option>
                ))}
            </SmallSelect>
            <SmallSelect
                aria-label={`${props.labelPrefix} shell mode`}
                value={props.draft.shellMode}
                onChange={(event) => props.setDraft({...props.draft, shellMode: event.currentTarget.value})}
            >
                {shellModes.map((mode) => (
                    <option
                        key={mode}
                        value={mode}
                    >
                        {mode}
                    </option>
                ))}
            </SmallSelect>
        </>
    );
}

function draftFromPolicy(policy: WorkspacePolicy): WorkspacePolicyDraft {
    return {
        id: policy.id,
        name: policy.name,
        allowedRootsText: (policy.allowedRoots || []).join('\n'),
        defaultWorkspacePath: policy.defaultWorkspacePath,
        mode: policy.mode || 'ask_write',
        networkMode: policy.networkMode || 'ask',
        shellMode: policy.shellMode || 'ask',
    };
}

function requestFromDraft(draft: WorkspacePolicyDraft): WorkspacePolicyRequest {
    return {
        name: draft.name.trim(),
        allowedRoots: draft.allowedRootsText.split(/[\n,]/).map((root) => root.trim()).filter(Boolean),
        defaultWorkspacePath: draft.defaultWorkspacePath.trim(),
        mode: draft.mode,
        networkMode: draft.networkMode,
        shellMode: draft.shellMode,
        metadata: {},
    };
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
    grid-template-columns: minmax(150px, 0.8fr) minmax(180px, 1fr) minmax(180px, 1fr) minmax(180px, 1fr) 72px;
    gap: 8px;
    color: rgba(var(--center-channel-color-rgb), 0.64);
    font-size: 12px;
    font-weight: 600;

    @media (max-width: 900px) {
        display: none;
    }
`;

const PolicyRow = styled.div`
    display: grid;
    align-items: start;
    grid-template-columns: minmax(150px, 0.8fr) minmax(180px, 1fr) minmax(180px, 1fr) minmax(180px, 1fr) 72px;
    gap: 8px;
    padding: 8px 0;
    border-top: 1px solid rgba(var(--center-channel-color-rgb), 0.08);

    @media (max-width: 900px) {
        grid-template-columns: 1fr;
    }
`;

const TextInput = styled.input`
    width: 100%;
    min-height: 32px;
    padding: 6px 8px;
    border: 1px solid rgba(var(--center-channel-color-rgb), 0.16);
    border-radius: 4px;
    background: var(--center-channel-bg);
    color: var(--center-channel-color);
`;

const TextArea = styled.textarea`
    width: 100%;
    min-height: 64px;
    padding: 6px 8px;
    border: 1px solid rgba(var(--center-channel-color-rgb), 0.16);
    border-radius: 4px;
    background: var(--center-channel-bg);
    color: var(--center-channel-color);
    resize: vertical;
`;

const SmallSelect = styled.select`
    width: 100%;
    min-height: 32px;
    padding: 4px 8px;
    border: 1px solid rgba(var(--center-channel-color-rgb), 0.16);
    border-radius: 4px;
    background: var(--center-channel-bg);
    color: var(--center-channel-color);
`;

const ModesGroup = styled.div`
    display: grid;
    gap: 6px;
`;

const ActionButton = styled.button`
    min-height: 32px;
    padding: 0 12px;
    border: 0;
    border-radius: 4px;
    background: var(--button-bg);
    color: var(--button-color);
    font-weight: 600;

    &:disabled {
        opacity: 0.56;
    }
`;

const ErrorText = styled.div`
    margin-bottom: 8px;
    color: var(--error-text);
    font-size: 13px;
`;

const EmptyState = styled.div`
    padding: 12px 0;
    color: rgba(var(--center-channel-color-rgb), 0.64);
    font-size: 13px;
`;
