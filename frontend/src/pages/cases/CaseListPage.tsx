import { type ColumnDef } from '@tanstack/react-table';
import {
  ArrowUpDown,
  ChevronDown,
  Download,
  Filter,
  FolderSearch,
  Tags,
  Users
} from 'lucide-react';
import { useMemo, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { toast } from 'sonner';
import { SLAIndicator } from '@/components/domain/SLAIndicator';
import { PriorityBadge } from '@/components/domain/PriorityBadge';
import { StatusBadge } from '@/components/domain/StatusBadge';
import { Button } from '@/components/ui/Button';
import { Card, CardDescription, CardHeader, CardTitle } from '@/components/ui/Card';
import { DataTable } from '@/components/ui/DataTable';
import { EmptyState } from '@/components/ui/EmptyState';
import { SelectInput, TextInput } from '@/components/ui/FormField';
import { TableSkeleton } from '@/components/ui/LoadingSkeleton';
import { Pagination } from '@/components/ui/Pagination';
import { useCases } from '@/hooks/use-cases';
import { useDebounce } from '@/hooks/use-debounce';
import { cn } from '@/lib/cn';
import type { CaseFilters, CaseListItem, CaseStatus, Priority, SlaState } from '@/lib/api/types';
import { formatDateTime } from '@/lib/utils/date';
import { useUiStore } from '@/store/ui-store';

const scopeTabs: Array<{ key: CaseFilters['scope']; label: string }> = [
  { key: 'my', label: 'My Cases' },
  { key: 'team', label: "Team Cases" },
  { key: 'all', label: 'All Cases' }
];

const statusOptions: CaseStatus[] = [
  'DRAFT',
  'IN_PROGRESS',
  'PENDING_APPROVAL',
  'SUSPENDED',
  'WITHDRAWN',
  'CLOSED',
  'REJECTED'
];

const priorityOptions: Priority[] = ['CRITICAL', 'HIGH', 'NORMAL', 'LOW'];
const slaOptions: SlaState[] = ['ON_TRACK', 'WARNING', 'BREACHED'];
const stageOptions = [
  'INTAKE',
  'DOCUMENT_COLLECTION',
  'CREDIT_REVIEW',
  'UNDERWRITING',
  'FINAL_APPROVAL',
  'DISBURSEMENT'
];

function parseArrayParam(param: string | null) {
  if (!param) {
    return undefined;
  }

  const values = param
    .split(',')
    .map((value) => value.trim())
    .filter(Boolean);

  return values.length ? values : undefined;
}

function getScopeFromParams(value: string | null): CaseFilters['scope'] {
  if (value === 'team' || value === 'all') {
    return value;
  }

  return 'my';
}

export default function CaseListPage() {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [selectedCaseIds, setSelectedCaseIds] = useState<Set<string>>(new Set());
  const storeScope = useUiStore((state) => state.caseScope);
  const setStoreScope = useUiStore((state) => state.setCaseScope);

  const page = Number(searchParams.get('page') ?? '1');
  const limit = Number(searchParams.get('limit') ?? '25');
  const scope = getScopeFromParams(searchParams.get('scope') ?? storeScope);

  const searchText = searchParams.get('query') ?? '';
  const debouncedSearch = useDebounce(searchText, 300);

  const filters: CaseFilters = {
    scope,
    status: parseArrayParam(searchParams.get('status')) as CaseStatus[] | undefined,
    stage: parseArrayParam(searchParams.get('stage')),
    caseType: searchParams.get('caseType') ?? undefined,
    dateFrom: searchParams.get('dateFrom') ?? undefined,
    dateTo: searchParams.get('dateTo') ?? undefined,
    query: debouncedSearch,
    slaStatus: parseArrayParam(searchParams.get('slaStatus')) as SlaState[] | undefined,
    priority: parseArrayParam(searchParams.get('priority')) as Priority[] | undefined,
    assignedTo: searchParams.get('assignedTo') ?? undefined,
    tags: parseArrayParam(searchParams.get('tags')),
    page,
    limit
  };

  const casesQuery = useCases(filters);

  const updateParam = (key: string, value: string | number | undefined) => {
    const next = new URLSearchParams(searchParams);

    if (value === undefined || value === '') {
      next.delete(key);
    } else {
      next.set(key, String(value));
    }

    if (key !== 'page') {
      next.set('page', '1');
    }

    setSearchParams(next);
  };

  const columns = useMemo<Array<ColumnDef<CaseListItem>>>(
    () => [
      {
        accessorKey: 'referenceNumber',
        header: 'Reference #',
        cell: ({ row }) => (
          <p className="font-mono text-xs font-semibold text-brand-700 dark:text-brand-200">
            {row.original.referenceNumber}
          </p>
        )
      },
      {
        accessorKey: 'borrowerName',
        header: 'Borrower',
        cell: ({ row }) => <span className="font-medium">{row.original.borrowerName}</span>
      },
      {
        accessorKey: 'caseType',
        header: 'Case Type'
      },
      {
        accessorKey: 'stage',
        header: 'Current Stage'
      },
      {
        accessorKey: 'status',
        header: 'Status',
        cell: ({ row }) => <StatusBadge status={row.original.status} />
      },
      {
        accessorKey: 'priority',
        header: 'Priority',
        cell: ({ row }) => <PriorityBadge priority={row.original.priority} />
      },
      {
        accessorKey: 'assignedTo',
        header: 'Assigned To',
        cell: ({ row }) => row.original.assignedTo?.displayName ?? '--'
      },
      {
        accessorKey: 'slaStatus',
        header: 'SLA Status',
        cell: ({ row }) => (
          <SLAIndicator status={row.original.slaStatus} remainingMinutes={row.original.slaRemainingMinutes} />
        )
      },
      {
        accessorKey: 'createdAt',
        header: 'Created At',
        cell: ({ row }) => formatDateTime(row.original.createdAt)
      },
      {
        accessorKey: 'updatedAt',
        header: 'Updated At',
        cell: ({ row }) => formatDateTime(row.original.updatedAt)
      },
      {
        id: 'actions',
        header: 'Actions',
        enableSorting: false,
        cell: ({ row }) => (
          <details className="relative" onClick={(event) => event.stopPropagation()}>
            <summary
              className="inline-flex cursor-pointer list-none items-center gap-1 rounded-md border border-neutral-200 px-2 py-1 text-xs text-neutral-600 hover:bg-neutral-100 dark:border-neutral-700 dark:text-neutral-300 dark:hover:bg-neutral-800"
              aria-label={`Actions for case ${row.original.referenceNumber}`}
            >
              Quick
              <ChevronDown className="size-3" aria-hidden="true" />
            </summary>
            <div className="absolute right-0 z-20 mt-1 w-36 rounded-lg border border-neutral-200 bg-white p-1 shadow-lg dark:border-neutral-700 dark:bg-neutral-900">
              <button
                type="button"
                className="block w-full rounded-md px-2 py-1.5 text-left text-xs hover:bg-neutral-100 dark:hover:bg-neutral-800"
                onClick={() => navigate(`/cases/${row.original.id}`)}
              >
                View
              </button>
              <button
                type="button"
                className="block w-full rounded-md px-2 py-1.5 text-left text-xs hover:bg-neutral-100 dark:hover:bg-neutral-800"
                onClick={() => toast.success('Case claimed')}
              >
                Claim
              </button>
              <button
                type="button"
                className="block w-full rounded-md px-2 py-1.5 text-left text-xs hover:bg-neutral-100 dark:hover:bg-neutral-800"
                onClick={() => toast.info('Reassign flow opened')}
              >
                Reassign
              </button>
            </div>
          </details>
        )
      }
    ],
    [navigate]
  );

  const items = casesQuery.data?.items ?? [];
  const total = casesQuery.data?.total ?? 0;

  const selectedCount = selectedCaseIds.size;

  const handleRowSelect = (row: CaseListItem, selected: boolean) => {
    setSelectedCaseIds((previous) => {
      const next = new Set(previous);
      if (selected) {
        next.add(row.id);
      } else {
        next.delete(row.id);
      }
      return next;
    });
  };

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader className="mb-4 flex-col gap-3 lg:flex-row lg:items-center">
          <div>
            <CardTitle>Case Workbench</CardTitle>
            <CardDescription>Prioritize active cases, meet SLAs, and complete workflows faster.</CardDescription>
          </div>
          <div className="ml-auto flex items-center gap-2">
            <Button variant="secondary" size="sm">
              <ArrowUpDown className="size-4" aria-hidden="true" />
              Sort presets
            </Button>
            <Button size="sm" onClick={() => toast.info('Export queued')}>
              <Download className="size-4" aria-hidden="true" />
              Export
            </Button>
          </div>
        </CardHeader>

        <div className="space-y-4">
          <div className="flex flex-wrap gap-2">
            {scopeTabs.map((tab) => (
              <button
                key={tab.key}
                type="button"
                className={cn(
                  'rounded-full border px-3 py-1.5 text-sm font-medium transition',
                  scope === tab.key
                    ? 'border-brand-200 bg-brand-50 text-brand-700 dark:border-brand-400/40 dark:bg-brand-500/15 dark:text-brand-200'
                    : 'border-neutral-200 text-neutral-600 hover:bg-neutral-100 dark:border-neutral-700 dark:text-neutral-300 dark:hover:bg-neutral-800'
                )}
                onClick={() => {
                  setStoreScope(tab.key);
                  updateParam('scope', tab.key);
                }}
              >
                {tab.label}
              </button>
            ))}
          </div>

          <div className="grid grid-cols-1 gap-3 lg:grid-cols-6">
            <SelectInput
              value={parseArrayParam(searchParams.get('status')) ?? []}
              onChange={(event) => {
                const selected = Array.from(event.target.selectedOptions).map((option) => option.value);
                updateParam('status', selected.join(','));
              }}
              aria-label="Status filter"
              multiple
              className="h-[7.25rem]"
            >
              {statusOptions.map((status) => (
                <option key={status} value={status}>
                  {status.replaceAll('_', ' ')}
                </option>
              ))}
            </SelectInput>

            <SelectInput
              value={parseArrayParam(searchParams.get('stage')) ?? []}
              onChange={(event) => {
                const selected = Array.from(event.target.selectedOptions).map((option) => option.value);
                updateParam('stage', selected.join(','));
              }}
              aria-label="Stage filter"
              multiple
              className="h-[7.25rem]"
            >
              {stageOptions.map((stage) => (
                <option key={stage} value={stage}>
                  {stage.replaceAll('_', ' ')}
                </option>
              ))}
            </SelectInput>

            <TextInput
              value={searchParams.get('caseType') ?? ''}
              onChange={(event) => updateParam('caseType', event.target.value)}
              placeholder="Case type"
              aria-label="Case type filter"
            />

            <TextInput
              type="date"
              value={searchParams.get('dateFrom') ?? ''}
              onChange={(event) => updateParam('dateFrom', event.target.value)}
              aria-label="Created from"
            />

            <TextInput
              type="date"
              value={searchParams.get('dateTo') ?? ''}
              onChange={(event) => updateParam('dateTo', event.target.value)}
              aria-label="Created to"
            />

            <TextInput
              value={searchText}
              onChange={(event) => updateParam('query', event.target.value)}
              placeholder="Search ref # or borrower"
              aria-label="Search query"
            />
          </div>

          <div className="flex flex-wrap items-center justify-between gap-3">
            <Button variant="ghost" size="sm" onClick={() => setShowAdvanced((current) => !current)}>
              <Filter className="size-4" aria-hidden="true" />
              Advanced filters
            </Button>

            {selectedCount > 0 ? (
              <div className="flex items-center gap-2 rounded-lg border border-brand-200 bg-brand-50 px-3 py-1.5 text-xs dark:border-brand-500/40 dark:bg-brand-500/10">
                <span>{selectedCount} selected</span>
                <Button variant="ghost" size="sm" onClick={() => toast.success('Cases reassigned')}>
                  <Users className="size-3.5" aria-hidden="true" />
                  Reassign
                </Button>
                <Button variant="ghost" size="sm" onClick={() => toast.success('Tags updated')}>
                  <Tags className="size-3.5" aria-hidden="true" />
                  Tag
                </Button>
                <Button variant="ghost" size="sm" onClick={() => toast.success('Export started')}>
                  <Download className="size-3.5" aria-hidden="true" />
                  Export
                </Button>
              </div>
            ) : null}
          </div>

          {showAdvanced ? (
            <div className="grid grid-cols-1 gap-3 rounded-xl border border-dashed border-neutral-300 p-3 md:grid-cols-4 dark:border-neutral-700">
              <SelectInput
                value={searchParams.get('slaStatus') ?? ''}
                onChange={(event) => updateParam('slaStatus', event.target.value)}
                aria-label="SLA status filter"
              >
                <option value="">All SLA states</option>
                {slaOptions.map((sla) => (
                  <option key={sla} value={sla}>
                    {sla.replaceAll('_', ' ')}
                  </option>
                ))}
              </SelectInput>

              <SelectInput
                value={searchParams.get('priority') ?? ''}
                onChange={(event) => updateParam('priority', event.target.value)}
                aria-label="Priority filter"
              >
                <option value="">All priorities</option>
                {priorityOptions.map((priority) => (
                  <option key={priority} value={priority}>
                    {priority}
                  </option>
                ))}
              </SelectInput>

              <TextInput
                value={searchParams.get('assignedTo') ?? ''}
                onChange={(event) => updateParam('assignedTo', event.target.value)}
                placeholder="Assigned to user ID"
                aria-label="Assigned to filter"
              />

              <TextInput
                value={searchParams.get('tags') ?? ''}
                onChange={(event) => updateParam('tags', event.target.value)}
                placeholder="Tags (comma separated)"
                aria-label="Tag filter"
              />
            </div>
          ) : null}
        </div>
      </Card>

      {casesQuery.isLoading ? (
        <TableSkeleton rows={10} />
      ) : casesQuery.isError ? (
        <EmptyState
          icon={<FolderSearch className="size-8" aria-hidden="true" />}
          title="Could not load cases"
          description={casesQuery.error.message}
          actionLabel="Retry"
          onAction={() => void casesQuery.refetch()}
        />
      ) : items.length === 0 ? (
        <EmptyState
          icon={<FolderSearch className="size-8" aria-hidden="true" />}
          title="No cases found"
          description="Try broadening filters or switch to Team/All cases to find active work."
          actionLabel="Clear filters"
          onAction={() => {
            setSearchParams(new URLSearchParams({ scope }));
          }}
        />
      ) : (
        <>
          <DataTable
            data={items}
            columns={columns}
            rowId={(row) => row.id}
            selectedRowIds={selectedCaseIds}
            onRowSelect={handleRowSelect}
            onRowClick={(row) => navigate(`/cases/${row.id}`)}
            height={620}
            ariaLabel="Case list"
          />
          <Pagination
            page={page}
            total={total}
            limit={limit}
            onPageChange={(nextPage) => updateParam('page', nextPage)}
            onLimitChange={(nextLimit) => updateParam('limit', nextLimit)}
          />
        </>
      )}
    </div>
  );
}
