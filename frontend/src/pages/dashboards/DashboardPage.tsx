import { useState } from 'react';
import { Tabs, type TabItem } from '@/components/navigation/Tabs';
import PersonalDashboardPage from '@/pages/dashboards/PersonalDashboardPage';
import TeamDashboardPage from '@/pages/dashboards/TeamDashboardPage';
import ComplianceDashboardPage from '@/pages/dashboards/ComplianceDashboardPage';

const tabs: TabItem[] = [
    { key: 'personal', label: 'Personal Dashboard' },
    { key: 'team', label: 'Team Dashboard' },
    { key: 'compliance', label: 'Compliance & Audit' },
];

export default function DashboardPage() {
    const [activeTab, setActiveTab] = useState<string>('personal');

    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <h1 className="text-2xl font-bold tracking-tight">Unified Dashboard</h1>
                    <p className="text-sm text-neutral-500 dark:text-neutral-400">
                        Overview of your personal tasks, team workload, and compliance metrics.
                    </p>
                </div>
            </div>

            <div className="mb-6">
                <Tabs items={tabs} value={activeTab} onChange={(key) => setActiveTab(key)} />
            </div>

            <div className="mt-4">
                {activeTab === 'personal' && <PersonalDashboardPage disableHeader />}
                {activeTab === 'team' && <TeamDashboardPage disableHeader />}
                {activeTab === 'compliance' && <ComplianceDashboardPage disableHeader />}
            </div>
        </div>
    );
}
