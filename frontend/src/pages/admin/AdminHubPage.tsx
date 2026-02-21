import { useState } from 'react';
import { Tabs, type TabItem } from '@/components/navigation/Tabs';
import { Users, Shield, Settings } from 'lucide-react';

const tabs: TabItem[] = [
    { key: 'users', label: 'User Management' },
    { key: 'teams', label: 'Team Management' },
    { key: 'settings', label: 'System Settings' }
];

export default function AdminHubPage() {
    const [activeTab, setActiveTab] = useState<string>('users');

    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <h1 className="text-2xl font-bold tracking-tight">Administrator Hub</h1>
                    <p className="text-sm text-neutral-500 dark:text-neutral-400">
                        Manage users, configure teams, and adjust system settings.
                    </p>
                </div>
            </div>

            <div className="mb-6">
                <Tabs items={tabs} value={activeTab} onChange={(key) => setActiveTab(key)} />
            </div>

            <div className="mt-4">
                {activeTab === 'users' && (
                    <div className="rounded-xl border border-neutral-200 bg-white p-6 shadow-sm dark:border-neutral-800 dark:bg-neutral-900">
                        <div className="mb-4 flex items-center justify-between">
                            <h2 className="text-lg font-semibold flex items-center gap-2"><Users className="w-5 h-5" /> Users</h2>
                            <button className="rounded-md bg-brand-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-700">Add User</button>
                        </div>
                        <p className="text-sm text-neutral-500">User management interface will be implemented here.</p>
                    </div>
                )}

                {activeTab === 'teams' && (
                    <div className="rounded-xl border border-neutral-200 bg-white p-6 shadow-sm dark:border-neutral-800 dark:bg-neutral-900">
                        <div className="mb-4 flex items-center justify-between">
                            <h2 className="text-lg font-semibold flex items-center gap-2"><Shield className="w-5 h-5" /> Teams</h2>
                            <button className="rounded-md bg-brand-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-700">Create Team</button>
                        </div>
                        <p className="text-sm text-neutral-500">Team and workbasket configuration will be implemented here.</p>
                    </div>
                )}

                {activeTab === 'settings' && (
                    <div className="rounded-xl border border-neutral-200 bg-white p-6 shadow-sm dark:border-neutral-800 dark:bg-neutral-900">
                        <div className="mb-4 flex items-center justify-between">
                            <h2 className="text-lg font-semibold flex items-center gap-2"><Settings className="w-5 h-5" /> Global Settings</h2>
                        </div>
                        <p className="text-sm text-neutral-500">System-wide configurations will be placed here.</p>
                    </div>
                )}
            </div>
        </div>
    );
}
