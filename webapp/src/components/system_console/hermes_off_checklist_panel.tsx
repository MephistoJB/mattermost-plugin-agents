// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React, {useCallback, useEffect, useState} from 'react';
import styled from 'styled-components';
import {RefreshIcon} from '@mattermost/compass-icons/components';
import {FormattedMessage, useIntl} from 'react-intl';

import {getHermesOffChecklist, updateHermesOffChecklistItem} from '@/client';
import type {HermesOffChecklist, HermesOffChecklistItem} from '@/types/runtime';

import {ButtonIcon} from '../assets/buttons';

import Panel from './panel';

export default function HermesOffChecklistPanel() {
    const intl = useIntl();
    const [checklist, setChecklist] = useState<HermesOffChecklist | null>(null);
    const [loading, setLoading] = useState(true);
    const [updatingItemKey, setUpdatingItemKey] = useState('');
    const [evidenceByItemKey, setEvidenceByItemKey] = useState<Record<string, string>>({});
    const [error, setError] = useState('');

    const loadChecklist = useCallback(async () => {
        setLoading(true);
        try {
            const nextChecklist = await getHermesOffChecklist();
            setChecklist(nextChecklist);
            setEvidenceByItemKey(evidenceMap(nextChecklist));
            setError('');
        } catch {
            setError(intl.formatMessage({id: 'hermes_off_checklist_panel.load_error', defaultMessage: 'Failed to load Hermes-off checklist.'}));
        } finally {
            setLoading(false);
        }
    }, [intl]);

    useEffect(() => {
        loadChecklist();
    }, [loadChecklist]);

    const updateItem = useCallback(async (item: HermesOffChecklistItem) => {
        setUpdatingItemKey(item.key);
        try {
            await updateHermesOffChecklistItem(item.key, {
                status: item.status === 'ok' ? 'manual' : 'ok',
                detail: evidenceByItemKey[item.key] || '',
            });
            await loadChecklist();
        } catch {
            setError(intl.formatMessage({id: 'hermes_off_checklist_panel.update_error', defaultMessage: 'Failed to update Hermes-off checklist.'}));
        } finally {
            setUpdatingItemKey('');
        }
    }, [evidenceByItemKey, intl, loadChecklist]);

    const updateEvidence = useCallback((itemKey: string, evidence: string) => {
        setEvidenceByItemKey((current) => ({
            ...current,
            [itemKey]: evidence,
        }));
    }, []);

    return (
        <Panel
            title={intl.formatMessage({id: 'hermes_off_checklist_panel.title', defaultMessage: 'Hermes-Off Checklist'})}
            subtitle={intl.formatMessage({id: 'hermes_off_checklist_panel.subtitle', defaultMessage: 'Track automatic readiness checks and manual cutover gates before disabling Hermes.'})}
        >
            <HeaderRow>
                <StatusPill $ready={Boolean(checklist?.ready)}>
                    {checklist?.ready ? (
                        <FormattedMessage
                            id='hermes_off_checklist_panel.ready'
                            defaultMessage='Ready'
                        />
                    ) : (
                        <FormattedMessage
                            id='hermes_off_checklist_panel.not_ready'
                            defaultMessage='Not ready'
                        />
                    )}
                </StatusPill>
                <ButtonIcon
                    type='button'
                    aria-label={intl.formatMessage({id: 'hermes_off_checklist_panel.refresh', defaultMessage: 'Refresh Hermes-off checklist'})}
                    onClick={loadChecklist}
                    disabled={loading}
                >
                    <RefreshIcon size={18}/>
                </ButtonIcon>
            </HeaderRow>
            {error && <ErrorText>{error}</ErrorText>}
            {renderChecklist(checklist, loading, updatingItemKey, evidenceByItemKey, updateEvidence, updateItem)}
        </Panel>
    );
}

function renderChecklist(
    checklist: HermesOffChecklist | null,
    loading: boolean,
    updatingItemKey: string,
    evidenceByItemKey: Record<string, string>,
    updateEvidence: (itemKey: string, evidence: string) => void,
    updateItem: (item: HermesOffChecklistItem) => void,
) {
    if (loading && !checklist) {
        return (
            <EmptyState>
                <FormattedMessage
                    id='hermes_off_checklist_panel.loading'
                    defaultMessage='Loading checklist...'
                />
            </EmptyState>
        );
    }
    if (!checklist) {
        return (
            <EmptyState>
                <FormattedMessage
                    id='hermes_off_checklist_panel.empty'
                    defaultMessage='No checklist available.'
                />
            </EmptyState>
        );
    }

    return (
        <GroupList>
            {checklist.groups.map((group) => (
                <ChecklistGroup key={group.key}>
                    <GroupHeader>{group.label}</GroupHeader>
                    {group.items.map((item) => (
                        <ChecklistRow key={item.key}>
                            <ItemStatus $status={item.status}>{item.status}</ItemStatus>
                            <ItemMain>
                                <ItemLabel>{item.label}</ItemLabel>
                                <ItemDetail>{formatDetail(item)}</ItemDetail>
                                {isManualGate(item) && (
                                    <EvidenceInput
                                        aria-label={`${item.label} evidence`}
                                        value={evidenceByItemKey[item.key] || ''}
                                        placeholder='Evidence or operator note'
                                        onChange={(e) => updateEvidence(item.key, e.target.value)}
                                    />
                                )}
                            </ItemMain>
                            {isManualGate(item) && (
                                <ActionButton
                                    type='button'
                                    onClick={() => updateItem(item)}
                                    disabled={updatingItemKey === item.key || markDoneDisabled(item, evidenceByItemKey[item.key] || '')}
                                >
                                    {item.status === 'ok' ? (
                                        <FormattedMessage
                                            id='hermes_off_checklist_panel.reopen'
                                            defaultMessage='Reopen'
                                        />
                                    ) : (
                                        <FormattedMessage
                                            id='hermes_off_checklist_panel.mark_done'
                                            defaultMessage='Mark done'
                                        />
                                    )}
                                </ActionButton>
                            )}
                        </ChecklistRow>
                    ))}
                </ChecklistGroup>
            ))}
        </GroupList>
    );
}

function evidenceMap(checklist: HermesOffChecklist) {
    const out: Record<string, string> = {};
    for (const group of checklist.groups) {
        for (const item of group.items) {
            if (item.manual) {
                out[item.key] = item.evidence || '';
            }
        }
    }
    return out;
}

function isManualGate(item: HermesOffChecklistItem) {
    return Boolean(item.manual);
}

function markDoneDisabled(item: HermesOffChecklistItem, evidence: string) {
    return item.status !== 'ok' && evidence.trim() === '';
}

function formatDetail(item: HermesOffChecklistItem) {
    if (!item.required) {
        return item.detail;
    }
    return `${item.detail} | required`;
}

const HeaderRow = styled.div`
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-bottom: 12px;
`;

const StatusPill = styled.span<{$ready: boolean}>`
    display: inline-flex;
    align-items: center;
    min-height: 24px;
    padding: 2px 8px;
    border-radius: 4px;
    background: ${({$ready}) => ($ready ? 'rgba(var(--online-indicator-rgb), 0.12)' : 'rgba(var(--error-text-color-rgb), 0.12)')};
    color: ${({$ready}) => ($ready ? 'var(--online-indicator)' : 'var(--error-text)')};
    font-size: 12px;
    font-weight: 600;
`;

const GroupList = styled.div`
    display: flex;
    flex-direction: column;
    gap: 12px;
`;

const ChecklistGroup = styled.div`
    display: flex;
    flex-direction: column;
    gap: 6px;
`;

const GroupHeader = styled.div`
    color: rgba(var(--center-channel-color-rgb), 0.88);
    font-size: 12px;
    font-weight: 600;
`;

const ChecklistRow = styled.div`
    display: grid;
    grid-template-columns: 72px minmax(0, 1fr) auto;
    gap: 8px;
    align-items: start;
    padding: 6px 0;
    border-top: 1px solid rgba(var(--center-channel-color-rgb), 0.08);

    @media screen and (max-width: 700px) {
        grid-template-columns: 1fr;
    }
`;

const ItemStatus = styled.span<{$status: string}>`
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-height: 20px;
    border-radius: 4px;
    background: ${({$status}) => {
        if ($status === 'ok') {
            return 'rgba(var(--online-indicator-rgb), 0.12)';
        }
        if ($status === 'warning' || $status === 'manual') {
            return 'rgba(var(--away-indicator-rgb), 0.12)';
        }
        return 'rgba(var(--error-text-color-rgb), 0.12)';
    }};
    color: ${({$status}) => {
        if ($status === 'ok') {
            return 'var(--online-indicator)';
        }
        if ($status === 'warning' || $status === 'manual') {
            return 'var(--away-indicator)';
        }
        return 'var(--error-text)';
    }};
    font-size: 11px;
    font-weight: 600;
`;

const ItemMain = styled.div`
    min-width: 0;
`;

const ItemLabel = styled.div`
    color: rgba(var(--center-channel-color-rgb), 0.88);
    font-size: 12px;
    font-weight: 600;
`;

const ItemDetail = styled.div`
    margin-top: 2px;
    color: rgba(var(--center-channel-color-rgb), 0.64);
    font-size: 12px;
`;

const EvidenceInput = styled.input`
    width: 100%;
    min-height: 30px;
    margin-top: 6px;
    padding: 4px 8px;
    border: 1px solid rgba(var(--center-channel-color-rgb), 0.16);
    border-radius: 4px;
    background: var(--center-channel-bg);
    color: rgba(var(--center-channel-color-rgb), 0.88);
    font-size: 12px;
`;

const ActionButton = styled.button`
    min-width: 84px;
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
