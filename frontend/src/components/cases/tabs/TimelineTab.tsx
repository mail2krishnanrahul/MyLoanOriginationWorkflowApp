import { Download } from 'lucide-react';
import { useMemo, useState } from 'react';
import { CaseTimeline } from '@/components/domain/CaseTimeline';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { FormField, SelectInput, TextInput } from '@/components/ui/FormField';
import { TableSkeleton } from '@/components/ui/LoadingSkeleton';
import { useCaseTimeline } from '@/hooks/use-timeline';
import type { TimelineEvent } from '@/lib/api/types';

interface TimelineTabProps {
  caseId: string;
}

const eventTypeOptions: TimelineEvent['type'][] = [
  'CASE_CREATED',
  'STAGE_CHANGED',
  'TASK_COMPLETED',
  'DOCUMENT_UPLOADED',
  'APPROVAL_GRANTED',
  'SLA_WARNING',
  'COMMENT',
  'NOTIFICATION'
];

function exportTimelineCsv(events: TimelineEvent[]) {
  const headers = ['id', 'type', 'actor', 'timestamp', 'description'];
  const rows = events.map((event) => [
    event.id,
    event.type,
    event.actor.displayName,
    event.timestamp,
    event.description.replaceAll(',', ';')
  ]);
  const csv = [headers.join(','), ...rows.map((row) => row.join(','))].join('\n');

  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = 'case-timeline.csv';
  link.click();
  URL.revokeObjectURL(url);
}

export function TimelineTab({ caseId }: TimelineTabProps) {
  const timelineQuery = useCaseTimeline(caseId);

  const [eventType, setEventType] = useState('');
  const [actor, setActor] = useState('');
  const [fromDate, setFromDate] = useState('');
  const [toDate, setToDate] = useState('');

  const filteredEvents = useMemo(() => {
    return (timelineQuery.data ?? []).filter((event) => {
      if (eventType && event.type !== eventType) {
        return false;
      }

      if (actor && !event.actor.displayName.toLowerCase().includes(actor.toLowerCase())) {
        return false;
      }

      if (fromDate && new Date(event.timestamp) < new Date(fromDate)) {
        return false;
      }

      if (toDate && new Date(event.timestamp) > new Date(`${toDate}T23:59:59`)) {
        return false;
      }

      return true;
    });
  }, [timelineQuery.data, eventType, actor, fromDate, toDate]);

  if (timelineQuery.isLoading) {
    return <TableSkeleton rows={7} />;
  }

  if (timelineQuery.isError) {
    return <div className="panel-muted p-4 text-sm text-danger-500">{timelineQuery.error.message}</div>;
  }

  return (
    <div id="timeline-panel" role="tabpanel" aria-labelledby="timeline-tab" className="space-y-4">
      <Card className="grid gap-3 md:grid-cols-5">
        <FormField id="timelineType" label="Event type">
          <SelectInput id="timelineType" value={eventType} onChange={(event) => setEventType(event.target.value)}>
            <option value="">All events</option>
            {eventTypeOptions.map((type) => (
              <option key={type} value={type}>
                {type.replaceAll('_', ' ')}
              </option>
            ))}
          </SelectInput>
        </FormField>

        <FormField id="timelineActor" label="Actor">
          <TextInput
            id="timelineActor"
            value={actor}
            onChange={(event) => setActor(event.target.value)}
            placeholder="Search actor"
          />
        </FormField>

        <FormField id="timelineFrom" label="From date">
          <TextInput id="timelineFrom" type="date" value={fromDate} onChange={(event) => setFromDate(event.target.value)} />
        </FormField>

        <FormField id="timelineTo" label="To date">
          <TextInput id="timelineTo" type="date" value={toDate} onChange={(event) => setToDate(event.target.value)} />
        </FormField>

        <div className="flex items-end gap-2">
          <Button variant="secondary" className="w-full" onClick={() => exportTimelineCsv(filteredEvents)}>
            <Download className="size-4" aria-hidden="true" />
            CSV
          </Button>
          <Button variant="secondary" className="w-full" onClick={() => window.print()}>
            <Download className="size-4" aria-hidden="true" />
            PDF
          </Button>
        </div>
      </Card>

      <CaseTimeline events={filteredEvents} />
    </div>
  );
}
