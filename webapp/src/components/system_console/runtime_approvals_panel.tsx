// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React, {useCallback, useEffect, useState} from 'react';
import styled from 'styled-components';
import {RefreshIcon} from '@mattermost/compass-icons/components';
import {FormattedMessage, useIntl} from 'react-intl';

import {getAdminRuntimeApprovals, submitAdminRuntimeApproval} from '@/client';
import type {RuntimeApproval} from '@/types/runtime';
import {runtimeApprovalSummary} from '@/utils/runtime_approvals';

import {ButtonIcon} from '../assets/buttons';

import Panel from './panel';

const APPROVAL_LIMIT = 50;
const APPROVAL_STATUS_OPTIONS = ['', 'pending', 'accepted', 'denied', 'expired', 'cancelled'];

export default function RuntimeApprovalsPanel() {
    const intl = useIntl();
    const [approvals, setApprovals] = useState<RuntimeApproval[]>([]);
    const [statusFilter, setStatusFilter] = useState('pending');
    const [sessionFilter, setSessionFilter] = useState('');
    const [requestedByFilter, setRequestedByFilter] = useState('');
    const [loading, setLoading] = useState(true);
    const [decisionApprovalID, setDecisionApprovalID] = useState('');
    const [error, setError] = useState('');

    const loadApprovals = useCallback(async () => {
        setLoading(true);
        try {
            const params = {limit: APPROVAL_LIMIT} as {
                runtimeSessionId?: string;
                requestedBy?: string;
                status?: string;
                limit: number;
            };
            if (sessionFilter.trim()) {
                params.runtimeSessionId = sessionFilter.trim();
            }
            if (requestedByFilter.trim()) {
                params.requestedBy = requestedByFilter.trim();
            }
            if (statusFilter) {
                params.status = statusFilter;
            }
            setApprovals(await getAdminRuntimeApprovals(params));
            setError('');
        } catch {
            setError(intl.formatMessage({id: 'runtime_approvals_panel.load_error', defaultMessage: 'Failed to load runtime approvals.'}));
        } finally {
            setLoading(false);
        }
    }, [intl, requestedByFilter, sessionFilter, statusFilter]);

    useEffect(() => {
        loadApprovals();
    }, [loadApprovals]);

    const submitDecision = useCallback(async (approvalID: string, decision: 'accept' | 'deny') => {
        setDecisionApprovalID(`${approvalID}:${decision}`);
        try {
            await submitAdminRuntimeApproval(approvalID, decision);
            await loadApprovals();
        } catch {
            setError(intl.formatMessage({id: 'runtime_approvals_panel.decision_error', defaultMessage: 'Failed to submit runtime approval decision.'}));
        } finally {
            setDecisionApprovalID('');
        }
    }, [intl, loadApprovals]);

    return (
        <Panel
            title={intl.formatMessage({id: 'runtime_approvals_panel.title', defaultMessage: 'Runtime Approvals'})}
            subtitle={intl.formatMessage({id: 'runtime_approvals_panel.subtitle', defaultMessage: 'Review and decide pending Codex/local runtime approval requests.'})}
        >
            <HeaderRow>
                <ApprovalCount>
                    <FormattedMessage
                        id='runtime_approvals_panel.approval_count'
                        defaultMessage='{count, plural, one {# approval} other {# approvals}}'
                        values={{count: approvals.length}}
                    />
                </ApprovalCount>
                <ButtonIcon
                    type='button'
                    aria-label={intl.formatMessage({id: 'runtime_approvals_panel.refresh', defaultMessage: 'Refresh runtime approvals'})}
                    onClick={loadApprovals}
                    disabled={loading}
                >
                    <RefreshIcon size={18}/>
                </ButtonIcon>
            </HeaderRow>
            <FilterRow>
                <FilterSelect
                    aria-label={intl.formatMessage({id: 'runtime_approvals_panel.status_filter', defaultMessage: 'Approval status filter'})}
                    value={statusFilter}
                    onChange={(event) => setStatusFilter(event.target.value)}
                >
                    {APPROVAL_STATUS_OPTIONS.map((status) => (
                        <option
                            key={status || 'all'}
                            value={status}
                        >
                            {status || intl.formatMessage({id: 'runtime_approvals_panel.all_statuses', defaultMessage: 'All statuses'})}
                        </option>
                    ))}
                </FilterSelect>
                <FilterInput
                    aria-label={intl.formatMessage({id: 'runtime_approvals_panel.session_filter', defaultMessage: 'Runtime session ID filter'})}
                    value={sessionFilter}
                    placeholder={intl.formatMessage({id: 'runtime_approvals_panel.session_filter_placeholder', defaultMessage: 'Runtime session ID'})}
                    onChange={(event) => setSessionFilter(event.target.value)}
                />
                <FilterInput
                    aria-label={intl.formatMessage({id: 'runtime_approvals_panel.requested_by_filter', defaultMessage: 'Requested by user ID filter'})}
                    value={requestedByFilter}
                    placeholder={intl.formatMessage({id: 'runtime_approvals_panel.requested_by_filter_placeholder', defaultMessage: 'Requested by'})}
                    onChange={(event) => setRequestedByFilter(event.target.value)}
                />
                <ActionButton
                    type='button'
                    onClick={loadApprovals}
                    disabled={loading}
                >
                    <FormattedMessage
                        id='runtime_approvals_panel.apply_filters'
                        defaultMessage='Apply'
                    />
                </ActionButton>
            </FilterRow>
            {error && <ErrorText>{error}</ErrorText>}
            {renderApprovalContent({approvals, loading, decisionApprovalID, submitDecision})}
        </Panel>
    );
}

function renderApprovalContent(props: {
    approvals: RuntimeApproval[];
    loading: boolean;
    decisionApprovalID: string;
    submitDecision: (approvalID: string, decision: 'accept' | 'deny') => void;
}) {
    if (props.loading && props.approvals.length === 0) {
        return (
            <EmptyState>
                <FormattedMessage
                    id='runtime_approvals_panel.loading'
                    defaultMessage='Loading approvals...'
                />
            </EmptyState>
        );
    }
    if (props.approvals.length === 0) {
        return (
            <EmptyState>
                <FormattedMessage
                    id='runtime_approvals_panel.empty'
                    defaultMessage='No runtime approvals found.'
                />
            </EmptyState>
        );
    }

    return (
        <ApprovalList>
            {props.approvals.map((approval) => (
                <ApprovalItem key={approval.id}>
                    <ApprovalMain>
                        <ApprovalTitle>{runtimeApprovalSummary(approval)}</ApprovalTitle>
                        <ApprovalMeta>
                            <StatusPill $status={approval.status}>{approval.status}</StatusPill>
                            <span>{approval.id}</span>
                            {approval.runtimeSessionID && <span>{`session ${shortID(approval.runtimeSessionID)}`}</span>}
                            {approval.subagentRunID && <span>{`subagent ${shortID(approval.subagentRunID)}`}</span>}
                            {approval.externalApprovalID && <span>{`external ${approval.externalApprovalID}`}</span>}
                            {approval.requestedBy && <span>{`requested ${shortID(approval.requestedBy)}`}</span>}
                            {approval.decidedBy && <span>{`decided ${shortID(approval.decidedBy)}`}</span>}
                            {approval.expiresAt > 0 && <span>{`expires ${formatMillis(approval.expiresAt)}`}</span>}
                        </ApprovalMeta>
                        {approval.decisionReason && <ApprovalReason>{approval.decisionReason}</ApprovalReason>}
                    </ApprovalMain>
                    {approval.status === 'pending' && (
                        <ApprovalActions>
                            <ActionButton
                                type='button'
                                onClick={() => props.submitDecision(approval.id, 'accept')}
                                disabled={props.decisionApprovalID === `${approval.id}:accept`}
                            >
                                <FormattedMessage
                                    id='runtime_approvals_panel.accept'
                                    defaultMessage='Accept'
                                />
                            </ActionButton>
                            <ActionButton
                                type='button'
                                onClick={() => props.submitDecision(approval.id, 'deny')}
                                disabled={props.decisionApprovalID === `${approval.id}:deny`}
                            >
                                <FormattedMessage
                                    id='runtime_approvals_panel.deny'
                                    defaultMessage='Deny'
                                />
                            </ActionButton>
                        </ApprovalActions>
                    )}
                </ApprovalItem>
            ))}
        </ApprovalList>
    );
}

function shortID(id: string): string {
    if (id.length <= 8) {
        return id;
    }
    return `${id.slice(0, 8)}...`;
}

function formatMillis(ms: number): string {
    return new Date(ms).toISOString();
}

const HeaderRow = styled.div`
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 12px;
`;

const ApprovalCount = styled.div`
    color: rgba(var(--center-channel-color-rgb), 0.72);
    font-size: 13px;
`;

const FilterRow = styled.div`
    display: grid;
    grid-template-columns: 150px minmax(160px, 1fr) minmax(140px, 1fr) 72px;
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

const ApprovalList = styled.div`
    display: grid;
    gap: 8px;
`;

const ApprovalItem = styled.div`
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 12px;
    padding: 8px 0;
    border-top: 1px solid rgba(var(--center-channel-color-rgb), 0.08);
`;

const ApprovalMain = styled.div`
    min-width: 0;
`;

const ApprovalTitle = styled.div`
    overflow: hidden;
    font-size: 13px;
    font-weight: 600;
    text-overflow: ellipsis;
    white-space: nowrap;
`;

const ApprovalMeta = styled.div`
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 4px;
    color: rgba(var(--center-channel-color-rgb), 0.72);
    font-size: 12px;
`;

const ApprovalReason = styled.div`
    margin-top: 4px;
    color: rgba(var(--center-channel-color-rgb), 0.64);
    font-size: 12px;
`;

const ApprovalActions = styled.div`
    display: flex;
    gap: 6px;
`;

const StatusPill = styled.span<{$status: string}>`
    display: inline-flex;
    align-items: center;
    min-height: 20px;
    padding: 1px 6px;
    border-radius: 4px;
    background: ${({$status}) => ($status === 'denied' ? 'rgba(var(--error-text-color-rgb), 0.12)' : 'rgba(var(--button-bg-rgb), 0.1)')};
    color: ${({$status}) => ($status === 'denied' ? 'var(--error-text)' : 'var(--button-bg)')};
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
