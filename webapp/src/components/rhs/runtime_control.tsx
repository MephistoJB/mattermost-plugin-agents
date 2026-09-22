// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React, {useCallback, useEffect, useMemo, useState} from 'react';
import styled from 'styled-components';

import {
    getRuntimeApprovals,
    getRuntimeSessions,
    getRuntimeTasks,
    getScopedRuntimePolicy,
    getSupervisorRuns,
    getWorkspacePolicies,
    submitRuntimeApproval,
    submitRuntimeSessionAction,
    upsertScopedRuntimePolicy,
} from '@/client';
import type {RuntimeApproval, RuntimePolicy, RuntimeSession, RuntimeSessionAction, RuntimeTask, RuntimeType, SupervisorRun, WorkspacePolicy} from '@/types/runtime';
import {runtimeApprovalSummary} from '@/utils/runtime_approvals';

type Props = {
    channelId?: string;
    selectedPostId?: string;
    conversationId?: string;
};

type Scope = {
    type: 'channel' | 'thread';
    id: string;
};

const runtimeOptions: Array<{label: string; value: RuntimeType}> = [
    {label: 'Inherit', value: 'inherit'},
    {label: 'Codex', value: 'codex'},
    {label: 'OpenAI', value: 'openai'},
    {label: 'Local', value: 'local'},
];

export default function RuntimeControl(props: Props) {
    const scope = useMemo<Scope | null>(() => {
        if (props.selectedPostId) {
            return {type: 'thread', id: props.selectedPostId};
        }
        if (props.channelId) {
            return {type: 'channel', id: props.channelId};
        }
        return null;
    }, [props.channelId, props.selectedPostId]);

    const [policy, setPolicy] = useState<RuntimePolicy | null>(null);
    const [sessions, setSessions] = useState<RuntimeSession[]>([]);
    const [tasks, setTasks] = useState<RuntimeTask[]>([]);
    const [approvals, setApprovals] = useState<RuntimeApproval[]>([]);
    const [supervisorRuns, setSupervisorRuns] = useState<SupervisorRun[]>([]);
    const [workspacePolicies, setWorkspacePolicies] = useState<WorkspacePolicy[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState('');
    const [savingRuntime, setSavingRuntime] = useState<RuntimeType | null>(null);
    const [savingWorkspacePolicy, setSavingWorkspacePolicy] = useState(false);
    const [savingBudget, setSavingBudget] = useState(false);
    const [decidingApproval, setDecidingApproval] = useState<string | null>(null);
    const [actingSession, setActingSession] = useState<string | null>(null);

    const load = useCallback(async () => {
        if (!scope) {
            return;
        }
        setLoading(true);
        setError('');
        try {
            const [nextPolicy, nextSessions, nextTasks, nextApprovals, nextSupervisorRuns] = await Promise.all([
                getScopedRuntimePolicy(scope.type, scope.id),
                getRuntimeSessions({
                    conversationId: props.conversationId,
                    limit: 5,
                }),
                getRuntimeTasks({
                    channelId: props.channelId,
                    rootPostId: props.selectedPostId,
                    limit: 5,
                }),
                getRuntimeApprovals(5),
                getSupervisorRuns({
                    conversationId: props.conversationId,
                    rootTaskId: props.selectedPostId,
                    limit: 5,
                }),
            ]);
            setPolicy(nextPolicy);
            setSessions(nextSessions);
            setTasks(nextTasks);
            setApprovals(nextApprovals);
            setSupervisorRuns(nextSupervisorRuns);
            getWorkspacePolicies().
                then(setWorkspacePolicies).
                catch(() => setWorkspacePolicies([]));
        } catch {
            setError('Runtime controls unavailable.');
        } finally {
            setLoading(false);
        }
    }, [props.channelId, props.conversationId, props.selectedPostId, scope]);

    useEffect(() => {
        load();
    }, [load]);

    const setRuntime = useCallback(async (runtimeType: RuntimeType) => {
        if (!scope) {
            return;
        }
        setSavingRuntime(runtimeType);
        setError('');
        try {
            const nextPolicy = await upsertScopedRuntimePolicy(scope.type, scope.id, {
                runtimeType,
                providerID: providerForRuntime(runtimeType),
                model: policy?.model || '',
                workspacePolicyID: policy?.workspacePolicyID || '',
                approvalPolicyID: policy?.approvalPolicyID || '',
                allowCloud: runtimeType !== 'local',
                allowLocal: runtimeType !== 'openai',
                cloudBudgetCents: policy?.cloudBudgetCents || 0,
                cloudBudgetWindow: policy?.cloudBudgetWindow || '',
                cloudEscalationConfirmed: runtimeType !== 'local',
            });
            setPolicy(nextPolicy);
        } catch {
            setError('Could not save runtime policy.');
        } finally {
            setSavingRuntime(null);
        }
    }, [policy, scope]);

    const setWorkspacePolicy = useCallback(async (workspacePolicyID: string) => {
        if (!scope) {
            return;
        }
        setSavingWorkspacePolicy(true);
        setError('');
        try {
            const runtimeType = policy?.runtimeType || 'inherit';
            const nextPolicy = await upsertScopedRuntimePolicy(scope.type, scope.id, {
                runtimeType,
                providerID: policy?.providerID || providerForRuntime(runtimeType),
                model: policy?.model || '',
                workspacePolicyID,
                approvalPolicyID: policy?.approvalPolicyID || '',
                allowCloud: policy?.allowCloud ?? runtimeType !== 'local',
                allowLocal: policy?.allowLocal ?? runtimeType !== 'openai',
                cloudBudgetCents: policy?.cloudBudgetCents || 0,
                cloudBudgetWindow: policy?.cloudBudgetWindow || '',
                cloudEscalationConfirmed: runtimePolicyCanUseCloud(runtimeType, policy?.allowCloud ?? runtimeType !== 'local'),
            });
            setPolicy(nextPolicy);
        } catch {
            setError('Could not save workspace policy.');
        } finally {
            setSavingWorkspacePolicy(false);
        }
    }, [policy, scope]);

    const setCloudBudget = useCallback(async (rawValue: string) => {
        if (!scope) {
            return;
        }
        const cloudBudgetCents = Math.max(0, Math.round(Number(rawValue) || 0));
        setSavingBudget(true);
        setError('');
        try {
            const runtimeType = policy?.runtimeType || 'inherit';
            const nextPolicy = await upsertScopedRuntimePolicy(scope.type, scope.id, {
                runtimeType,
                providerID: policy?.providerID || providerForRuntime(runtimeType),
                model: policy?.model || '',
                workspacePolicyID: policy?.workspacePolicyID || '',
                approvalPolicyID: policy?.approvalPolicyID || '',
                allowCloud: policy?.allowCloud ?? runtimeType !== 'local',
                allowLocal: policy?.allowLocal ?? runtimeType !== 'openai',
                cloudBudgetCents,
                cloudBudgetWindow: policy?.cloudBudgetWindow || '',
                cloudEscalationConfirmed: runtimePolicyCanUseCloud(runtimeType, policy?.allowCloud ?? runtimeType !== 'local'),
            });
            setPolicy(nextPolicy);
        } catch {
            setError('Could not save cloud budget.');
        } finally {
            setSavingBudget(false);
        }
    }, [policy, scope]);

    const setCloudBudgetWindow = useCallback(async (cloudBudgetWindow: string) => {
        if (!scope) {
            return;
        }
        setSavingBudget(true);
        setError('');
        try {
            const runtimeType = policy?.runtimeType || 'inherit';
            const nextPolicy = await upsertScopedRuntimePolicy(scope.type, scope.id, {
                runtimeType,
                providerID: policy?.providerID || providerForRuntime(runtimeType),
                model: policy?.model || '',
                workspacePolicyID: policy?.workspacePolicyID || '',
                approvalPolicyID: policy?.approvalPolicyID || '',
                allowCloud: policy?.allowCloud ?? runtimeType !== 'local',
                allowLocal: policy?.allowLocal ?? runtimeType !== 'openai',
                cloudBudgetCents: policy?.cloudBudgetCents || 0,
                cloudBudgetWindow,
                cloudEscalationConfirmed: runtimePolicyCanUseCloud(runtimeType, policy?.allowCloud ?? runtimeType !== 'local'),
            });
            setPolicy(nextPolicy);
        } catch {
            setError('Could not save cloud budget window.');
        } finally {
            setSavingBudget(false);
        }
    }, [policy, scope]);

    const decideApproval = useCallback(async (approvalId: string, decision: 'accept' | 'deny') => {
        setDecidingApproval(approvalId);
        setError('');
        try {
            await submitRuntimeApproval(approvalId, decision);
            setApprovals((current) => current.filter((approval) => approval.id !== approvalId));
        } catch {
            setError('Could not submit approval decision.');
        } finally {
            setDecidingApproval(null);
        }
    }, []);

    const submitSessionAction = useCallback(async (sessionID: string, action: RuntimeSessionAction) => {
        setActingSession(sessionID);
        setError('');
        try {
            await submitRuntimeSessionAction(sessionID, {action});
            await load();
        } catch {
            setError(`Could not ${action} runtime session.`);
        } finally {
            setActingSession(null);
        }
    }, [load]);

    if (!scope) {
        return null;
    }

    const currentRuntime = policy?.runtimeType || 'inherit';

    return (
        <Panel data-testid='runtime-control-panel'>
            <HeaderRow>
                <Title>{'Runtime'}</Title>
                <ScopeLabel>{scope.type}</ScopeLabel>
                <IconButton
                    type='button'
                    aria-label='Refresh runtime controls'
                    onClick={load}
                    disabled={loading}
                >
                    <i className='icon-refresh'/>
                </IconButton>
            </HeaderRow>
            <RuntimeSegments>
                {runtimeOptions.map((option) => (
                    <SegmentButton
                        key={option.value}
                        type='button'
                        $active={currentRuntime === option.value}
                        disabled={savingRuntime !== null}
                        onClick={() => setRuntime(option.value)}
                    >
                        {savingRuntime === option.value ? '...' : option.label}
                    </SegmentButton>
                ))}
            </RuntimeSegments>
            {policy?.model && <MetaLine>{`Model: ${policy.model}`}</MetaLine>}
            {workspacePolicies.length > 0 ? (
                <Select
                    aria-label='Workspace policy'
                    value={policy?.workspacePolicyID || ''}
                    disabled={savingWorkspacePolicy}
                    onChange={(event) => setWorkspacePolicy(event.currentTarget.value)}
                >
                    <option value=''>{'Inherited workspace'}</option>
                    {workspacePolicies.map((workspacePolicy) => (
                        <option
                            key={workspacePolicy.id}
                            value={workspacePolicy.id}
                        >
                            {workspacePolicy.name || workspacePolicy.id}
                        </option>
                    ))}
                </Select>
            ) : policy?.workspacePolicyID && (
                <MetaLine>{`Workspace policy: ${policy.workspacePolicyID}`}</MetaLine>
            )}
            <BudgetRow>
                <BudgetLabel htmlFor='runtime-cloud-budget'>{'Cloud budget'}</BudgetLabel>
                <BudgetInput
                    id='runtime-cloud-budget'
                    aria-label='Cloud budget cents'
                    type='number'
                    inputMode='numeric'
                    min={0}
                    step={1}
                    value={policy?.cloudBudgetCents || 0}
                    disabled={savingBudget}
                    onChange={(event) => setCloudBudget(event.currentTarget.value)}
                />
                <BudgetUnit>{'cents'}</BudgetUnit>
                <BudgetWindowSelect
                    aria-label='Cloud budget window'
                    value={policy?.cloudBudgetWindow || ''}
                    disabled={savingBudget}
                    onChange={(event) => setCloudBudgetWindow(event.currentTarget.value)}
                >
                    <option value=''>{'none'}</option>
                    <option value='daily'>{'daily'}</option>
                    <option value='weekly'>{'weekly'}</option>
                    <option value='monthly'>{'monthly'}</option>
                </BudgetWindowSelect>
            </BudgetRow>
            {error && <ErrorLine>{error}</ErrorLine>}
            <Lists>
                <SummaryList
                    title='Supervisor'
                    empty='No supervisor runs'
                    items={supervisorRuns.map((run) => ({
                        id: run.id,
                        primary: `${run.status} / ${run.subagents?.length || 0} subagents`,
                        secondary: supervisorSecondary(run),
                    }))}
                />
                <SummaryList
                    title='Sessions'
                    empty='No recent sessions'
                    items={sessions.map((session) => ({
                        id: session.id,
                        primary: `${session.runtimeType} / ${session.status}`,
                        secondary: sessionSecondary(session),
                        actions: (
                            <SessionActions
                                session={session}
                                acting={actingSession === session.id}
                                onAction={submitSessionAction}
                            />
                        ),
                    }))}
                />
                <SummaryList
                    title='Tasks'
                    empty='No tasks'
                    items={tasks.map((task) => ({
                        id: task.id,
                        primary: `${task.taskType} / ${task.status}`,
                        secondary: task.title || task.prompt,
                    }))}
                />
                <ApprovalsList
                    approvals={approvals}
                    decidingApproval={decidingApproval}
                    onDecision={decideApproval}
                />
            </Lists>
        </Panel>
    );
}

function providerForRuntime(runtimeType: RuntimeType): string {
    switch (runtimeType) {
    case 'codex':
        return 'codex';
    case 'openai':
        return 'openai';
    case 'local':
        return 'local';
    default:
        return '';
    }
}

function runtimePolicyCanUseCloud(runtimeType: RuntimeType, allowCloud: boolean): boolean {
    return runtimeType === 'codex' || runtimeType === 'openai' || allowCloud;
}

function supervisorSecondary(run: SupervisorRun): string {
    const subagents = run.subagents || [];
    if (subagents.length === 0) {
        return run.objective || run.id;
    }
    return subagents.map((subagent) => `${subagent.role || subagent.title}: ${subagent.status}`).join(', ');
}

function sessionSecondary(session: RuntimeSession): string {
    const details = [
        session.model || session.providerID || session.id,
        session.externalSessionID ? `external ${session.externalSessionID}` : '',
        session.workspacePath ? `workspace ${session.workspacePath}` : '',
        session.lastError ? `error ${session.lastError}` : '',
    ].filter(Boolean);
    return details.join(' / ');
}

function SummaryList(props: {title: string; empty: string; items: Array<{id: string; primary: string; secondary: string; actions?: React.ReactNode}>}) {
    return (
        <ListSection>
            <ListTitle>{props.title}</ListTitle>
            {props.items.length === 0 ? (
                <EmptyLine>{props.empty}</EmptyLine>
            ) : props.items.map((item) => (
                <ListItem key={item.id}>
                    <ItemText>
                        <ItemPrimary>{item.primary}</ItemPrimary>
                        <ItemSecondary>{item.secondary}</ItemSecondary>
                    </ItemText>
                    {item.actions}
                </ListItem>
            ))}
        </ListSection>
    );
}

function SessionActions(props: {session: RuntimeSession; acting: boolean; onAction: (sessionID: string, action: RuntimeSessionAction) => void}) {
    const canStop = props.session.status === 'running' || props.session.status === 'waiting_approval';
    const canResume = props.session.status === 'idle' || props.session.status === 'failed' || props.session.status === 'cancelled' || props.session.status === 'completed';
    if (!canStop && !canResume) {
        return null;
    }
    return (
        <ItemActions>
            {canResume && (
                <SmallButton
                    type='button'
                    aria-label={`Resume session ${props.session.id}`}
                    disabled={props.acting}
                    onClick={() => props.onAction(props.session.id, 'resume')}
                >
                    {'Resume'}
                </SmallButton>
            )}
            {canStop && (
                <SmallButton
                    type='button'
                    aria-label={`Stop session ${props.session.id}`}
                    disabled={props.acting}
                    onClick={() => props.onAction(props.session.id, 'stop')}
                >
                    {'Stop'}
                </SmallButton>
            )}
        </ItemActions>
    );
}

function ApprovalsList(props: {
    approvals: RuntimeApproval[];
    decidingApproval: string | null;
    onDecision: (approvalId: string, decision: 'accept' | 'deny') => void;
}) {
    return (
        <ListSection>
            <ListTitle>{'Approvals'}</ListTitle>
            {props.approvals.length === 0 ? (
                <EmptyLine>{'No pending approvals'}</EmptyLine>
            ) : props.approvals.map((approval) => (
                <ApprovalItem key={approval.id}>
                    <ApprovalText>
                        <ItemPrimary>{runtimeApprovalSummary(approval)}</ItemPrimary>
                        <ItemSecondary>{approval.id}</ItemSecondary>
                        {(approval.externalApprovalID || approval.runtimeSessionID) && (
                            <ItemSecondary>{approval.externalApprovalID || approval.runtimeSessionID}</ItemSecondary>
                        )}
                    </ApprovalText>
                    <ApprovalActions>
                        <SmallButton
                            type='button'
                            disabled={props.decidingApproval === approval.id}
                            onClick={() => props.onDecision(approval.id, 'accept')}
                        >
                            {'Accept'}
                        </SmallButton>
                        <SmallButton
                            type='button'
                            disabled={props.decidingApproval === approval.id}
                            onClick={() => props.onDecision(approval.id, 'deny')}
                        >
                            {'Deny'}
                        </SmallButton>
                    </ApprovalActions>
                </ApprovalItem>
            ))}
        </ListSection>
    );
}

const Panel = styled.div`
    border-bottom: 1px solid rgba(var(--center-channel-color-rgb), 0.12);
    padding: 10px 12px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    background: rgba(var(--center-channel-color-rgb), 0.03);
`;

const HeaderRow = styled.div`
    display: flex;
    align-items: center;
    gap: 8px;
`;

const Title = styled.div`
    font-size: 12px;
    font-weight: 700;
`;

const ScopeLabel = styled.div`
    color: rgba(var(--center-channel-color-rgb), 0.64);
    font-size: 11px;
    margin-right: auto;
    text-transform: uppercase;
`;

const IconButton = styled.button`
    border: 0;
    background: transparent;
    color: rgba(var(--center-channel-color-rgb), 0.72);
    height: 24px;
    width: 24px;
    border-radius: 4px;

    &:hover {
        background: rgba(var(--center-channel-color-rgb), 0.08);
        color: rgb(var(--center-channel-color-rgb));
    }
`;

const RuntimeSegments = styled.div`
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 4px;
`;

const SegmentButton = styled.button<{$active: boolean}>`
    border: 1px solid ${(props) => (props.$active ? 'rgba(var(--button-bg-rgb), 0.8)' : 'rgba(var(--center-channel-color-rgb), 0.16)')};
    background: ${(props) => (props.$active ? 'rgba(var(--button-bg-rgb), 0.12)' : 'rgb(var(--center-channel-bg-rgb))')};
    color: ${(props) => (props.$active ? 'rgb(var(--button-bg-rgb))' : 'rgb(var(--center-channel-color-rgb))')};
    border-radius: 4px;
    font-size: 11px;
    font-weight: 600;
    height: 28px;
    min-width: 0;
`;

const Select = styled.select`
    border: 1px solid rgba(var(--center-channel-color-rgb), 0.16);
    background: rgb(var(--center-channel-bg-rgb));
    color: rgb(var(--center-channel-color-rgb));
    border-radius: 4px;
    font-size: 11px;
    height: 28px;
    min-width: 0;
    padding: 0 8px;
`;

const BudgetRow = styled.div`
    display: grid;
    grid-template-columns: auto minmax(64px, 96px) auto minmax(74px, 88px);
    align-items: center;
    gap: 6px;
`;

const BudgetLabel = styled.label`
    color: rgba(var(--center-channel-color-rgb), 0.72);
    font-size: 11px;
    font-weight: 600;
`;

const BudgetInput = styled.input`
    border: 1px solid rgba(var(--center-channel-color-rgb), 0.16);
    background: rgb(var(--center-channel-bg-rgb));
    color: rgb(var(--center-channel-color-rgb));
    border-radius: 4px;
    font-size: 11px;
    height: 28px;
    min-width: 0;
    padding: 0 8px;
`;

const BudgetUnit = styled.span`
    color: rgba(var(--center-channel-color-rgb), 0.56);
    font-size: 11px;
`;

const BudgetWindowSelect = styled.select`
    border: 1px solid rgba(var(--center-channel-color-rgb), 0.16);
    background: rgb(var(--center-channel-bg-rgb));
    color: rgb(var(--center-channel-color-rgb));
    border-radius: 4px;
    font-size: 11px;
    height: 28px;
    min-width: 0;
`;

const MetaLine = styled.div`
    color: rgba(var(--center-channel-color-rgb), 0.72);
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
`;

const ErrorLine = styled.div`
    color: var(--error-text);
    font-size: 11px;
`;

const Lists = styled.div`
    display: grid;
    grid-template-columns: 1fr;
    gap: 8px;
`;

const ListSection = styled.div`
    display: flex;
    flex-direction: column;
    gap: 4px;
`;

const ListTitle = styled.div`
    color: rgba(var(--center-channel-color-rgb), 0.64);
    font-size: 11px;
    font-weight: 700;
`;

const EmptyLine = styled.div`
    color: rgba(var(--center-channel-color-rgb), 0.56);
    font-size: 11px;
`;

const ListItem = styled.div`
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 8px;
    min-width: 0;
`;

const ItemText = styled.div`
    min-width: 0;
`;

const ItemPrimary = styled.div`
    font-size: 11px;
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
`;

const ItemSecondary = styled.div`
    color: rgba(var(--center-channel-color-rgb), 0.64);
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
`;

const ApprovalItem = styled.div`
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 8px;
`;

const ApprovalText = styled.div`
    min-width: 0;
`;

const ApprovalActions = styled.div`
    display: flex;
    gap: 4px;
`;

const ItemActions = styled.div`
    display: flex;
    gap: 4px;
`;

const SmallButton = styled.button`
    border: 1px solid rgba(var(--center-channel-color-rgb), 0.16);
    background: rgb(var(--center-channel-bg-rgb));
    border-radius: 4px;
    color: rgb(var(--center-channel-color-rgb));
    font-size: 11px;
    font-weight: 600;
    height: 26px;
    padding: 0 8px;

    &:hover {
        background: rgba(var(--center-channel-color-rgb), 0.08);
    }
`;
