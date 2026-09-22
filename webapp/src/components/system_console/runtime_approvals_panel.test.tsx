// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React from 'react';
import {IntlProvider} from 'react-intl';
import {fireEvent, render, screen, waitFor} from '@testing-library/react';

import RuntimeApprovalsPanel from './runtime_approvals_panel';

const mockGetAdminRuntimeApprovals = jest.fn();
const mockSubmitAdminRuntimeApproval = jest.fn();

jest.mock('@/client', () => ({
    getAdminRuntimeApprovals: (...args: unknown[]) => mockGetAdminRuntimeApprovals(...args),
    submitAdminRuntimeApproval: (...args: unknown[]) => mockSubmitAdminRuntimeApproval(...args),
}));

function renderPanel() {
    return render(
        <IntlProvider locale='en'>
            <RuntimeApprovalsPanel/>
        </IntlProvider>,
    );
}

describe('RuntimeApprovalsPanel', () => {
    beforeEach(() => {
        mockGetAdminRuntimeApprovals.mockReset();
        mockSubmitAdminRuntimeApproval.mockReset();
    });

    test('renders pending approvals with decision controls', async () => {
        mockGetAdminRuntimeApprovals.mockResolvedValue([{
            id: 'approval-1',
            runtimeSessionID: 'session-123456789',
            externalApprovalID: 'provider-approval-1',
            subagentRunID: 'subagent-123456789',
            status: 'pending',
            requestedBy: 'user-123456789',
            expiresAt: 1000,
            requestPayload: {
                method: 'item/fileChange/requestApproval',
                params: {
                    action: 'write',
                    path: '/workspace/result.md',
                },
            },
        }]);

        renderPanel();

        await screen.findByText('write: /workspace/result.md');
        expect(screen.getByText('approval-1')).not.toBeNull();
        expect(screen.getByText('1 approval')).not.toBeNull();
        expect(screen.getAllByText('pending').length).toBeGreaterThan(0);
        expect(screen.getByText('session session-...')).not.toBeNull();
        expect(screen.getByText('subagent subagent...')).not.toBeNull();
        expect(screen.getByText('external provider-approval-1')).not.toBeNull();
        expect(screen.getByText('requested user-123...')).not.toBeNull();
        expect(screen.getByText('expires 1970-01-01T00:00:01.000Z')).not.toBeNull();
        expect(screen.getByRole('button', {name: 'Accept'})).not.toBeNull();
        expect(screen.getByRole('button', {name: 'Deny'})).not.toBeNull();
    });

    test('applies approval filters', async () => {
        mockGetAdminRuntimeApprovals.mockResolvedValue([]);

        renderPanel();

        await screen.findByText('No runtime approvals found.');
        fireEvent.change(screen.getByLabelText('Approval status filter'), {target: {value: 'denied'}});
        fireEvent.change(screen.getByLabelText('Runtime session ID filter'), {target: {value: 'session-1'}});
        fireEvent.change(screen.getByLabelText('Requested by user ID filter'), {target: {value: 'user-1'}});
        fireEvent.click(screen.getByRole('button', {name: 'Apply'}));

        await waitFor(() => expect(mockGetAdminRuntimeApprovals).toHaveBeenLastCalledWith({
            runtimeSessionId: 'session-1',
            requestedBy: 'user-1',
            status: 'denied',
            limit: 50,
        }));
    });

    test('submits approval decision and reloads approvals', async () => {
        mockGetAdminRuntimeApprovals.
            mockResolvedValueOnce([{
                id: 'approval-1',
                runtimeSessionID: 'session-1',
                status: 'pending',
                requestPayload: {
                    params: {
                        command: 'npm test',
                    },
                },
            }]).
            mockResolvedValueOnce([]);
        mockSubmitAdminRuntimeApproval.mockImplementation(() => Promise.resolve());

        renderPanel();

        await screen.findByText('Command: npm test');
        fireEvent.click(screen.getByRole('button', {name: 'Accept'}));

        await waitFor(() => expect(mockSubmitAdminRuntimeApproval).toHaveBeenCalledWith('approval-1', 'accept'));
        await waitFor(() => expect(mockGetAdminRuntimeApprovals).toHaveBeenCalledTimes(2));
    });

    test('renders load failure', async () => {
        mockGetAdminRuntimeApprovals.mockRejectedValue(new Error('boom'));

        renderPanel();

        await screen.findByText('Failed to load runtime approvals.');
    });
});
