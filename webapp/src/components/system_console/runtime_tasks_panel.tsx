// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React, {useCallback, useEffect, useState} from 'react';
import styled from 'styled-components';
import {RefreshIcon, TrashCanOutlineIcon} from '@mattermost/compass-icons/components';
import {FormattedMessage, useIntl} from 'react-intl';

import {getAdminRuntimeTaskRuns, getAdminRuntimeTasks, submitAdminRuntimeTaskAction} from '@/client';
import type {RuntimeTask, RuntimeTaskAction, RuntimeTaskRun} from '@/types/runtime';

import {ButtonIcon} from '../assets/buttons';

import Panel from './panel';

const ACTIVE_TASK_LIMIT = 50;
const SNOOZE_MS = 60 * 60 * 1000;
const TASK_STATUS_OPTIONS = ['', 'queued', 'running', 'waiting_approval', 'paused', 'completed', 'failed', 'cancelled'];

export default function RuntimeTasksPanel() {
    const intl = useIntl();
    const [tasks, setTasks] = useState<RuntimeTask[]>([]);
    const [statusFilter, setStatusFilter] = useState('');
    const [channelFilter, setChannelFilter] = useState('');
    const [rootPostFilter, setRootPostFilter] = useState('');
    const [loading, setLoading] = useState(true);
    const [actionTaskID, setActionTaskID] = useState('');
    const [selectedTaskID, setSelectedTaskID] = useState('');
    const [taskRuns, setTaskRuns] = useState<RuntimeTaskRun[]>([]);
    const [runsLoading, setRunsLoading] = useState(false);
    const [error, setError] = useState('');

    const loadTasks = useCallback(async () => {
        setLoading(true);
        try {
            const params = {limit: ACTIVE_TASK_LIMIT} as {
                channelId?: string;
                rootPostId?: string;
                status?: string;
                limit: number;
            };
            if (channelFilter.trim()) {
                params.channelId = channelFilter.trim();
            }
            if (rootPostFilter.trim()) {
                params.rootPostId = rootPostFilter.trim();
            }
            if (statusFilter) {
                params.status = statusFilter;
            }
            const nextTasks = await getAdminRuntimeTasks(params);
            setTasks(nextTasks);
            setError('');
        } catch {
            setError(intl.formatMessage({id: 'runtime_tasks_panel.load_error', defaultMessage: 'Failed to load runtime tasks.'}));
        } finally {
            setLoading(false);
        }
    }, [channelFilter, intl, rootPostFilter, statusFilter]);

    useEffect(() => {
        loadTasks();
    }, [loadTasks]);

    const submitAction = useCallback(async (taskID: string, action: RuntimeTaskAction) => {
        setActionTaskID(`${taskID}:${action}`);
        try {
            await submitAdminRuntimeTaskAction(taskID, action === 'snooze' ? {action, snoozeMs: SNOOZE_MS} : {action});
            await loadTasks();
        } catch {
            setError(intl.formatMessage({id: 'runtime_tasks_panel.action_error', defaultMessage: 'Failed to update runtime task.'}));
        } finally {
            setActionTaskID('');
        }
    }, [intl, loadTasks]);

    const loadTaskRuns = useCallback(async (taskID: string) => {
        if (selectedTaskID === taskID) {
            setSelectedTaskID('');
            setTaskRuns([]);
            return;
        }
        setSelectedTaskID(taskID);
        setRunsLoading(true);
        try {
            const runs = await getAdminRuntimeTaskRuns(taskID);
            setTaskRuns(runs);
            setError('');
        } catch {
            setError(intl.formatMessage({id: 'runtime_tasks_panel.runs_error', defaultMessage: 'Failed to load task run history.'}));
        } finally {
            setRunsLoading(false);
        }
    }, [intl, selectedTaskID]);

    return (
        <Panel
            title={intl.formatMessage({id: 'runtime_tasks_panel.title', defaultMessage: 'Runtime Tasks'})}
            subtitle={intl.formatMessage({id: 'runtime_tasks_panel.subtitle', defaultMessage: 'Review and control autonomous tasks, reminders, and follow-up watchers.'})}
        >
            <HeaderRow>
                <TaskCount>
                    <FormattedMessage
                        id='runtime_tasks_panel.task_count'
                        defaultMessage='{count, plural, one {# task} other {# tasks}}'
                        values={{count: tasks.length}}
                    />
                </TaskCount>
                <ButtonIcon
                    type='button'
                    aria-label={intl.formatMessage({id: 'runtime_tasks_panel.refresh', defaultMessage: 'Refresh runtime tasks'})}
                    onClick={loadTasks}
                    disabled={loading}
                >
                    <RefreshIcon size={18}/>
                </ButtonIcon>
            </HeaderRow>
            <FilterRow>
                <FilterSelect
                    aria-label={intl.formatMessage({id: 'runtime_tasks_panel.status_filter', defaultMessage: 'Task status filter'})}
                    value={statusFilter}
                    onChange={(e) => setStatusFilter(e.target.value)}
                >
                    {TASK_STATUS_OPTIONS.map((status) => (
                        <option
                            key={status || 'all'}
                            value={status}
                        >
                            {status || intl.formatMessage({id: 'runtime_tasks_panel.all_statuses', defaultMessage: 'All statuses'})}
                        </option>
                    ))}
                </FilterSelect>
                <FilterInput
                    aria-label={intl.formatMessage({id: 'runtime_tasks_panel.channel_filter', defaultMessage: 'Channel ID filter'})}
                    value={channelFilter}
                    placeholder={intl.formatMessage({id: 'runtime_tasks_panel.channel_filter_placeholder', defaultMessage: 'Channel ID'})}
                    onChange={(e) => setChannelFilter(e.target.value)}
                />
                <FilterInput
                    aria-label={intl.formatMessage({id: 'runtime_tasks_panel.root_post_filter', defaultMessage: 'Thread root post ID filter'})}
                    value={rootPostFilter}
                    placeholder={intl.formatMessage({id: 'runtime_tasks_panel.root_post_filter_placeholder', defaultMessage: 'Thread root post ID'})}
                    onChange={(e) => setRootPostFilter(e.target.value)}
                />
                <ActionButton
                    type='button'
                    onClick={loadTasks}
                    disabled={loading}
                >
                    <FormattedMessage
                        id='runtime_tasks_panel.apply_filters'
                        defaultMessage='Apply'
                    />
                </ActionButton>
            </FilterRow>
            {error && <ErrorText>{error}</ErrorText>}
            {renderTaskContent({tasks, loading, actionTaskID, selectedTaskID, taskRuns, runsLoading, intl, submitAction, loadTaskRuns})}
        </Panel>
    );
}

function renderTaskContent(props: {
    tasks: RuntimeTask[];
    loading: boolean;
    actionTaskID: string;
    selectedTaskID: string;
    taskRuns: RuntimeTaskRun[];
    runsLoading: boolean;
    intl: ReturnType<typeof useIntl>;
    submitAction: (taskID: string, action: RuntimeTaskAction) => void;
    loadTaskRuns: (taskID: string) => void;
}) {
    if (props.loading && props.tasks.length === 0) {
        return (
            <EmptyState>
                <FormattedMessage
                    id='runtime_tasks_panel.loading'
                    defaultMessage='Loading tasks...'
                />
            </EmptyState>
        );
    }

    if (props.tasks.length === 0) {
        return (
            <EmptyState>
                <FormattedMessage
                    id='runtime_tasks_panel.empty'
                    defaultMessage='No runtime tasks found.'
                />
            </EmptyState>
        );
    }

    return (
        <TaskList>
            {props.tasks.map((task) => (
                <TaskItem key={task.id}>
                    <TaskRow>
                        <TaskMain>
                            <TaskTitle>{task.title || task.prompt || task.id}</TaskTitle>
                            <TaskMeta>
                                <StatusPill $status={task.status}>{task.status}</StatusPill>
                                <span>{task.taskType || 'task'}</span>
                                {task.channelID && <span>{shortID(task.channelID)}</span>}
                                {task.rootPostID && <span>{shortID(task.rootPostID)}</span>}
                                {task.nextRunAt > 0 && <span>{formatMillis(task.nextRunAt)}</span>}
                            </TaskMeta>
                            {task.lastRun && (
                                <TaskRunMeta>
                                    <FormattedMessage
                                        id='runtime_tasks_panel.last_run'
                                        defaultMessage='Last run: {status}'
                                        values={{status: task.lastRun.status}}
                                    />
                                    {task.lastRun.resultPostID && <span>{shortID(task.lastRun.resultPostID)}</span>}
                                    {task.lastRun.error && <TaskRunError>{task.lastRun.error}</TaskRunError>}
                                </TaskRunMeta>
                            )}
                        </TaskMain>
                        <TaskActions>
                            <ActionButton
                                type='button'
                                onClick={() => props.loadTaskRuns(task.id)}
                                disabled={props.runsLoading && props.selectedTaskID === task.id}
                            >
                                <FormattedMessage
                                    id='runtime_tasks_panel.runs'
                                    defaultMessage='Runs'
                                />
                            </ActionButton>
                            <ActionButton
                                type='button'
                                onClick={() => props.submitAction(task.id, 'run')}
                                disabled={isActionDisabled(props.actionTaskID, task.id, 'run')}
                            >
                                <FormattedMessage
                                    id='runtime_tasks_panel.run'
                                    defaultMessage='Run'
                                />
                            </ActionButton>
                            {task.status === 'paused' ? (
                                <ActionButton
                                    type='button'
                                    onClick={() => props.submitAction(task.id, 'resume')}
                                    disabled={isActionDisabled(props.actionTaskID, task.id, 'resume')}
                                >
                                    <FormattedMessage
                                        id='runtime_tasks_panel.resume'
                                        defaultMessage='Resume'
                                    />
                                </ActionButton>
                            ) : (
                                <ActionButton
                                    type='button'
                                    onClick={() => props.submitAction(task.id, 'pause')}
                                    disabled={isActionDisabled(props.actionTaskID, task.id, 'pause')}
                                >
                                    <FormattedMessage
                                        id='runtime_tasks_panel.pause'
                                        defaultMessage='Pause'
                                    />
                                </ActionButton>
                            )}
                            <ActionButton
                                type='button'
                                onClick={() => props.submitAction(task.id, 'snooze')}
                                disabled={isActionDisabled(props.actionTaskID, task.id, 'snooze')}
                            >
                                <FormattedMessage
                                    id='runtime_tasks_panel.snooze'
                                    defaultMessage='Snooze'
                                />
                            </ActionButton>
                            <ActionButton
                                type='button'
                                onClick={() => props.submitAction(task.id, 'stop')}
                                disabled={isActionDisabled(props.actionTaskID, task.id, 'stop')}
                            >
                                <FormattedMessage
                                    id='runtime_tasks_panel.stop'
                                    defaultMessage='Stop'
                                />
                            </ActionButton>
                            <DeleteButton
                                type='button'
                                aria-label={props.intl.formatMessage({id: 'runtime_tasks_panel.delete', defaultMessage: 'Delete runtime task'})}
                                onClick={() => props.submitAction(task.id, 'delete')}
                                disabled={isActionDisabled(props.actionTaskID, task.id, 'delete')}
                            >
                                <TrashCanOutlineIcon size={16}/>
                            </DeleteButton>
                        </TaskActions>
                    </TaskRow>
                    {props.selectedTaskID === task.id && (
                        <TaskRunsPanel>
                            {props.runsLoading ? (
                                <EmptyState>
                                    <FormattedMessage
                                        id='runtime_tasks_panel.runs_loading'
                                        defaultMessage='Loading task runs...'
                                    />
                                </EmptyState>
                            ) : renderTaskRuns(props.taskRuns)}
                        </TaskRunsPanel>
                    )}
                </TaskItem>
            ))}
        </TaskList>
    );
}

function renderTaskRuns(runs: RuntimeTaskRun[]) {
    if (runs.length === 0) {
        return (
            <EmptyState>
                <FormattedMessage
                    id='runtime_tasks_panel.no_runs'
                    defaultMessage='No task runs found.'
                />
            </EmptyState>
        );
    }

    return (
        <TaskRunList>
            {runs.map((run) => (
                <TaskRunRow key={run.id}>
                    <StatusPill $status={run.status}>{run.status}</StatusPill>
                    <span>{run.startedAt > 0 ? formatMillis(run.startedAt) : 'not started'}</span>
                    {run.runtimeSessionID && <span>{shortID(run.runtimeSessionID)}</span>}
                    {run.resultPostID && <span>{shortID(run.resultPostID)}</span>}
                    {run.error && <TaskRunError>{run.error}</TaskRunError>}
                </TaskRunRow>
            ))}
        </TaskRunList>
    );
}

function shortID(id: string) {
    return id.length > 8 ? id.slice(0, 8) : id;
}

function formatMillis(value: number) {
    return new Date(value).toLocaleString();
}

function isActionDisabled(actionTaskID: string, taskID: string, action: RuntimeTaskAction) {
    return actionTaskID === `${taskID}:${action}`;
}

const HeaderRow = styled.div`
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-bottom: 12px;
`;

const FilterRow = styled.div`
    display: grid;
    grid-template-columns: minmax(120px, 160px) minmax(120px, 1fr) minmax(120px, 1fr) auto;
    gap: 8px;
    margin-bottom: 12px;

    @media screen and (max-width: 700px) {
        grid-template-columns: 1fr;
    }
`;

const FilterInput = styled.input`
    min-height: 32px;
    padding: 4px 8px;
    border: 1px solid rgba(var(--center-channel-color-rgb), 0.16);
    border-radius: 4px;
    background: var(--center-channel-bg);
    color: rgba(var(--center-channel-color-rgb), 0.88);
    font-size: 12px;
`;

const FilterSelect = styled.select`
    min-height: 32px;
    padding: 4px 8px;
    border: 1px solid rgba(var(--center-channel-color-rgb), 0.16);
    border-radius: 4px;
    background: var(--center-channel-bg);
    color: rgba(var(--center-channel-color-rgb), 0.88);
    font-size: 12px;
`;

const TaskCount = styled.div`
    color: rgba(var(--center-channel-color-rgb), 0.72);
    font-size: 13px;
`;

const TaskList = styled.div`
    display: flex;
    flex-direction: column;
    gap: 8px;
`;

const TaskItem = styled.div`
    display: flex;
    flex-direction: column;
    border: 1px solid rgba(var(--center-channel-color-rgb), 0.12);
    border-radius: 4px;
    background: rgba(var(--center-channel-color-rgb), 0.03);
`;

const TaskRow = styled.div`
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 12px;
    align-items: center;
    padding: 10px 12px;

    @media screen and (max-width: 900px) {
        grid-template-columns: 1fr;
    }
`;

const TaskMain = styled.div`
    min-width: 0;
`;

const TaskTitle = styled.div`
    overflow: hidden;
    color: rgba(var(--center-channel-color-rgb), 0.96);
    font-size: 14px;
    font-weight: 600;
    line-height: 20px;
    text-overflow: ellipsis;
    white-space: nowrap;
`;

const TaskMeta = styled.div`
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    align-items: center;
    margin-top: 4px;
    color: rgba(var(--center-channel-color-rgb), 0.64);
    font-size: 12px;
`;

const TaskRunMeta = styled.div`
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    align-items: center;
    margin-top: 4px;
    color: rgba(var(--center-channel-color-rgb), 0.64);
    font-size: 12px;
`;

const TaskRunError = styled.span`
    overflow: hidden;
    max-width: 360px;
    color: var(--error-text);
    text-overflow: ellipsis;
    white-space: nowrap;
`;

const StatusPill = styled.span<{$status: string}>`
    display: inline-flex;
    align-items: center;
    min-height: 20px;
    padding: 1px 6px;
    border-radius: 4px;
    background: ${({$status}) => ($status === 'failed' || $status === 'cancelled' ? 'rgba(var(--error-text-color-rgb), 0.12)' : 'rgba(var(--button-bg-rgb), 0.12)')};
    color: ${({$status}) => ($status === 'failed' || $status === 'cancelled' ? 'var(--error-text)' : 'var(--button-bg)')};
    font-size: 11px;
    font-weight: 600;
`;

const TaskActions = styled.div`
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 6px;
`;

const TaskRunsPanel = styled.div`
    padding: 0 12px 10px;
`;

const TaskRunList = styled.div`
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding-top: 8px;
    border-top: 1px solid rgba(var(--center-channel-color-rgb), 0.08);
`;

const TaskRunRow = styled.div`
    display: grid;
    grid-template-columns: 88px minmax(150px, 1fr) repeat(2, minmax(72px, auto)) minmax(0, 1fr);
    gap: 8px;
    align-items: center;
    color: rgba(var(--center-channel-color-rgb), 0.72);
    font-size: 12px;

    @media screen and (max-width: 900px) {
        grid-template-columns: 1fr;
    }
`;

const ActionButton = styled.button`
    min-width: 56px;
    min-height: 28px;
    padding: 3px 8px;
    border: 1px solid rgba(var(--center-channel-color-rgb), 0.16);
    border-radius: 4px;
    background: var(--center-channel-bg);
    color: rgba(var(--center-channel-color-rgb), 0.88);
    font-size: 12px;
    font-weight: 600;

    &:disabled {
        cursor: default;
        opacity: 0.56;
    }
`;

const DeleteButton = styled(ButtonIcon)`
    width: 30px;
    height: 28px;
`;

const EmptyState = styled.div`
    padding: 18px 12px;
    border: 1px dashed rgba(var(--center-channel-color-rgb), 0.16);
    border-radius: 4px;
    color: rgba(var(--center-channel-color-rgb), 0.64);
    font-size: 13px;
    text-align: center;
`;

const ErrorText = styled.div`
    margin-bottom: 12px;
    color: var(--error-text);
    font-size: 13px;
`;
