// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React from 'react';
import {IntlProvider} from 'react-intl';
import {fireEvent, render, screen, waitFor} from '@testing-library/react';

import WorkspacePoliciesPanel from './workspace_policies_panel';

const mockGetWorkspacePolicies = jest.fn();
const mockCreateWorkspacePolicy = jest.fn();
const mockUpdateWorkspacePolicy = jest.fn();

jest.mock('@/client', () => ({
    getWorkspacePolicies: (...args: unknown[]) => mockGetWorkspacePolicies(...args),
    createWorkspacePolicy: (...args: unknown[]) => mockCreateWorkspacePolicy(...args),
    updateWorkspacePolicy: (...args: unknown[]) => mockUpdateWorkspacePolicy(...args),
}));

function renderPanel() {
    return render(
        <IntlProvider locale='en'>
            <WorkspacePoliciesPanel/>
        </IntlProvider>,
    );
}

describe('WorkspacePoliciesPanel', () => {
    beforeEach(() => {
        mockGetWorkspacePolicies.mockReset();
        mockCreateWorkspacePolicy.mockReset();
        mockUpdateWorkspacePolicy.mockReset();
    });

    test('renders workspace policies and saves edits', async () => {
        mockGetWorkspacePolicies.
            mockResolvedValueOnce([{
                id: 'policy-1',
                name: 'Project',
                allowedRoots: ['/workspace/project'],
                defaultWorkspacePath: '/workspace/project',
                mode: 'ask_write',
                networkMode: 'ask',
                shellMode: 'ask',
            }]).
            mockResolvedValueOnce([]);
        mockUpdateWorkspacePolicy.mockResolvedValue({});

        renderPanel();

        await screen.findByDisplayValue('Project');
        expect(screen.getByText('1 policy')).not.toBeNull();
        fireEvent.change(screen.getByLabelText('Workspace policy allowed roots'), {target: {value: '/workspace/project\n/workspace/shared'}});
        fireEvent.change(screen.getByLabelText('Workspace policy shell mode'), {target: {value: 'allowed'}});
        fireEvent.click(screen.getByRole('button', {name: 'Save'}));

        await waitFor(() => expect(mockUpdateWorkspacePolicy).toHaveBeenCalledWith('policy-1', {
            name: 'Project',
            allowedRoots: ['/workspace/project', '/workspace/shared'],
            defaultWorkspacePath: '/workspace/project',
            mode: 'ask_write',
            networkMode: 'ask',
            shellMode: 'allowed',
            metadata: {},
        }));
    });

    test('creates workspace policies', async () => {
        mockGetWorkspacePolicies.mockResolvedValue([]);
        mockCreateWorkspacePolicy.mockResolvedValue({});

        renderPanel();

        await screen.findByText('No workspace policies found.');
        fireEvent.change(screen.getByLabelText('New workspace policy name'), {target: {value: 'Sensitive'}});
        fireEvent.change(screen.getByLabelText('New workspace policy allowed roots'), {target: {value: '/secure/project,/secure/shared'}});
        fireEvent.change(screen.getByLabelText('New workspace policy default workspace'), {target: {value: '/secure/project'}});
        fireEvent.change(screen.getByLabelText('New workspace policy mode'), {target: {value: 'read_only'}});
        fireEvent.change(screen.getByLabelText('New workspace policy network mode'), {target: {value: 'none'}});
        fireEvent.click(screen.getByRole('button', {name: 'Add'}));

        await waitFor(() => expect(mockCreateWorkspacePolicy).toHaveBeenCalledWith({
            name: 'Sensitive',
            allowedRoots: ['/secure/project', '/secure/shared'],
            defaultWorkspacePath: '/secure/project',
            mode: 'read_only',
            networkMode: 'none',
            shellMode: 'ask',
            metadata: {},
        }));
    });

    test('renders load failure', async () => {
        mockGetWorkspacePolicies.mockRejectedValue(new Error('boom'));

        renderPanel();

        await screen.findByText('Failed to load workspace policies.');
    });
});
