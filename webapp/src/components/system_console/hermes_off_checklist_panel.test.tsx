// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React from 'react';
import {IntlProvider} from 'react-intl';
import {fireEvent, render, screen, waitFor} from '@testing-library/react';

import HermesOffChecklistPanel from './hermes_off_checklist_panel';

const mockGetHermesOffChecklist = jest.fn();
const mockUpdateHermesOffChecklistItem = jest.fn();

jest.mock('@/client', () => ({
    getHermesOffChecklist: () => mockGetHermesOffChecklist(),
    updateHermesOffChecklistItem: (...args: unknown[]) => mockUpdateHermesOffChecklistItem(...args),
}));

function renderPanel() {
    return render(
        <IntlProvider locale='en'>
            <HermesOffChecklistPanel/>
        </IntlProvider>,
    );
}

describe('HermesOffChecklistPanel', () => {
    beforeEach(() => {
        mockGetHermesOffChecklist.mockReset();
        mockUpdateHermesOffChecklistItem.mockReset();
    });

    test('renders automatic and manual Hermes-off gates', async () => {
        mockGetHermesOffChecklist.mockResolvedValue({
            ready: false,
            generatedAt: 1,
            health: {enabled: true, healthy: true, activeSessions: 0},
            groups: [
                {
                    key: 'runtime_readiness',
                    label: 'Runtime readiness',
                    items: [
                        {key: 'cloud_runtime_policy', label: 'Codex/OpenAI routing', status: 'ok', required: true, manual: false, detail: 'cloud runtime policy configured'},
                    ],
                },
                {
                    key: 'cutover',
                    label: 'Cutover',
                    items: [
                        {key: 'test_channel_soak', label: 'Test channel soak', status: 'manual', required: true, manual: true, detail: 'run a representative test channel without Hermes for 7 days', evidence: ''},
                    ],
                },
                {
                    key: 'hermes_capability_migration',
                    label: 'Hermes capability migration',
                    items: [
                        {key: 'migration_workspace_files', label: 'Workspace and files', status: 'manual', required: true, manual: true, detail: 'verify attachment import, workspace access, generated file attachments, and generated file links', evidence: ''},
                        {key: 'migration_approval_resume', label: 'Approval and resume flow', status: 'manual', required: true, manual: true, detail: 'verify pending approvals, accept/deny decisions, provider continuation, resumed output, and thread-visible decision/resume notices', evidence: ''},
                    ],
                },
            ],
        });

        renderPanel();

        await screen.findByText('Runtime readiness');
        expect(screen.getByText('Not ready')).not.toBeNull();
        expect(screen.getByText('Codex/OpenAI routing')).not.toBeNull();
        expect(screen.getByText('cloud runtime policy configured | required')).not.toBeNull();
        expect(screen.getByText('Cutover')).not.toBeNull();
        expect(screen.getByText('Test channel soak')).not.toBeNull();
        expect(screen.getByText('run a representative test channel without Hermes for 7 days | required')).not.toBeNull();
        expect(screen.getByText('Hermes capability migration')).not.toBeNull();
        expect(screen.getByText('Workspace and files')).not.toBeNull();
        expect(screen.getByText('verify attachment import, workspace access, generated file attachments, and generated file links | required')).not.toBeNull();
        expect(screen.getByText('Approval and resume flow')).not.toBeNull();
        expect(screen.getByText('verify pending approvals, accept/deny decisions, provider continuation, resumed output, and thread-visible decision/resume notices | required')).not.toBeNull();
        expect(screen.getByLabelText('Test channel soak evidence')).not.toBeNull();
        for (const button of screen.getAllByRole('button', {name: 'Mark done'})) {
            expect((button as HTMLButtonElement).disabled).toBe(true);
        }
    });

    test('updates manual checklist gates', async () => {
        mockGetHermesOffChecklist.
            mockResolvedValueOnce({
                ready: false,
                generatedAt: 1,
                health: {enabled: true, healthy: true, activeSessions: 0},
                groups: [{
                    key: 'cutover',
                    label: 'Cutover',
                    items: [
                        {key: 'test_channel_soak', label: 'Test channel soak', status: 'manual', required: true, manual: true, detail: 'run a representative test channel without Hermes for 7 days', evidence: ''},
                    ],
                }],
            }).
            mockResolvedValueOnce({
                ready: false,
                generatedAt: 2,
                health: {enabled: true, healthy: true, activeSessions: 0},
                groups: [{
                    key: 'cutover',
                    label: 'Cutover',
                    items: [
                        {key: 'test_channel_soak', label: 'Test channel soak', status: 'ok', required: true, manual: true, detail: 'run a representative test channel without Hermes for 7 days', evidence: '7 days passed'},
                    ],
                }],
            });
        mockUpdateHermesOffChecklistItem.mockResolvedValue({key: 'test_channel_soak', status: 'ok'});

        renderPanel();

        await screen.findByText('Test channel soak');
        expect((screen.getByRole('button', {name: 'Mark done'}) as HTMLButtonElement).disabled).toBe(true);
        fireEvent.change(screen.getByLabelText('Test channel soak evidence'), {target: {value: '7 days passed'}});
        expect((screen.getByRole('button', {name: 'Mark done'}) as HTMLButtonElement).disabled).toBe(false);
        fireEvent.click(screen.getByRole('button', {name: 'Mark done'}));

        await waitFor(() => expect(mockUpdateHermesOffChecklistItem).toHaveBeenCalledWith('test_channel_soak', {
            status: 'ok',
            detail: '7 days passed',
        }));
        await screen.findByDisplayValue('7 days passed');
        expect(screen.getByRole('button', {name: 'Reopen'})).not.toBeNull();
    });

    test('renders load failure and refreshes', async () => {
        mockGetHermesOffChecklist.
            mockRejectedValueOnce(new Error('boom')).
            mockResolvedValueOnce({
                ready: true,
                generatedAt: 2,
                health: {enabled: true, healthy: true, activeSessions: 0},
                groups: [],
            });

        renderPanel();

        await screen.findByText('Failed to load Hermes-off checklist.');
        fireEvent.click(screen.getByLabelText('Refresh Hermes-off checklist'));

        await waitFor(() => expect(mockGetHermesOffChecklist).toHaveBeenCalledTimes(2));
        await screen.findByText('Ready');
    });
});
