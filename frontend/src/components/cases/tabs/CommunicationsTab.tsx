import { type ColumnDef } from '@tanstack/react-table';
import { Mail, MessageSquare, Smartphone } from 'lucide-react';
import { useMemo, useState } from 'react';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card, CardDescription, CardHeader, CardTitle } from '@/components/ui/Card';
import { DataTable } from '@/components/ui/DataTable';
import { FormField, SelectInput, TextArea, TextInput } from '@/components/ui/FormField';
import { Modal } from '@/components/ui/Modal';
import { TableSkeleton } from '@/components/ui/LoadingSkeleton';
import { useCaseNotifications, useResendNotification, useSendNotification } from '@/hooks/use-notifications';
import type { CaseNotification } from '@/lib/api/types';
import { formatDateTime } from '@/lib/utils/date';

interface CommunicationsTabProps {
  caseId: string;
}

function channelIcon(channel: CaseNotification['channel']) {
  if (channel === 'EMAIL') {
    return <Mail className="size-4" aria-hidden="true" />;
  }

  if (channel === 'SMS') {
    return <Smartphone className="size-4" aria-hidden="true" />;
  }

  return <MessageSquare className="size-4" aria-hidden="true" />;
}

export function CommunicationsTab({ caseId }: CommunicationsTabProps) {
  const notificationsQuery = useCaseNotifications(caseId);
  const sendNotification = useSendNotification(caseId);
  const resendNotification = useResendNotification(caseId);

  const [selectedNotification, setSelectedNotification] = useState<CaseNotification | null>(null);
  const [channel, setChannel] = useState<'EMAIL' | 'SMS' | 'IN_APP'>('EMAIL');
  const [recipient, setRecipient] = useState('');
  const [templateId, setTemplateId] = useState('LOAN_STATUS_UPDATE');
  const [subject, setSubject] = useState('Case update available');
  const [body, setBody] = useState('Your case has moved to the next processing stage.');

  const columns = useMemo<Array<ColumnDef<CaseNotification>>>(
    () => [
      {
        accessorKey: 'channel',
        header: 'Channel',
        cell: ({ row }) => (
          <span className="inline-flex items-center gap-1 text-sm">
            {channelIcon(row.original.channel)}
            {row.original.channel}
          </span>
        )
      },
      { accessorKey: 'recipient', header: 'Recipient' },
      { accessorKey: 'subject', header: 'Subject' },
      {
        accessorKey: 'sentAt',
        header: 'Sent at',
        cell: ({ row }) => formatDateTime(row.original.sentAt)
      },
      {
        accessorKey: 'status',
        header: 'Status',
        cell: ({ row }) => (
          <Badge variant={row.original.status === 'SENT' ? 'success' : 'danger'}>{row.original.status}</Badge>
        )
      },
      {
        accessorKey: 'acknowledged',
        header: 'Acknowledged',
        cell: ({ row }) => (row.original.acknowledged ? 'Yes' : 'No')
      },
      {
        id: 'actions',
        header: 'Actions',
        enableSorting: false,
        cell: ({ row }) => (
          <div className="flex gap-1">
            <Button
              variant="ghost"
              size="sm"
              onClick={(event) => {
                event.stopPropagation();
                setSelectedNotification(row.original);
              }}
            >
              View
            </Button>
            <Button
              variant="ghost"
              size="sm"
              disabled={row.original.status !== 'FAILED'}
              onClick={(event) => {
                event.stopPropagation();
                resendNotification.mutate(row.original.id);
              }}
            >
              Resend
            </Button>
          </div>
        )
      }
    ],
    [resendNotification]
  );

  if (notificationsQuery.isLoading) {
    return <TableSkeleton rows={6} />;
  }

  if (notificationsQuery.isError) {
    return <div className="panel-muted p-4 text-sm text-danger-500">{notificationsQuery.error.message}</div>;
  }

  return (
    <div id="communications-panel" role="tabpanel" aria-labelledby="communications-tab" className="space-y-4">
      <Card>
        <CardHeader>
          <div>
            <CardTitle>Sent notifications</CardTitle>
            <CardDescription>Delivery and acknowledgement tracking</CardDescription>
          </div>
        </CardHeader>
        <DataTable
          data={notificationsQuery.data ?? []}
          columns={columns}
          rowId={(row) => row.id}
          onRowClick={(row) => setSelectedNotification(row)}
          height={420}
        />
      </Card>

      <Card>
        <CardHeader>
          <div>
            <CardTitle>Send notification</CardTitle>
            <CardDescription>Use templates and send over the right channel</CardDescription>
          </div>
        </CardHeader>

        <form
          className="grid gap-3 md:grid-cols-2"
          onSubmit={async (event) => {
            event.preventDefault();
            await sendNotification.mutateAsync({
              channel,
              templateId,
              recipient,
              subject,
              body
            });
          }}
        >
          <FormField id="notificationTemplate" label="Template" required>
            <SelectInput
              id="notificationTemplate"
              value={templateId}
              onChange={(event) => setTemplateId(event.target.value)}
            >
              <option value="LOAN_STATUS_UPDATE">Loan Status Update</option>
              <option value="MISSING_DOCUMENT">Missing Document Reminder</option>
              <option value="APPROVAL_REQUEST">Approval Request</option>
            </SelectInput>
          </FormField>

          <FormField id="notificationChannel" label="Channel" required>
            <SelectInput
              id="notificationChannel"
              value={channel}
              onChange={(event) => setChannel(event.target.value as 'EMAIL' | 'SMS' | 'IN_APP')}
            >
              <option value="EMAIL">Email</option>
              <option value="SMS">SMS</option>
              <option value="IN_APP">In-App</option>
            </SelectInput>
          </FormField>

          <FormField id="notificationRecipient" label="Recipient" required>
            <TextInput
              id="notificationRecipient"
              value={recipient}
              onChange={(event) => setRecipient(event.target.value)}
              placeholder="borrower@example.com"
            />
          </FormField>

          <FormField id="notificationSubject" label="Subject" required>
            <TextInput
              id="notificationSubject"
              value={subject}
              onChange={(event) => setSubject(event.target.value)}
              placeholder="Subject"
            />
          </FormField>

          <div className="md:col-span-2">
            <FormField id="notificationBody" label="Preview" required>
              <TextArea
                id="notificationBody"
                rows={4}
                value={body}
                onChange={(event) => setBody(event.target.value)}
              />
            </FormField>
          </div>

          <div className="md:col-span-2 flex justify-end">
            <Button type="submit" loading={sendNotification.isPending} disabled={!recipient || !subject || !body}>
              Send notification
            </Button>
          </div>
        </form>
      </Card>

      <Modal
        open={Boolean(selectedNotification)}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) {
            setSelectedNotification(null);
          }
        }}
        title={selectedNotification?.subject ?? 'Notification'}
        description={selectedNotification ? `${selectedNotification.channel} to ${selectedNotification.recipient}` : ''}
        size="md"
      >
        <article className="space-y-3">
          <div className="text-xs text-neutral-500 dark:text-neutral-300">
            Sent {formatDateTime(selectedNotification?.sentAt)} | Status: {selectedNotification?.status}
          </div>
          <div className="rounded-lg bg-neutral-100 p-4 text-sm text-neutral-700 dark:bg-neutral-800 dark:text-neutral-100">
            {selectedNotification?.body}
          </div>
        </article>
      </Modal>
    </div>
  );
}
