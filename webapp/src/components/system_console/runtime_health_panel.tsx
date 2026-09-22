// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React, {useCallback, useEffect, useState} from 'react';
import styled from 'styled-components';
import {RefreshIcon} from '@mattermost/compass-icons/components';
import {FormattedMessage, useIntl} from 'react-intl';

import {getRuntimeHealth} from '@/client';
import type {RuntimeHealth} from '@/types/runtime';

import {ButtonIcon} from '../assets/buttons';

import Panel from './panel';

export default function RuntimeHealthPanel() {
    const intl = useIntl();
    const [health, setHealth] = useState<RuntimeHealth | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState('');

    const loadHealth = useCallback(async () => {
        setLoading(true);
        try {
            const nextHealth = await getRuntimeHealth();
            setHealth(nextHealth);
            setError('');
        } catch {
            setError(intl.formatMessage({id: 'runtime_health_panel.load_error', defaultMessage: 'Failed to load runtime health.'}));
        } finally {
            setLoading(false);
        }
    }, [intl]);

    useEffect(() => {
        loadHealth();
    }, [loadHealth]);

    return (
        <Panel
            title={intl.formatMessage({id: 'runtime_health_panel.title', defaultMessage: 'Runtime Health'})}
            subtitle={intl.formatMessage({id: 'runtime_health_panel.subtitle', defaultMessage: 'Monitor agent runtime sessions, tasks, approvals, and supervisor runs.'})}
        >
            <HeaderRow>
                <StatusPill $healthy={Boolean(health?.healthy)}>
                    {health?.healthy ? (
                        <FormattedMessage
                            id='runtime_health_panel.status_healthy'
                            defaultMessage='Healthy'
                        />
                    ) : (
                        <FormattedMessage
                            id='runtime_health_panel.status_unavailable'
                            defaultMessage='Unavailable'
                        />
                    )}
                </StatusPill>
                <ButtonIcon
                    type='button'
                    aria-label={intl.formatMessage({id: 'runtime_health_panel.refresh', defaultMessage: 'Refresh runtime health'})}
                    onClick={loadHealth}
                    disabled={loading}
                >
                    <RefreshIcon size={18}/>
                </ButtonIcon>
            </HeaderRow>
            {error && <ErrorText>{error}</ErrorText>}
            <MetricGrid>
                <Metric
                    label={intl.formatMessage({id: 'runtime_health_panel.active_sessions', defaultMessage: 'Active sessions'})}
                    value={health?.activeSessions ?? 0}
                    loading={loading}
                />
                <Metric
                    label={intl.formatMessage({id: 'runtime_health_panel.active_tasks', defaultMessage: 'Active tasks'})}
                    value={health?.activeTasks ?? 0}
                    loading={loading}
                />
                <Metric
                    label={intl.formatMessage({id: 'runtime_health_panel.pending_approvals', defaultMessage: 'Pending approvals'})}
                    value={health?.pendingApprovals ?? 0}
                    loading={loading}
                />
                <Metric
                    label={intl.formatMessage({id: 'runtime_health_panel.supervisor_runs', defaultMessage: 'Supervisor runs'})}
                    value={health?.activeSupervisorRuns ?? 0}
                    loading={loading}
                />
                <Metric
                    label={intl.formatMessage({id: 'runtime_health_panel.hermes_off_ready', defaultMessage: 'Hermes-off ready'})}
                    value={health?.hermesOffReady ? 1 : 0}
                    displayValue={health?.hermesOffReady ? 'yes' : 'no'}
                    loading={loading}
                />
            </MetricGrid>
            {health && (
                <Breakdown>
                    <ReadinessBlock>
                        <ReadinessHeader>
                            <FormattedMessage
                                id='runtime_health_panel.readiness'
                                defaultMessage='Hermes-off readiness'
                            />
                        </ReadinessHeader>
                        {health.readiness?.map((check) => (
                            <ReadinessLine key={check.key}>
                                <ReadinessStatus $status={check.status}>{check.status}</ReadinessStatus>
                                <span>{check.label}</span>
                                <code>{check.detail}</code>
                            </ReadinessLine>
                        ))}
                    </ReadinessBlock>
                    <BreakdownLine>
                        <span>
                            <FormattedMessage
                                id='runtime_health_panel.sessions_breakdown'
                                defaultMessage='Sessions'
                            />
                        </span>
                        <code>{formatCounts(health.sessionsByStatus)}</code>
                    </BreakdownLine>
                    <BreakdownLine>
                        <span>
                            <FormattedMessage
                                id='runtime_health_panel.runtimes_breakdown'
                                defaultMessage='Runtimes'
                            />
                        </span>
                        <code>{formatCounts(health.sessionsByRuntimeType)}</code>
                    </BreakdownLine>
                    <BreakdownLine>
                        <span>
                            <FormattedMessage
                                id='runtime_health_panel.usage_breakdown'
                                defaultMessage='Usage'
                            />
                        </span>
                        <code>{formatUsage(health.usage)}</code>
                    </BreakdownLine>
                    <BreakdownLine>
                        <span>
                            <FormattedMessage
                                id='runtime_health_panel.tasks_breakdown'
                                defaultMessage='Tasks'
                            />
                        </span>
                        <code>{formatCounts(health.tasksByStatus)}</code>
                    </BreakdownLine>
                    <BreakdownLine>
                        <span>
                            <FormattedMessage
                                id='runtime_health_panel.supervisors_breakdown'
                                defaultMessage='Supervisors'
                            />
                        </span>
                        <code>{formatCounts(health.supervisorsByStatus)}</code>
                    </BreakdownLine>
                    <BreakdownLine>
                        <span>
                            <FormattedMessage
                                id='runtime_health_panel.voice_breakdown'
                                defaultMessage='Voice'
                            />
                        </span>
                        <code>{formatVoiceHealth(health.voice)}</code>
                    </BreakdownLine>
                    <BreakdownLine>
                        <span>
                            <FormattedMessage
                                id='runtime_health_panel.recovery_breakdown'
                                defaultMessage='Restart recovery'
                            />
                        </span>
                        <code>{formatRecoveryHealth(health.recovery)}</code>
                    </BreakdownLine>
                </Breakdown>
            )}
        </Panel>
    );
}

function Metric(props: {label: string; value: number; displayValue?: string; loading: boolean}) {
    return (
        <MetricBox>
            <MetricValue>{props.loading ? '...' : (props.displayValue ?? props.value)}</MetricValue>
            <MetricLabel>{props.label}</MetricLabel>
        </MetricBox>
    );
}

function formatCounts(counts: Record<string, number> | undefined): string {
    if (!counts) {
        return 'none';
    }
    const entries = Object.entries(counts).
        filter(([, count]) => count > 0).
        sort(([a], [b]) => a.localeCompare(b));
    if (entries.length === 0) {
        return 'none';
    }
    return entries.map(([key, count]) => `${key}:${count}`).join(', ');
}

function formatVoiceHealth(voice: RuntimeHealth['voice']): string {
    if (!voice) {
        return 'unknown';
    }
    return [
        `stt:${voice.transcriptionConfigured ? 'configured' : 'missing'}`,
        `local_stt:${voice.localTranscriptionConfigured ? 'configured' : 'missing'}`,
        `tts:${voice.textToSpeechConfigured ? 'configured' : 'off'}`,
        `local_tts:${voice.localTextToSpeechConfigured ? 'configured' : 'missing'}`,
        `local_ready:${voice.localVoiceReady ? 'yes' : 'no'}`,
    ].join(', ');
}

function formatUsage(usage: RuntimeHealth['usage']): string {
    if (!usage) {
        return 'unknown';
    }
    return [
        `in:${usage.input_tokens || 0}`,
        `out:${usage.output_tokens || 0}`,
        `duration:${formatDurationMS(usage.duration_ms || 0)}`,
        `cost:$${(usage.cost || 0).toFixed(4)}`,
    ].join(', ');
}

function formatRecoveryHealth(recovery: RuntimeHealth['recovery']): string {
    if (!recovery) {
        return 'unknown';
    }
    if (!recovery.startedAt) {
        return 'not run';
    }
    const errors = [
        recovery.runtimeRecoveryError ? `runtime:${recovery.runtimeRecoveryError}` : '',
        recovery.taskRecoveryError ? `tasks:${recovery.taskRecoveryError}` : '',
    ].filter(Boolean);
    if (errors.length > 0) {
        return `failed, ${errors.join(', ')}`;
    }
    return recovery.ready ? 'ok' : 'running';
}

function formatDurationMS(ms: number): string {
    if (!ms || ms <= 0) {
        return '0s';
    }
    const seconds = Math.round(ms / 1000);
    if (seconds < 60) {
        return `${seconds}s`;
    }
    const minutes = Math.floor(seconds / 60);
    const remainder = seconds % 60;
    return remainder === 0 ? `${minutes}m` : `${minutes}m${remainder}s`;
}

const HeaderRow = styled.div`
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-bottom: 12px;
`;

const StatusPill = styled.span<{$healthy: boolean}>`
    display: inline-flex;
    align-items: center;
    min-height: 24px;
    padding: 2px 8px;
    border-radius: 4px;
    background: ${({$healthy}) => ($healthy ? 'rgba(var(--online-indicator-rgb), 0.12)' : 'rgba(var(--error-text-color-rgb), 0.12)')};
    color: ${({$healthy}) => ($healthy ? 'var(--online-indicator)' : 'var(--error-text)')};
    font-size: 12px;
    font-weight: 600;
`;

const MetricGrid = styled.div`
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
    gap: 8px;
`;

const MetricBox = styled.div`
    min-height: 64px;
    padding: 10px 12px;
    border: 1px solid rgba(var(--center-channel-color-rgb), 0.12);
    border-radius: 4px;
    background: rgba(var(--center-channel-color-rgb), 0.03);
`;

const MetricValue = styled.div`
    font-size: 20px;
    font-weight: 600;
    line-height: 24px;
`;

const MetricLabel = styled.div`
    margin-top: 4px;
    color: rgba(var(--center-channel-color-rgb), 0.72);
    font-size: 12px;
`;

const Breakdown = styled.div`
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-top: 12px;
`;

const ReadinessBlock = styled.div`
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding-bottom: 8px;
`;

const ReadinessHeader = styled.div`
    color: rgba(var(--center-channel-color-rgb), 0.88);
    font-size: 12px;
    font-weight: 600;
`;

const ReadinessLine = styled.div`
    display: grid;
    grid-template-columns: 56px minmax(112px, 0.5fr) minmax(0, 1fr);
    gap: 8px;
    align-items: center;
    color: rgba(var(--center-channel-color-rgb), 0.72);
    font-size: 12px;

    code {
        overflow-wrap: anywhere;
        color: rgba(var(--center-channel-color-rgb), 0.88);
    }
`;

const ReadinessStatus = styled.span<{$status: string}>`
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-height: 20px;
    border-radius: 4px;
    background: ${({$status}) => {
        if ($status === 'ok') {
            return 'rgba(var(--online-indicator-rgb), 0.12)';
        }
        if ($status === 'warning') {
            return 'rgba(var(--away-indicator-rgb), 0.12)';
        }
        return 'rgba(var(--error-text-color-rgb), 0.12)';
    }};
    color: ${({$status}) => {
        if ($status === 'ok') {
            return 'var(--online-indicator)';
        }
        if ($status === 'warning') {
            return 'var(--away-indicator)';
        }
        return 'var(--error-text)';
    }};
    font-size: 11px;
    font-weight: 600;
`;

const BreakdownLine = styled.div`
    display: grid;
    grid-template-columns: 96px minmax(0, 1fr);
    gap: 8px;
    color: rgba(var(--center-channel-color-rgb), 0.72);
    font-size: 12px;

    code {
        overflow-wrap: anywhere;
        color: rgba(var(--center-channel-color-rgb), 0.88);
    }
`;

const ErrorText = styled.div`
    margin-bottom: 12px;
    color: var(--error-text);
    font-size: 13px;
`;
