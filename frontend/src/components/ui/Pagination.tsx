import { ChevronLeft, ChevronRight } from 'lucide-react';
import { Button } from '@/components/ui/Button';
import { SelectInput } from '@/components/ui/FormField';

interface PaginationProps {
  page: number;
  total: number;
  limit: number;
  onPageChange: (page: number) => void;
  onLimitChange: (limit: number) => void;
}

export function Pagination({ page, total, limit, onPageChange, onLimitChange }: PaginationProps) {
  const totalPages = Math.max(1, Math.ceil(total / limit));

  return (
    <div className="flex flex-col items-start justify-between gap-3 border-t border-neutral-200 px-4 py-3 sm:flex-row sm:items-center dark:border-neutral-700">
      <p className="text-sm text-neutral-500 dark:text-neutral-300">
        Showing {(page - 1) * limit + 1} to {Math.min(page * limit, total)} of {total}
      </p>
      <div className="flex items-center gap-2">
        <SelectInput
          aria-label="Rows per page"
          className="h-9 w-24"
          value={limit}
          onChange={(event) => onLimitChange(Number(event.target.value))}
        >
          {[25, 50, 100].map((size) => (
            <option key={size} value={size}>
              {size} / page
            </option>
          ))}
        </SelectInput>
        <Button variant="secondary" size="sm" onClick={() => onPageChange(page - 1)} disabled={page <= 1}>
          <ChevronLeft className="size-4" aria-hidden="true" />
          Prev
        </Button>
        <span className="w-24 text-center text-sm text-neutral-600 dark:text-neutral-200">
          Page {page} / {totalPages}
        </span>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => onPageChange(page + 1)}
          disabled={page >= totalPages}
        >
          Next
          <ChevronRight className="size-4" aria-hidden="true" />
        </Button>
      </div>
    </div>
  );
}
