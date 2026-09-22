// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React, {useCallback, useEffect, useState} from 'react';
import styled from 'styled-components';
import {RefreshIcon} from '@mattermost/compass-icons/components';
import {FormattedMessage, useIntl} from 'react-intl';

import {getAdminRuntimeSessions, submitAdminRuntimeSessionAction} from '@/client';
import type {RuntimeSession, RuntimeSessionAction} from '@/types/runtime';

import {ButtonIcon} from '../assets/buttons';

import Panel from './panel';

const SESSION_LIMIT = 50;
const SESSION_STATUS_OPTIONS = ['', 'idle', 'running', 'waiting_approval', 'completed', 'failed', 'cancelled'];

export default function RuntimeSessionsPanel() {
    const intl = useIntl();
    const [sessions, setSessions] = useState<RuntimeSession[]>([]);
    const [statusFilter, setStatusFilter] = useState('');
    const [channelFilter, setChannelFilter] = useState('');
    const [rootPostFilter, setRootPostFilter] = useState('');
    const [userFilter, setUserFilter] = useState('');
    const [agentFilter, setAgentFilter] = useState('');
    const [loading, setLoading] = useState(true);
    const [actionSessionID, setActionSessionID] = useState('');
    const [error, setError] = useState('');

    const loadSessions = useCallback(async () => {
        setLoading(true);
        try {
            const params = {limit: SESSION_LIMIT} as {
                channelId?: string;
                rootPostId?: string;
                userId?: string;
                agentId?: string;
                status?: string;
                limit: number;
            };
            if (channelFilter.trim()) {
                params.channelId = channelFilter.trim();
            }
            if (rootPostFilter.trim()) {
                params.rootPostId = rootPostFilter.trim();
            }
            if (userFilter.trim()) {
                params.userId = userFilter.trim();
            }
            if (agentFilter.trim()) {
                params.agentId = agentFilter.trim();
            }
            if (statusFilter) {
                params.status = statusFilter;
            }
            setSessions(await getAdminRuntimeSessions(params));
            setError('');
        } catch {
            setError(intl.formatMessage({id: 'runtime_sessions_panel.load_error', defaultMessage: 'Failed to load runtime sessions.'}));
        } finally {
            setLoading(false);
        }
    }, [agentFilter, channelFilter, intl, rootPostFilter, statusFilter, userFilter]);

    useEffect(() => {
        loadSessions();
    }, [loadSessions]);

    const submitAction = useCallback(async (sessionID: string, action: RuntimeSessionAction) => {
        setActionSessionID(`${sessionID}:${action}`);
        try {
            await submitAdminRuntimeSessionAction(sessionID, {action});
            await loadSessions();
        } catch {
            setError(intl.formatMessage({id: 'runtime_sessions_panel.action_error', defaultMessage: 'Failed to update runtime session.'}));
        } finally {
            setActionSessionID('');
        }
    }, [intl, loadSessions]);

    return (
        <Panel
            title={intl.formatMessage({id: 'runtime_sessions_panel.title', defaultMessage: 'Runtime Sessions'})}
            subtitle={intl.formatMessage({id: 'runtime_sessions_panel.subtitle', defaultMessage: 'Inspect and control active, failed, and resumable Codex/local sessions.'})}
        >
            <HeaderRow>
                <SessionCount>
                    <FormattedMessage
                        id='runtime_sessions_panel.session_count'
                        defaultMessage='{count, plural, one {# session} other {# sessions}}'
                        values={{count: sessions.length}}
                    />
                </SessionCount>
                <ButtonIcon
                    type='button'
                    aria-label={intl.formatMessage({id: 'runtime_sessions_panel.refresh', defaultMessage: 'Refresh runtime sessions'})}
                    onClick={loadSessions}
                    disabled={loading}
                >
                    <RefreshIcon size={18}/>
                </ButtonIcon>
            </HeaderRow>
            <FilterRow>
                <FilterSelect
                    aria-label={intl.formatMessage({id: 'runtime_sessions_panel.status_filter', defaultMessage: 'Session status filter'})}
                    value={statusFilter}
                    onChange={(event) => setStatusFilter(event.target.value)}
                >
                    {SESSION_STATUS_OPTIONS.map((status) => (
                        <option
                            key={status || 'all'}
                            value={status}
                        >
                            {status || intl.formatMessage({id: 'runtime_sessions_panel.all_statuses', defaultMessage: 'All statuses'})}
                        </option>
                    ))}
                </FilterSelect>
                <FilterInput
                    aria-label={intl.formatMessage({id: 'runtime_sessions_panel.channel_filter', defaultMessage: 'Channel ID filter'})}
                    value={channelFilter}
                    placeholder={intl.formatMessage({id: 'runtime_sessions_panel.channel_filter_placeholder', defaultMessage: 'Channel ID'})}
                    onChange={(event) => setChannelFilter(event.target.value)}
                />
                <FilterInput
                    aria-label={intl.formatMessage({id: 'runtime_sessions_panel.root_post_filter', defaultMessage: 'Thread root post ID filter'})}
                    value={rootPostFilter}
                    placeholder={intl.formatMessage({id: 'runtime_sessions_panel.root_post_filter_placeholder', defaultMessage: 'Thread root post ID'})}
                    onChange={(event) => setRootPostFilter(event.target.value)}
                />
                <FilterInput
                    aria-label={intl.formatMessage({id: 'runtime_sessions_panel.user_filter', defaultMessage: 'User ID filter'})}
                    value={userFilter}
                    placeholder={intl.formatMessage({id: 'runtime_sessions_panel.user_filter_placeholder', defaultMessage: 'User ID'})}
                    onChange={(event) => setUserFilter(event.target.value)}
                />
                <FilterInput
                    aria-label={intl.formatMessage({id: 'runtime_sessions_panel.agent_filter', defaultMessage: 'Agent ID filter'})}
                    value={agentFilter}
                    placeholder={intl.formatMessage({id: 'runtime_sessions_panel.agent_filter_placeholder', defaultMessage: 'Agent ID'})}
                    onChange={(event) => setAgentFilter(event.target.value)}
                />
                <ActionButton
                    type='button'
                    onClick={loadSessions}
                    disabled={loading}
                >
                    <FormattedMessage
                        id='runtime_sessions_panel.apply_filters'
                        defaultMessage='Apply'
                    />
                </ActionButton>
            </FilterRow>
            {error && <ErrorText>{error}</ErrorText>}
            {renderSessionContent({sessions, loading, actionSessionID, submitAction})}
        </Panel>
    );
}

function renderSessionContent(props: {
    sessions: RuntimeSession[];
    loading: boolean;
    actionSessionID: string;
    submitAction: (sessionID: string, action: RuntimeSessionAction) => void;
}) {
    if (props.loading && props.sessions.length === 0) {
        return (
            <EmptyState>
                <FormattedMessage
                    id='runtime_sessions_panel.loading'
                    defaultMessage='Loading sessions...'
                />
            </EmptyState>
        );
    }

    if (props.sessions.length === 0) {
        return (
            <EmptyState>
                <FormattedMessage
                    id='runtime_sessions_panel.empty'
                    defaultMessage='No runtime sessions found.'
                />
            </EmptyState>
        );
    }

    return (
        <SessionList>
            {props.sessions.map((session) => (
                <SessionItem key={session.id}>
                    <SessionMain>
                        <SessionTitle>{session.id}</SessionTitle>
                        <SessionMeta>
                            <StatusPill $status={session.status}>{session.status}</StatusPill>
                            <span>{session.runtimeType}</span>
                            <span>{session.providerID || 'provider:none'}</span>
                            <span>{session.model || 'model:none'}</span>
                            {session.userID && <span>{shortID(session.userID)}</span>}
                            {session.channelID && <span>{shortID(session.channelID)}</span>}
                            {session.rootPostID && <span>{shortID(session.rootPostID)}</span>}
                        </SessionMeta>
                        <SessionDetails>
                            {session.externalSessionID && <span>{`external ${session.externalSessionID}`}</span>}
                            {session.workspacePath && <span>{`workspace ${session.workspacePath}`}</span>}
                            {session.lastError && <ErrorInline>{session.lastError}</ErrorInline>}
                        </SessionDetails>
                    </SessionMain>
                    <SessionActions>
                        {canResume(session) && (
                            <ActionButton
                                type='button'
                                onClick={() => props.submitAction(session.id, 'resume')}
                                disabled={props.actionSessionID === `${session.id}:resume`}
                            >
                                <FormattedMessage
                                    id='runtime_sessions_panel.resume'
                                    defaultMessage='Resume'
                                />
                            </ActionButton>
                        )}
                        {canStop(session) && (
                            <ActionButton
                                type='button'
                                onClick={() => props.submitAction(session.id, 'stop')}
                                disabled={props.actionSessionID === `${session.id}:stop`}
                            >
                                <FormattedMessage
                                    id='runtime_sessions_panel.stop'
                                    defaultMessage='Stop'
                                />
                            </ActionButton>
                        )}
                    </SessionActions>
                </SessionItem>
            ))}
        </SessionList>
    );
}

function canStop(session: RuntimeSession): boolean {
    return session.status === 'running' || session.status === 'waiting_approval';
}

function canResume(session: RuntimeSession): boolean {
    return session.status === 'idle' || session.status === 'failed' || session.status === 'cancelled' || session.status === 'completed';
}

function shortID(id: string): string {
    if (id.length <= 8) {
        return id;
    }
    return `${id.slice(0, 8)}...`;
}

const HeaderRow = styled.div`
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 12px;
`;

const SessionCount = styled.div`
    color: rgba(var(--center-channel-color-rgb), 0.72);
    font-size: 13px;
`;

const FilterRow = styled.div`
    display: grid;
    grid-template-columns: 150px repeat(4, minmax(120px, 1fr)) 72px;
    gap: 8px;
    margin-bottom: 8px;
`;

const FilterSelect = styled.select`
    height: 32px;
    min-width: 0;
    border: 1px solid rgba(var(--center-channel-color-rgb), 0.16);
    border-radius: 4px;
    background: var(--center-channel-bg);
    color: var(--center-channel-color);
    font-size: 13px;
`;

const FilterInput = styled.input`
    height: 32px;
    min-width: 0;
    border: 1px solid rgba(var(--center-channel-color-rgb), 0.16);
    border-radius: 4px;
    background: var(--center-channel-bg);
    color: var(--center-channel-color);
    font-size: 13px;
`;

const SessionList = styled.div`
    display: grid;
    gap: 8px;
`;

const SessionItem = styled.div`
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 12px;
    padding: 8px 0;
    border-top: 1px solid rgba(var(--center-channel-color-rgb), 0.08);
`;

const SessionMain = styled.div`
    min-width: 0;
`;

const SessionTitle = styled.div`
    overflow: hidden;
    font-size: 13px;
    font-weight: 600;
    text-overflow: ellipsis;
    white-space: nowrap;
`;

const SessionMeta = styled.div`
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 4px;
    color: rgba(var(--center-channel-color-rgb), 0.72);
    font-size: 12px;
`;

const SessionDetails = styled.div`
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-top: 4px;
    color: rgba(var(--center-channel-color-rgb), 0.64);
    font-size: 12px;
`;

const SessionActions = styled.div`
    display: flex;
    gap: 6px;
`;

const StatusPill = styled.span<{$status: string}>`
    display: inline-flex;
    align-items: center;
    min-height: 20px;
    padding: 1px 6px;
    border-radius: 4px;
    background: ${({$status}) => ($status === 'failed' ? 'rgba(var(--error-text-color-rgb), 0.12)' : 'rgba(var(--button-bg-rgb), 0.1)')};
    color: ${({$status}) => ($status === 'failed' ? 'var(--error-text)' : 'var(--button-bg)')};
    font-size: 12px;
    font-weight: 600;
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

const ErrorInline = styled.span`
    color: var(--error-text);
`;
