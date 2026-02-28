import { ReactNode } from 'react';
import {
    Building2,
    ChevronRight,
    Landmark,
    Link as LinkIcon,
    MapPin,
    Search,
    ShieldCheck,
    Users,
    Banknote,
    CalendarClock,
    Briefcase
} from 'lucide-react';
import { Card, CardDescription, CardHeader, CardTitle } from '@/components/ui/Card';
import type {
    Asset,
    Collateral,
    CurrencyAmount,
    DealPayload,
    DealSecurity,
    Facility
} from '@/lib/api/types';
import { formatDate } from '@/lib/utils/date';
import { formatCurrency } from '@/lib/utils/format';

function formatAmount(amt?: CurrencyAmount) {
    if (!amt) return '--';
    return `${formatCurrency(amt.amount)} ${amt.currency}`;
}

// ----------------------------------------------------------------------------
// Reusable Tree Node Component
// ----------------------------------------------------------------------------
interface TreeNodeProps {
    title: ReactNode;
    subtitle?: ReactNode;
    icon?: ReactNode;
    badge?: ReactNode;
    defaultOpen?: boolean;
    children?: ReactNode;
    className?: string;
    isLeaf?: boolean;
}

function TreeNode({
    title,
    subtitle,
    icon,
    badge,
    defaultOpen = false,
    children,
    className = '',
    isLeaf = false
}: TreeNodeProps) {
    if (isLeaf) {
        return (
            <div className={`flex items-start gap-3 py-3 px-4 ml-6 border-l-2 border-neutral-100 dark:border-neutral-800 ${className}`}>
                <div className="mt-1 flex h-6 w-6 shrink-0 items-center justify-center rounded bg-neutral-100 text-neutral-500 dark:bg-neutral-800">
                    {icon || <div className="h-1.5 w-1.5 rounded-full bg-neutral-400" />}
                </div>
                <div className="flex-1">
                    <div className="flex flex-wrap items-start justify-between gap-4">
                        <div>
                            <div className="font-medium text-neutral-900 dark:text-neutral-100">{title}</div>
                            {subtitle && <div className="mt-1 text-sm text-neutral-500 dark:text-neutral-400">{subtitle}</div>}
                        </div>
                        {badge && <div>{badge}</div>}
                    </div>
                    {children && <div className="mt-3">{children}</div>}
                </div>
            </div>
        );
    }

    return (
        <details
            className={`group relative ml-4 pl-6 border-l-2 border-neutral-200/60 dark:border-neutral-800/60 transition-all ${className}`}
            open={defaultOpen}
        >
            <summary className="flex cursor-pointer list-none items-center gap-3 rounded-lg p-3 transition-colors hover:bg-neutral-50 dark:hover:bg-neutral-900/50 [&::-webkit-details-marker]:hidden -ml-9 relative bg-white dark:bg-transparent">
                {/* Horizontal connector line */}
                <div className="absolute top-1/2 left-0 w-4 h-0.5 bg-neutral-200/60 dark:bg-neutral-800/60 -translate-y-1/2" />

                <div className="relative z-10 flex h-6 w-6 shrink-0 items-center justify-center rounded border border-neutral-200 bg-white text-neutral-400 transition-colors group-hover:border-neutral-300 group-hover:text-neutral-600 dark:border-neutral-700 dark:bg-neutral-900 dark:group-hover:text-neutral-300">
                    <ChevronRight className="size-4 shrink-0 transition-transform duration-200 group-open:rotate-90" />
                </div>

                {icon && (
                    <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-neutral-100 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-400">
                        {icon}
                    </div>
                )}

                <div className="flex-1 flex flex-wrap items-center justify-between gap-4">
                    <div>
                        <div className="font-semibold text-neutral-900 dark:text-neutral-100">{title}</div>
                        {subtitle && <div className="text-sm text-neutral-500">{subtitle}</div>}
                    </div>
                    {badge && <div>{badge}</div>}
                </div>
            </summary>

            <div className="animate-in fade-in slide-in-from-top-2 duration-200 pb-2">
                {children}
            </div>
        </details>
    );
}

// ----------------------------------------------------------------------------
// Hierarchy Mappers
// ----------------------------------------------------------------------------

function AssetDetails({ asset, collateral }: { asset: Asset, collateral: Collateral }) {
    return (
        <TreeNode
            isLeaf
            icon={asset.assetType === 'PROPERTY' ? <MapPin className="size-3.5" /> : <Briefcase className="size-3.5" />}
            title={`${asset.assetType} ${asset.propertyType ? `(${asset.propertyType})` : ''}`}
            subtitle={
                <div className="space-y-1">
                    {asset.address && (
                        <p>{asset.address.line1}, {asset.address.suburb} {asset.address.state}</p>
                    )}
                    {asset.titleReference && <p>Title Ref: {asset.titleReference}</p>}
                    <p className="text-xs text-neutral-400">
                        Ownership: {collateral.securityProviderPartyIds.join(', ')}
                    </p>
                </div>
            }
            badge={
                <div className="text-right">
                    {asset.valuation && (
                        <div>
                            <p className="text-xs uppercase tracking-wider text-neutral-500">Valuation</p>
                            <p className="font-semibold text-neutral-900 dark:text-neutral-100">{formatAmount(asset.valuation.value)}</p>
                            <p className="text-[10px] text-neutral-400">Date: {formatDate(asset.valuation.valuationDate)}</p>
                        </div>
                    )}
                </div>
            }
        />
    );
}

function FacilityTree({ facility, security }: { facility: Facility, security?: DealSecurity }) {
    // Find linked security allocations for this specific facility
    const relatedAllocations = security?.facilityAllocations?.filter(a => a.facilityId === facility.facilityId) || [];

    // Find the actual collateral packages based on the allocations
    const relatedCollaterals = relatedAllocations.map(alloc => {
        const col = security?.collaterals?.find(c => c.collateralId === alloc.collateralId);
        return { allocation: alloc, collateral: col };
    }).filter(item => item.collateral !== undefined);

    return (
        <TreeNode
            icon={<Landmark className="size-4.5" />}
            defaultOpen
            title={
                <span className="flex items-center gap-2">
                    {facility.product.replace(/_/g, ' ')}
                    <span className="rounded bg-neutral-100 px-2 py-0.5 text-[10px] font-mono text-neutral-600 dark:bg-neutral-800 dark:text-neutral-400">
                        {facility.facilityId}
                    </span>
                </span>
            }
            subtitle={facility.purpose}
            badge={
                <div className="text-right">
                    <p className="text-lg font-bold text-neutral-900 dark:text-neutral-100">{formatAmount(facility.facilityLimit)}</p>
                    <span className="inline-block rounded-full bg-success-50 px-2 py-0.5 text-[10px] font-semibold text-success-700 uppercase tracking-widest dark:bg-success-500/10 dark:text-success-400">
                        {facility.status}
                    </span>
                </div>
            }
        >
            {/* Level 3: Schedules and Security */}
            <div className="mt-2 space-y-1">

                {/* Interest Rate Schedule */}
                {facility.pricing && (
                    <TreeNode
                        isLeaf
                        icon={<Banknote className="size-3.5" />}
                        title="Interest Rate Schedule"
                        subtitle={`${facility.pricing.rateType} Rate • Index: ${facility.pricing.index.replace(/_/g, ' ')}`}
                        badge={
                            <div className="text-right">
                                <p className="font-semibold text-neutral-900 dark:text-neutral-100">
                                    {facility.pricing.interestRate * 100}%
                                </p>
                                <p className="text-xs text-neutral-500">Margin: {facility.pricing.margin * 100}%</p>
                            </div>
                        }
                    />
                )}

                {/* Payment Schedule */}
                {facility.repayment && facility.term && (
                    <TreeNode
                        isLeaf
                        icon={<CalendarClock className="size-3.5" />}
                        title="Payment Schedule"
                        subtitle={`${facility.repayment.repaymentType.replace(/_/g, ' ')} • ${facility.repayment.frequency}`}
                        badge={
                            <div className="text-right">
                                <p className="font-semibold text-neutral-900 dark:text-neutral-100">
                                    {facility.term.termMonths} Months
                                </p>
                                <p className="text-xs text-neutral-500">Ends {formatDate(facility.term.endDate)}</p>
                            </div>
                        }
                    />
                )}

                {/* Assets & Security Package */}
                {relatedCollaterals.length > 0 ? (
                    <TreeNode
                        icon={<ShieldCheck className="size-4" />}
                        title="Assets & Security Packages"
                        subtitle={`${relatedCollaterals.length} collateral package(s) allocated to this facility`}
                        defaultOpen
                    >
                        {/* Level 4: Asset Details */}
                        {relatedCollaterals.map(({ collateral, allocation }, idx) => (
                            <div key={idx} className="mt-2 ml-4 relative">
                                <div className="mb-2 flex items-center justify-between rounded-md bg-neutral-50 px-3 py-2 text-sm dark:bg-neutral-900 border border-neutral-100 dark:border-neutral-800">
                                    <div className="flex items-center gap-2">
                                        <span className="rounded bg-brand-100 px-2 py-0.5 text-[10px] font-bold tracking-widest text-brand-700 uppercase dark:bg-brand-500/20 dark:text-brand-300">
                                            {collateral!.collateralType.replace(/_/g, ' ')}
                                        </span>
                                        <span className="text-neutral-500">Ranking: {collateral!.ranking}</span>
                                    </div>
                                    <div className="text-right text-xs">
                                        <span className="text-neutral-500 mr-2">Allocated:</span>
                                        <span className="font-semibold text-neutral-900 dark:text-neutral-100">
                                            {formatAmount(allocation.allocatedAmount)}
                                        </span>
                                    </div>
                                </div>

                                {collateral!.assets.map((asset) => (
                                    <AssetDetails key={asset.assetId} asset={asset} collateral={collateral!} />
                                ))}
                            </div>
                        ))}
                    </TreeNode>
                ) : (
                    <TreeNode
                        isLeaf
                        icon={<ShieldCheck className="size-3.5 text-neutral-300" />}
                        title="Unsecured Facility"
                        subtitle="No collateral packages allocated to this facility."
                        className="opacity-70"
                    />
                )}
            </div>
        </TreeNode>
    );
}

function DealHierarchyTree({ deal }: { deal: DealPayload }) {
    const beList = deal.borrowingEntities;
    if (!beList || beList.length === 0) return null;

    return (
        <Card>
            <CardHeader>
                <div className="flex items-center gap-2">
                    <Users className="size-5 text-brand-600 dark:text-brand-400" />
                    <div>
                        <CardTitle>Structuring Hierarchy</CardTitle>
                        <CardDescription>Borrowing entities, facilities, and underlying assets</CardDescription>
                    </div>
                </div>
            </CardHeader>

            {/* Root Container for the Tree */}
            <div className="p-4 pt-0 font-sans">
                {beList.map((be) => (
                    <div key={be.borrowingEntityId} className="mb-6 last:mb-0">
                        {/* Level 1: Borrowing Entity */}
                        <div className="flex items-center gap-3 rounded-xl border border-neutral-200 bg-neutral-50/50 p-4 dark:border-neutral-800 dark:bg-neutral-900/20 shadow-sm">
                            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-neutral-100 dark:bg-neutral-800">
                                <Building2 className="size-5 text-neutral-600 dark:text-neutral-400" />
                            </div>
                            <div className="flex-1 flex flex-wrap items-center justify-between gap-4">
                                <div>
                                    <h3 className="text-lg font-bold text-neutral-900 dark:text-neutral-100 flex items-center gap-2">
                                        {be.displayName}
                                        <span className="px-2 py-0.5 rounded text-[10px] tracking-wide bg-neutral-200/50 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-400 font-normal">
                                            {be.borrowingEntityType}
                                        </span>
                                    </h3>
                                    <p className="text-sm text-neutral-500">Entity ID: {be.borrowingEntityId}</p>
                                </div>
                                <div className="text-right max-sm:w-full">
                                    <p className="text-sm text-neutral-500 uppercase tracking-wide text-[10px]">Total Facilities</p>
                                    <p className="font-semibold text-neutral-800 dark:text-neutral-200">{be.facilities.length}</p>
                                </div>
                            </div>
                        </div>

                        {/* Level 2: Facilities */}
                        <div className="ml-5 mt-4">
                            {be.facilities.length === 0 ? (
                                <p className="text-sm text-neutral-400 italic pl-6 border-l-2 border-neutral-200 dark:border-neutral-800">
                                    No credit facilities defined.
                                </p>
                            ) : (
                                <div className="space-y-4">
                                    {be.facilities.map((fac) => (
                                        <FacilityTree key={fac.facilityId} facility={fac} security={deal.security} />
                                    ))}
                                </div>
                            )}
                        </div>
                    </div>
                ))}
            </div>
        </Card>
    );
}

// ----------------------------------------------------------------------------
// Summary Cards
// ----------------------------------------------------------------------------

function UmbrellaLimitCard({ deal }: { deal: DealPayload }) {
    const limit = deal.umbrellaLimit;
    if (!limit) return null;

    return (
        <div className="grid gap-3 sm:grid-cols-3">
            <div className="panel-muted p-4">
                <p className="text-xs uppercase tracking-wider text-neutral-500 dark:text-neutral-400">Approved Limit</p>
                <p className="mt-1 text-2xl font-bold text-neutral-900 dark:text-neutral-100">{formatAmount(limit.approvedLimit)}</p>
                <p className="text-[10px] text-neutral-400 mt-2 flex items-center gap-1">
                    <LinkIcon className="size-3" /> Umbrella: {limit.umbrellaLimitId}
                </p>
            </div>
            <div className="panel-muted p-4">
                <p className="text-xs uppercase tracking-wider text-neutral-500 dark:text-neutral-400">Current Utilisation</p>
                <p className="mt-1 text-2xl font-semibold text-neutral-900 dark:text-neutral-100">{formatAmount(limit.currentUtilisation)}</p>
            </div>
            <div className="panel-muted border border-brand-200/50 bg-brand-50 p-4 dark:border-brand-500/20 dark:bg-brand-500/10">
                <p className="text-xs uppercase tracking-wider text-brand-600 dark:text-brand-400 font-semibold">Available Headroom</p>
                <p className="mt-1 text-2xl font-bold text-brand-700 dark:text-brand-300">{formatAmount(limit.availableHeadroom)}</p>
            </div>
        </div>
    );
}

// ----------------------------------------------------------------------------
// Main Tab Component
// ----------------------------------------------------------------------------

export interface Deal360TabProps {
    dealPayload?: DealPayload;
}

export function Deal360Tab({ dealPayload }: Deal360TabProps) {
    if (!dealPayload || !dealPayload.dealId) {
        return (
            <div className="flex h-64 flex-col items-center justify-center rounded-xl border border-dashed border-neutral-300 bg-neutral-50 p-8 text-center dark:border-neutral-700 dark:bg-neutral-900/50">
                <Search className="size-10 text-neutral-400 mb-4" />
                <h3 className="text-lg font-semibold text-neutral-900 dark:text-neutral-100">No Deal Data Found</h3>
                <p className="mt-2 max-w-sm text-sm text-neutral-500 dark:text-neutral-400">
                    This case does not have complete 360-degree deal metadata available in its payload.
                </p>
            </div>
        );
    }

    return (
        <div className="space-y-6 animate-in fade-in slide-in-from-bottom-2 duration-300">

            {/* Header */}
            <div className="flex flex-wrap items-center justify-between gap-4 py-2 border-b border-neutral-200 pb-4 dark:border-neutral-800">
                <div className="flex items-center gap-4">
                    <div className="h-12 w-12 rounded-full bg-brand-100 dark:bg-brand-500/20 flex items-center justify-center text-brand-600 dark:text-brand-400">
                        <Briefcase className="size-6" />
                    </div>
                    <div>
                        <h2 className="text-2xl font-bold text-neutral-900 dark:text-neutral-50">{dealPayload.dealName}</h2>
                        <p className="text-sm text-neutral-500 flex items-center gap-2">
                            <span className="font-mono bg-neutral-100 px-1.5 py-0.5 rounded dark:bg-neutral-800">{dealPayload.dealId}</span>
                            <span>•</span>
                            <span>Banker: {dealPayload.banker?.bankerId ?? 'Unassigned'}</span>
                        </p>
                    </div>
                </div>
                <div className="text-right">
                    <span className="rounded-full bg-neutral-900 px-3 py-1 font-semibold text-white dark:bg-neutral-100 dark:text-neutral-900 shadow-sm">
                        {dealPayload.status}
                    </span>
                </div>
            </div>

            <UmbrellaLimitCard deal={dealPayload} />

            {/* Unified Tree Hierarchy */}
            <DealHierarchyTree deal={dealPayload} />

        </div>
    );
}
