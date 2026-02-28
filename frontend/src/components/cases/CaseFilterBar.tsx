import { useState, useRef, useEffect } from 'react';
import {
    Search, X, ChevronDown, ChevronUp, BellRing, AlertTriangle, Star, Clock
} from 'lucide-react';
import { useCaseListStore } from '@/stores/caseListStore';
import { StatusBadge } from './StatusBadge';
import { PriorityBadge } from './PriorityBadge';
import type { CaseStatus, CasePriority, CaseComplexity, SkillCode } from '@/types/cases';
import { cn } from '@/lib/cn';

const ALL_STATUSES: CaseStatus[] = [
    'NEW', 'ALLOCATABLE', 'IN_PROGRESS', 'ADDITIONAL_INFO',
    'IN_QA', 'REMEDIATION', 'COMPLETED', 'WITHDRAWN', 'ON_HOLD'
];

const ALL_PRIORITIES: CasePriority[] = [
    'URGENT', 'HIGH', 'MEDIUM', 'LOW'
];

const ALL_COMPLEXITIES: CaseComplexity[] = [
    'SIMPLE', 'STANDARD_1', 'STANDARD_2', 'STANDARD_3', 'NON_STANDARD'
];

const ALL_SKILLS: SkillCode[] = [
    'CREDIT_ANALYST', 'FRAUD_SPECIALIST', 'SENIOR_UNDERWRITER',
    'LEGAL_REVIEWER', 'KYC_ANALYST', 'COMPLIANCE_OFFICER',
    'VALUATION_EXPERT', 'RELATIONSHIP_MANAGER', 'QUALITY_ASSURANCE'
];

export function CaseFilterBar() {
    const store = useCaseListStore();
    const [statusOpen, setStatusOpen] = useState(false);
    const [priorityOpen, setPriorityOpen] = useState(false);
    const [skillOpen, setSkillOpen] = useState(false);

    const statusRef = useRef<HTMLDivElement>(null);
    const priorityRef = useRef<HTMLDivElement>(null);
    const skillRef = useRef<HTMLDivElement>(null);
    const searchInputRef = useRef<HTMLInputElement>(null);

    useEffect(() => {
        function handleClickOutside(event: MouseEvent) {
            if (statusRef.current && !statusRef.current.contains(event.target as Node)) setStatusOpen(false);
            if (priorityRef.current && !priorityRef.current.contains(event.target as Node)) setPriorityOpen(false);
            if (skillRef.current && !skillRef.current.contains(event.target as Node)) setSkillOpen(false);
        }
        document.addEventListener('mousedown', handleClickOutside);
        return () => document.removeEventListener('mousedown', handleClickOutside);
    }, []);

    const hasAdvancedFilters =
        store.filters.complexities.length > 0 ||
        store.filters.skillCodes.length > 0 ||
        store.filters.assignedToMe ||
        store.filters.hasBlockingErrors ||
        store.filters.isVip ||
        !!store.filters.slaDueBefore ||
        !!store.filters.createdAfter ||
        !!store.filters.createdBefore ||
        !!store.filters.teamId;

    const hasAnyFilters = store.filters.search !== '' || store.filters.statuses.length > 0 || store.filters.priorities.length > 0 || hasAdvancedFilters;

    const toggleStatus = (status: CaseStatus) => {
        const next = store.filters.statuses.includes(status)
            ? store.filters.statuses.filter(s => s !== status)
            : [...store.filters.statuses, status];
        store.setStatusFilter(next);
    };

    const togglePriority = (priority: CasePriority) => {
        const next = store.filters.priorities.includes(priority)
            ? store.filters.priorities.filter(p => p !== priority)
            : [...store.filters.priorities, priority];
        store.setPriorityFilter(next);
    };

    const toggleComplexity = (comp: CaseComplexity) => {
        const next = store.filters.complexities.includes(comp)
            ? store.filters.complexities.filter(c => c !== comp)
            : [...store.filters.complexities, comp];
        store.setAdvancedFilter('complexities', next);
    };

    const toggleSkill = (skill: SkillCode) => {
        const next = store.filters.skillCodes.includes(skill)
            ? store.filters.skillCodes.filter(s => s !== skill)
            : [...store.filters.skillCodes, skill];
        store.setAdvancedFilter('skillCodes', next);
    };

    return (
        <div className="w-full space-y-4">
            {/* Primary Filter Row */}
            <div className="flex flex-col md:flex-row items-center gap-4 w-full relative z-30">

                {/* Search */}
                <div className="relative flex-grow w-full md:w-auto">
                    <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none">
                        <Search className="h-5 w-5 text-gray-400" />
                    </div>
                    <input
                        ref={searchInputRef}
                        type="text"
                        className="block w-full pl-10 pr-10 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-blue-500 focus:outline-none sm:text-sm transition-all"
                        placeholder="Search cases..."
                        value={store.filters.search}
                        onChange={(e) => store.setSearch(e.target.value)}
                        onKeyDown={(e) => {
                            if (e.key === 'Escape') {
                                store.setSearch('');
                                searchInputRef.current?.blur();
                            }
                        }}
                    />
                    {store.filters.search && (
                        <button
                            onClick={() => { store.setSearch(''); searchInputRef.current?.focus(); }}
                            className="absolute inset-y-0 right-0 pr-3 flex items-center text-gray-400 hover:text-gray-600 focus:outline-none"
                            aria-label="Clear search"
                        >
                            <X className="h-4 w-4" />
                        </button>
                    )}
                </div>

                {/* Dropdowns & Toggles */}
                <div className="flex items-center gap-3 w-full md:w-auto overflow-x-auto pb-2 md:pb-0 shrink-0">

                    {/* Status Dropdown */}
                    <div className="relative shrink-0" ref={statusRef}>
                        <button
                            onClick={() => setStatusOpen(!statusOpen)}
                            className="px-4 py-2 bg-white border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50 focus:outline-none focus:ring-2 focus:ring-blue-500 flex items-center gap-2 text-sm font-medium transition-colors"
                            aria-expanded={statusOpen}
                            aria-haspopup="listbox"
                        >
                            {store.filters.statuses.length === 0
                                ? 'All Statuses'
                                : store.filters.statuses.length === 1
                                    ? store.filters.statuses[0].replace('_', ' ')
                                    : `${store.filters.statuses.length} Statuses`}
                            <ChevronDown className={cn("h-4 w-4 text-gray-400 transition-transform", statusOpen && "rotate-180")} />
                        </button>
                        {statusOpen && (
                            <div className="absolute z-50 mt-2 w-64 bg-white border border-gray-100 rounded-xl shadow-lg py-1 max-h-80 overflow-y-auto left-0 md:right-0 md:left-auto focus:outline-none" role="listbox">
                                {ALL_STATUSES.map(status => (
                                    <label key={status} className="flex items-center px-4 py-2 hover:bg-gray-50 cursor-pointer">
                                        <input
                                            type="checkbox"
                                            className="form-checkbox h-4 w-4 text-blue-600 rounded border-gray-300 focus:ring-blue-500 mr-3"
                                            checked={store.filters.statuses.includes(status)}
                                            onChange={() => toggleStatus(status)}
                                        />
                                        <StatusBadge status={status} size="sm" />
                                    </label>
                                ))}
                            </div>
                        )}
                    </div>

                    {/* Priority Dropdown */}
                    <div className="relative shrink-0" ref={priorityRef}>
                        <button
                            onClick={() => setPriorityOpen(!priorityOpen)}
                            className="px-4 py-2 bg-white border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50 focus:outline-none focus:ring-2 focus:ring-blue-500 flex items-center gap-2 text-sm font-medium transition-colors"
                        >
                            {store.filters.priorities.length === 0
                                ? 'All Priorities'
                                : store.filters.priorities.length === 1
                                    ? store.filters.priorities[0]
                                    : `${store.filters.priorities.length} Priorities`}
                            <ChevronDown className={cn("h-4 w-4 text-gray-400 transition-transform", priorityOpen && "rotate-180")} />
                        </button>
                        {priorityOpen && (
                            <div className="absolute z-50 mt-2 w-56 bg-white border border-gray-100 rounded-xl shadow-lg py-1 left-0 md:right-0 md:left-auto">
                                {ALL_PRIORITIES.map(priority => (
                                    <label key={priority} className="flex items-center px-4 py-2 hover:bg-gray-50 cursor-pointer">
                                        <input
                                            type="checkbox"
                                            className="form-checkbox h-4 w-4 text-blue-600 rounded border-gray-300 focus:ring-blue-500 mr-3"
                                            checked={store.filters.priorities.includes(priority)}
                                            onChange={() => togglePriority(priority)}
                                        />
                                        <PriorityBadge priority={priority} size="sm" />
                                    </label>
                                ))}
                            </div>
                        )}
                    </div>

                    {/* Advanced Filters Toggle */}
                    <button
                        onClick={store.toggleAdvancedFilters}
                        className="px-4 py-2 bg-white border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50 focus:outline-none focus:ring-2 focus:ring-blue-500 flex items-center gap-2 text-sm font-medium transition-colors relative shrink-0"
                    >
                        {store.advancedFiltersOpen ? 'Less Filters' : 'More Filters'}
                        {store.advancedFiltersOpen ? (
                            <ChevronUp className="h-4 w-4 text-gray-400" />
                        ) : (
                            <ChevronDown className="h-4 w-4 text-gray-400" />
                        )}
                        {hasAdvancedFilters && (
                            <span className="absolute -top-1 -right-1 flex h-3 w-3">
                                <span className="relative inline-flex rounded-full h-3 w-3 bg-blue-600 border-2 border-white"></span>
                            </span>
                        )}
                    </button>
                </div>
            </div>

            {/* Advanced Filters Row */}
            <div
                className={cn(
                    "overflow-hidden transition-all duration-300 ease-in-out z-20 relative",
                    store.advancedFiltersOpen ? "max-h-[800px] opacity-100 mt-4" : "max-h-0 opacity-0 mt-0"
                )}
            >
                <div className="bg-white p-5 rounded-xl border border-gray-100 shadow-sm flex flex-col gap-5">

                    <div className="flex flex-wrap items-center gap-6">
                        <div className="space-y-2 flex-grow max-w-2xl">
                            <label className="text-xs font-semibold text-gray-500 uppercase tracking-wider">Complexity</label>
                            <div className="flex flex-wrap gap-2">
                                {ALL_COMPLEXITIES.map(comp => {
                                    const isSelected = store.filters.complexities.includes(comp);
                                    return (
                                        <button
                                            key={comp}
                                            onClick={() => toggleComplexity(comp)}
                                            className={cn(
                                                "text-xs px-3 py-1.5 rounded-full border font-medium transition-colors focus:outline-none focus:ring-2 focus:ring-offset-1 focus:ring-blue-500",
                                                isSelected
                                                    ? "bg-purple-100 text-purple-700 border-purple-300 hover:bg-purple-200"
                                                    : "bg-white text-gray-600 border-gray-200 hover:bg-gray-50"
                                            )}
                                        >
                                            {comp.replace('_', ' ')}
                                        </button>
                                    );
                                })}
                            </div>
                        </div>

                        <div className="space-y-2 min-w-[200px]" ref={skillRef}>
                            <label className="text-xs font-semibold text-gray-500 uppercase tracking-wider">Required Skills</label>
                            <div className="relative">
                                <button
                                    onClick={() => setSkillOpen(!skillOpen)}
                                    className="w-full text-left px-3 py-1.5 bg-white border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50 flex items-center justify-between text-sm transition-colors"
                                >
                                    {store.filters.skillCodes.length === 0 ? 'Any Skill' : `${store.filters.skillCodes.length} Skills Selected`}
                                    <ChevronDown className="h-4 w-4 text-gray-400" />
                                </button>
                                {skillOpen && (
                                    <div className="absolute z-50 mt-1 w-full bg-white border border-gray-100 rounded-xl shadow-lg py-1 max-h-60 overflow-y-auto left-0">
                                        {ALL_SKILLS.map(skill => (
                                            <label key={skill} className="flex items-center px-4 py-2 hover:bg-gray-50 cursor-pointer text-sm">
                                                <input
                                                    type="checkbox"
                                                    className="form-checkbox h-4 w-4 text-blue-600 rounded border-gray-300 mr-3 focus:ring-blue-500"
                                                    checked={store.filters.skillCodes.includes(skill)}
                                                    onChange={() => toggleSkill(skill)}
                                                />
                                                {skill.replace('_', ' ')}
                                            </label>
                                        ))}
                                    </div>
                                )}
                            </div>
                        </div>
                    </div>

                    <div className="flex flex-wrap gap-3">
                        <button
                            onClick={() => store.setAdvancedFilter('assignedToMe', !store.filters.assignedToMe)}
                            className={cn(
                                "inline-flex items-center gap-2 px-3 py-1.5 rounded-full text-sm font-medium border transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-1",
                                store.filters.assignedToMe ? "bg-blue-600 text-white border-blue-600 hover:bg-blue-700" : "bg-white text-gray-700 border-gray-300 hover:bg-gray-50"
                            )}
                        >
                            <BellRing className="w-4 h-4" /> Assigned to Me
                        </button>
                        <button
                            onClick={() => store.setAdvancedFilter('hasBlockingErrors', !store.filters.hasBlockingErrors)}
                            className={cn(
                                "inline-flex items-center gap-2 px-3 py-1.5 rounded-full text-sm font-medium border transition-colors focus:outline-none focus:ring-2 focus:ring-red-500 focus:ring-offset-1",
                                store.filters.hasBlockingErrors ? "bg-red-600 text-white border-red-600 hover:bg-red-700" : "bg-white text-gray-700 border-gray-300 hover:bg-gray-50"
                            )}
                        >
                            <AlertTriangle className="w-4 h-4" /> Has Blocking Errors
                        </button>
                        <button
                            onClick={() => store.setAdvancedFilter('isVip', !store.filters.isVip)}
                            className={cn(
                                "inline-flex items-center gap-2 px-3 py-1.5 rounded-full text-sm font-medium border transition-colors focus:outline-none focus:ring-2 focus:ring-amber-500 focus:ring-offset-1",
                                store.filters.isVip ? "bg-amber-500 text-white border-amber-500 hover:bg-amber-600" : "bg-white text-gray-700 border-gray-300 hover:bg-gray-50"
                            )}
                        >
                            <Star className="w-4 h-4" /> VIP Cases Only
                        </button>
                        <button
                            onClick={() => {
                                if (store.filters.slaDueBefore) {
                                    store.setAdvancedFilter('slaDueBefore', null);
                                } else {
                                    store.setAdvancedFilter('slaDueBefore', new Date(Date.now() + 4 * 60 * 60 * 1000).toISOString());
                                }
                            }}
                            className={cn(
                                "inline-flex items-center gap-2 px-3 py-1.5 rounded-full text-sm font-medium border transition-colors focus:outline-none focus:ring-2 focus:ring-orange-500 focus:ring-offset-1",
                                store.filters.slaDueBefore ? "bg-orange-500 text-white border-orange-500 hover:bg-orange-600" : "bg-white text-gray-700 border-gray-300 hover:bg-gray-50"
                            )}
                        >
                            <Clock className="w-4 h-4" /> SLA at Risk
                        </button>
                    </div>

                    <div className="flex flex-wrap items-end gap-x-6 gap-y-4 pt-3 border-t border-gray-100">
                        <div className="space-y-1">
                            <label className="text-xs text-gray-500 font-medium">Created From</label>
                            <input
                                type="date"
                                value={store.filters.createdAfter?.split('T')[0] || ''}
                                onChange={(e) => store.setAdvancedFilter('createdAfter', e.target.value ? new Date(e.target.value).toISOString() : null)}
                                className="block w-full px-3 py-1.5 text-sm border border-gray-300 rounded-md focus:ring-blue-500 focus:border-blue-500 outline-none"
                            />
                        </div>
                        <div className="space-y-1">
                            <label className="text-xs text-gray-500 font-medium">Created To</label>
                            <input
                                type="date"
                                value={store.filters.createdBefore?.split('T')[0] || ''}
                                onChange={(e) => store.setAdvancedFilter('createdBefore', e.target.value ? new Date(e.target.value).toISOString() : null)}
                                className="block w-full px-3 py-1.5 text-sm border border-gray-300 rounded-md focus:ring-blue-500 focus:border-blue-500 outline-none"
                            />
                        </div>
                        <div className="space-y-1">
                            <label className="text-xs text-gray-500 font-medium">Team API ID</label>
                            <input
                                type="text"
                                placeholder="All Teams"
                                value={store.filters.teamId || ''}
                                onChange={(e) => store.setAdvancedFilter('teamId', e.target.value || null)}
                                className="block w-full px-3 py-1.5 text-sm border border-gray-300 rounded-md focus:ring-blue-500 focus:border-blue-500 outline-none"
                            />
                        </div>
                        {hasAnyFilters && (
                            <div className="ml-auto">
                                <button
                                    onClick={store.resetFilters}
                                    className="text-sm text-blue-600 hover:underline hover:text-blue-800 font-medium px-2 py-1.5 focus:outline-none focus:ring-2 focus:ring-blue-500 rounded"
                                >
                                    Reset all filters
                                </button>
                            </div>
                        )}
                    </div>
                </div>
            </div>

            {/* Active Filter Summary Row */}
            {hasAnyFilters && (
                <div className="flex flex-wrap items-center gap-2 pt-1 relative z-10">
                    <span className="text-xs text-gray-500 font-medium mr-1">Active:</span>

                    {store.filters.search && (
                        <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-gray-100 text-gray-700 border border-gray-200">
                            Search: "{store.filters.search}"
                            <button aria-label="Remove search filter" onClick={() => store.setSearch('')} className="hover:text-gray-900 focus:outline-none"><X className="w-3 h-3" /></button>
                        </span>
                    )}

                    {store.filters.statuses.map(s => (
                        <span key={s} className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-blue-50 text-blue-700 border border-blue-100">
                            Status: {s.replace('_', ' ')}
                            <button aria-label={`Remove status ${s}`} onClick={() => toggleStatus(s)} className="hover:text-blue-900 focus:outline-none"><X className="w-3 h-3" /></button>
                        </span>
                    ))}

                    {store.filters.priorities.map(p => (
                        <span key={p} className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-blue-50 text-blue-700 border border-blue-100">
                            Priority: {p}
                            <button aria-label={`Remove priority ${p}`} onClick={() => togglePriority(p)} className="hover:text-blue-900 focus:outline-none"><X className="w-3 h-3" /></button>
                        </span>
                    ))}

                    {store.filters.complexities.map(c => (
                        <span key={c} className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-purple-50 text-purple-700 border border-purple-100">
                            {c.replace('_', ' ')}
                            <button aria-label={`Remove complexity ${c}`} onClick={() => toggleComplexity(c)} className="hover:text-purple-900 focus:outline-none"><X className="w-3 h-3" /></button>
                        </span>
                    ))}

                    {store.filters.assignedToMe && (
                        <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-blue-50 text-blue-700 border border-blue-100">
                            Assigned to Me
                            <button aria-label="Remove Assigned filter" onClick={() => store.setAdvancedFilter('assignedToMe', false)} className="hover:text-blue-900 focus:outline-none"><X className="w-3 h-3" /></button>
                        </span>
                    )}

                    {store.filters.hasBlockingErrors && (
                        <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-red-50 text-red-700 border border-red-100">
                            Has Errors
                            <button aria-label="Remove Errors filter" onClick={() => store.setAdvancedFilter('hasBlockingErrors', false)} className="hover:text-red-900 focus:outline-none"><X className="w-3 h-3" /></button>
                        </span>
                    )}

                    {store.filters.isVip && (
                        <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-amber-50 text-amber-700 border border-amber-100">
                            VIP Only
                            <button aria-label="Remove VIP filter" onClick={() => store.setAdvancedFilter('isVip', false)} className="hover:text-amber-900 focus:outline-none"><X className="w-3 h-3" /></button>
                        </span>
                    )}

                    {store.filters.slaDueBefore && (
                        <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-orange-50 text-orange-700 border border-orange-100">
                            SLA Risk
                            <button aria-label="Remove SLA filter" onClick={() => store.setAdvancedFilter('slaDueBefore', null)} className="hover:text-orange-900 focus:outline-none"><X className="w-3 h-3" /></button>
                        </span>
                    )}

                    <button onClick={store.resetFilters} className="text-xs text-gray-500 hover:text-gray-900 underline ml-2 py-1 px-1 focus:outline-none">
                        Clear all
                    </button>
                </div>
            )}
        </div>
    );
}
