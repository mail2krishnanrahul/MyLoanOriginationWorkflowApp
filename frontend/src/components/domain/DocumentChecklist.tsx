import { FileCheck2, FileClock, FileWarning } from 'lucide-react';
import { Card, CardDescription, CardHeader, CardTitle } from '@/components/ui/Card';
import type { DocumentRequirement } from '@/lib/api/types';

interface DocumentChecklistProps {
  requirements: DocumentRequirement[];
}

export function DocumentChecklist({ requirements }: DocumentChecklistProps) {
  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>Document checklist</CardTitle>
          <CardDescription>Track required evidence by stage</CardDescription>
        </div>
      </CardHeader>
      <div className="space-y-2">
        {requirements.map((requirement) => {
          const complete = requirement.status === 'VERIFIED';
          const rejected = requirement.status === 'REJECTED';

          return (
            <div key={requirement.id} className="panel-muted flex items-center justify-between gap-3 px-3 py-2">
              <div className="flex items-center gap-2">
                <span className="rounded-md bg-neutral-100 p-1.5 text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300">
                  {complete ? (
                    <FileCheck2 className="size-4" aria-hidden="true" />
                  ) : rejected ? (
                    <FileWarning className="size-4 text-danger-500" aria-hidden="true" />
                  ) : (
                    <FileClock className="size-4 text-warning-500" aria-hidden="true" />
                  )}
                </span>
                <div>
                  <p className="text-sm font-medium text-neutral-800 dark:text-neutral-100">{requirement.name}</p>
                  <p className="text-xs text-neutral-500 dark:text-neutral-300">
                    {requirement.uploadedCount} / {requirement.requiredCount} uploaded | Due in {requirement.dueStage}
                  </p>
                </div>
              </div>
              <span className="text-xs font-semibold text-neutral-500 dark:text-neutral-300">
                {requirement.status}
              </span>
            </div>
          );
        })}
      </div>
    </Card>
  );
}
