// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React from 'react';
import {IntlProvider} from 'react-intl';
import {fireEvent, render, screen, waitFor} from '@testing-library/react';

import RuntimeHealthPanel from './runtime_health_panel';

const mockGetRuntimeHealth = jest.fn();

jest.mock('@/client', () => ({
    getRuntimeHealth: () => mockGetRuntimeHealth(),
}));

function renderPanel() {
    return render(
        <IntlProvider locale='en'>
            <RuntimeHealthPanel/>
        </IntlProvider>,
    );
}

describe('RuntimeHealthPanel', () => {
    beforeEach(() => {
        mockGetRuntimeHealth.mockReset();
    });

    test('renders runtime health metrics', async () => {
        mockGetRuntimeHealth.mockResolvedValue({
            enabled: true,
            healthy: true,
            hermesOffReady: true,
            readiness: [
                {key: 'control_plane', label: 'Runtime control plane', status: 'ok', required: true, detail: 'enabled'},
                {key: 'cloud_runtime_policy', label: 'Codex/OpenAI routing', status: 'ok', required: true, detail: 'cloud runtime policy configured'},
                {key: 'local_voice', label: 'Local voice path', status: 'ok', required: true, detail: 'local STT/TTS ready'},
                {key: 'restart_recovery', label: 'Restart recovery', status: 'ok', required: true, detail: 'last restart recovery completed successfully'},
            ],
            activeSessions: 2,
            sessionsByStatus: {running: 1, waiting_approval: 1},
            sessionsByRuntimeType: {codex: 1, local: 1},
            usage: {
                input_tokens: 150,
                output_tokens: 35,
                duration_ms: 3400,
                cost: 0.01,
            },
            activeTasks: 3,
            tasksByStatus: {queued: 2, running: 1},
            pendingApprovals: 4,
            activeSupervisorRuns: 1,
            supervisorsByStatus: {running: 1},
            voice: {
                transcriptionConfigured: true,
                localTranscriptionConfigured: true,
                textToSpeechConfigured: true,
                localTextToSpeechConfigured: true,
                localVoiceReady: true,
            },
            recovery: {
                startedAt: 10,
                completedAt: 20,
                ready: true,
            },
        });

        renderPanel();

        await screen.findByText('Healthy');
        expect(screen.getByText('Active sessions')).not.toBeNull();
        expect(screen.getByText('Active tasks')).not.toBeNull();
        expect(screen.getByText('Pending approvals')).not.toBeNull();
        expect(screen.getByText('Supervisor runs')).not.toBeNull();
        expect(screen.getByText('Hermes-off ready')).not.toBeNull();
        expect(screen.getByText('Hermes-off readiness')).not.toBeNull();
        expect(screen.getByText('Codex/OpenAI routing')).not.toBeNull();
        expect(screen.getByText('cloud runtime policy configured')).not.toBeNull();
        expect(screen.getByText('local STT/TTS ready')).not.toBeNull();
        expect(screen.getByText('last restart recovery completed successfully')).not.toBeNull();
        expect(screen.getByText('running:1, waiting_approval:1')).not.toBeNull();
        expect(screen.getByText('codex:1, local:1')).not.toBeNull();
        expect(screen.getByText('in:150, out:35, duration:3s, cost:$0.0100')).not.toBeNull();
        expect(screen.getByText('stt:configured, local_stt:configured, tts:configured, local_tts:configured, local_ready:yes')).not.toBeNull();
        expect(screen.getAllByText('Restart recovery').length).toBeGreaterThan(0);
        expect(screen.getAllByText('ok').length).toBeGreaterThan(0);
    });

    test('renders unavailable state on load failure', async () => {
        mockGetRuntimeHealth.mockRejectedValue(new Error('boom'));

        renderPanel();

        await screen.findByText('Failed to load runtime health.');
        expect(screen.getByText('Unavailable')).not.toBeNull();
    });

    test('refreshes runtime health', async () => {
        mockGetRuntimeHealth.
            mockResolvedValueOnce({
                enabled: true,
                healthy: true,
                hermesOffReady: false,
                activeSessions: 1,
                sessionsByStatus: {running: 1},
                sessionsByRuntimeType: {local: 1},
                activeTasks: 0,
                tasksByStatus: {},
                pendingApprovals: 0,
                activeSupervisorRuns: 0,
                supervisorsByStatus: {},
            }).
            mockResolvedValueOnce({
                enabled: true,
                healthy: true,
                hermesOffReady: false,
                activeSessions: 5,
                sessionsByStatus: {running: 5},
                sessionsByRuntimeType: {codex: 5},
                activeTasks: 0,
                tasksByStatus: {},
                pendingApprovals: 0,
                activeSupervisorRuns: 0,
                supervisorsByStatus: {},
            });

        renderPanel();

        await screen.findByText('1');
        fireEvent.click(screen.getByLabelText('Refresh runtime health'));

        await waitFor(() => expect(mockGetRuntimeHealth).toHaveBeenCalledTimes(2));
        await screen.findByText('5');
    });
});
