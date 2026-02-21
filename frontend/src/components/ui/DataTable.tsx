import {
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnDef,
  type SortingState
} from '@tanstack/react-table';
import { useVirtualizer } from '@tanstack/react-virtual';
import { ArrowDown, ArrowUp, ArrowUpDown } from 'lucide-react';
import { useMemo, useRef, useState } from 'react';
import { cn } from '@/lib/cn';

interface DataTableProps<TData> {
  data: TData[];
  columns: Array<ColumnDef<TData>>;
  height?: number;
  rowId?: (row: TData) => string;
  onRowClick?: (row: TData) => void;
  selectedRowIds?: Set<string>;
  onRowSelect?: (row: TData, selected: boolean) => void;
  ariaLabel?: string;
}

export function DataTable<TData>({
  data,
  columns,
  height = 560,
  rowId,
  onRowClick,
  selectedRowIds,
  onRowSelect,
  ariaLabel
}: DataTableProps<TData>) {
  const [sorting, setSorting] = useState<SortingState>([]);
  const containerRef = useRef<HTMLDivElement | null>(null);

  const table = useReactTable({
    data,
    columns,
    state: {
      sorting
    },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel()
  });

  const rows = table.getRowModel().rows;

  const rowVirtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => containerRef.current,
    estimateSize: () => 56,
    overscan: 12
  });

  const totalSize = rowVirtualizer.getTotalSize();
  const virtualRows = rowVirtualizer.getVirtualItems();

  const paddingTop = virtualRows.length > 0 ? virtualRows[0]?.start ?? 0 : 0;
  const paddingBottom =
    virtualRows.length > 0 ? totalSize - (virtualRows[virtualRows.length - 1]?.end ?? 0) : 0;

  const selectEnabled = useMemo(() => Boolean(onRowSelect && rowId), [onRowSelect, rowId]);

  return (
    <div className="panel overflow-hidden p-0">
      <div
        ref={containerRef}
        className="scrollbar-thin max-h-[80vh] overflow-auto"
        style={{ height }}
        role="region"
        aria-label={ariaLabel ?? 'Data table'}
      >
        <table className="w-full border-separate border-spacing-0 text-left text-sm">
          <thead className="sticky top-0 z-20 bg-neutral-50/95 backdrop-blur dark:bg-neutral-900/95">
            {table.getHeaderGroups().map((headerGroup) => (
              <tr key={headerGroup.id}>
                {selectEnabled ? (
                  <th className="w-12 border-b border-neutral-200 px-4 py-3 dark:border-neutral-700" />
                ) : null}
                {headerGroup.headers.map((header) => {
                  const canSort = header.column.getCanSort();
                  const sorted = header.column.getIsSorted();

                  return (
                    <th
                      key={header.id}
                      scope="col"
                      className="border-b border-neutral-200 px-4 py-3 font-semibold text-neutral-600 dark:border-neutral-700 dark:text-neutral-300"
                    >
                      {header.isPlaceholder ? null : (
                        <button
                          type="button"
                          className={cn(
                            'inline-flex items-center gap-1',
                            canSort ? 'cursor-pointer hover:text-neutral-900 dark:hover:text-white' : ''
                          )}
                          onClick={canSort ? header.column.getToggleSortingHandler() : undefined}
                        >
                          {flexRender(header.column.columnDef.header, header.getContext())}
                          {canSort ? (
                            sorted === 'asc' ? (
                              <ArrowUp className="size-4" aria-hidden="true" />
                            ) : sorted === 'desc' ? (
                              <ArrowDown className="size-4" aria-hidden="true" />
                            ) : (
                              <ArrowUpDown className="size-4 opacity-60" aria-hidden="true" />
                            )
                          ) : null}
                        </button>
                      )}
                    </th>
                  );
                })}
              </tr>
            ))}
          </thead>
          <tbody>
            {paddingTop > 0 ? (
              <tr>
                <td colSpan={table.getVisibleLeafColumns().length + (selectEnabled ? 1 : 0)} style={{ height: paddingTop }} />
              </tr>
            ) : null}
            {virtualRows.map((virtualRow) => {
              const row = rows[virtualRow.index];
              const record = row.original;
              const recordId = rowId ? rowId(record) : row.id;
              const selected = selectedRowIds?.has(recordId);

              return (
                <tr
                  key={row.id}
                  onClick={() => onRowClick?.(record)}
                  className={cn(
                    'group transition-colors duration-150',
                    onRowClick ? 'cursor-pointer hover:bg-brand-50/55 dark:hover:bg-brand-500/10' : '',
                    selected ? 'bg-brand-50/70 dark:bg-brand-500/10' : '',
                    'focus-within:bg-brand-50/55 dark:focus-within:bg-brand-500/10'
                  )}
                >
                  {selectEnabled ? (
                    <td className="border-b border-neutral-100 px-4 py-3 align-middle dark:border-neutral-800">
                      <input
                        aria-label="Select row"
                        type="checkbox"
                        checked={selected}
                        onChange={(event) => onRowSelect?.(record, event.target.checked)}
                        onClick={(event) => event.stopPropagation()}
                      />
                    </td>
                  ) : null}
                  {row.getVisibleCells().map((cell) => (
                    <td
                      key={cell.id}
                      className="border-b border-neutral-100 px-4 py-3 align-middle text-neutral-700 dark:border-neutral-800 dark:text-neutral-200"
                    >
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </td>
                  ))}
                </tr>
              );
            })}
            {paddingBottom > 0 ? (
              <tr>
                <td
                  colSpan={table.getVisibleLeafColumns().length + (selectEnabled ? 1 : 0)}
                  style={{ height: paddingBottom }}
                />
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>
    </div>
  );
}
