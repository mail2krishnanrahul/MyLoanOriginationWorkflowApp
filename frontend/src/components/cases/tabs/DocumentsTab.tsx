import { useMemo, useState } from 'react';
import { FileSearch, Upload } from 'lucide-react';
import { toast } from 'sonner';
import { type ColumnDef } from '@tanstack/react-table';
import { DocumentChecklist } from '@/components/domain/DocumentChecklist';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card, CardDescription, CardHeader, CardTitle } from '@/components/ui/Card';
import { DataTable } from '@/components/ui/DataTable';
import { EmptyState } from '@/components/ui/EmptyState';
import { FormField, SelectInput, TextInput } from '@/components/ui/FormField';
import { Modal } from '@/components/ui/Modal';
import { TableSkeleton } from '@/components/ui/LoadingSkeleton';
import { useCaseDocuments, useUploadDocument, useVerifyDocument } from '@/hooks/use-documents';
import type { DocumentRecord } from '@/lib/api/types';
import { formatDateTime } from '@/lib/utils/date';
import { formatBytes } from '@/lib/utils/format';

interface DocumentsTabProps {
  caseId: string;
}

function documentStatusVariant(status: DocumentRecord['status']) {
  if (status === 'VERIFIED') {
    return 'success';
  }

  if (status === 'REJECTED') {
    return 'danger';
  }

  if (status === 'UPLOADED') {
    return 'info';
  }

  if (status === 'ARCHIVED') {
    return 'neutral';
  }

  return 'warning';
}

async function fileToBase64(file: File) {
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = reject;
    reader.readAsDataURL(file);
  });
}

export function DocumentsTab({ caseId }: DocumentsTabProps) {
  const documentsQuery = useCaseDocuments(caseId);
  const uploadDocument = useUploadDocument(caseId);
  const verifyDocument = useVerifyDocument(caseId);

  const [uploadOpen, setUploadOpen] = useState(false);
  const [viewerOpen, setViewerOpen] = useState(false);
  const [selectedDocument, setSelectedDocument] = useState<DocumentRecord | null>(null);
  const [uploadType, setUploadType] = useState('KYC_PROOF');

  const columns = useMemo<Array<ColumnDef<DocumentRecord>>>(
    () => [
      {
        accessorKey: 'fileName',
        header: 'Filename',
        cell: ({ row }) => <span className="font-medium">{row.original.fileName}</span>
      },
      { accessorKey: 'type', header: 'Type' },
      {
        accessorKey: 'sizeBytes',
        header: 'Size',
        cell: ({ row }) => formatBytes(row.original.sizeBytes)
      },
      {
        accessorKey: 'uploadedBy',
        header: 'Uploaded By',
        cell: ({ row }) => row.original.uploadedBy.displayName
      },
      {
        accessorKey: 'uploadedAt',
        header: 'Uploaded At',
        cell: ({ row }) => formatDateTime(row.original.uploadedAt)
      },
      {
        accessorKey: 'status',
        header: 'Status',
        cell: ({ row }) => (
          <Badge variant={documentStatusVariant(row.original.status)}>{row.original.status}</Badge>
        )
      },
      {
        accessorKey: 'version',
        header: 'Version',
        cell: ({ row }) => <span className="font-mono">v{row.original.version}</span>
      },
      {
        id: 'actions',
        header: 'Actions',
        enableSorting: false,
        cell: ({ row }) => (
          <div className="flex flex-wrap gap-1">
            <Button
              variant="ghost"
              size="sm"
              onClick={(event) => {
                event.stopPropagation();
                setSelectedDocument(row.original);
                setViewerOpen(true);
              }}
            >
              View
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={(event) => {
                event.stopPropagation();
                toast.success('Download started');
              }}
            >
              Download
            </Button>
            <Button
              variant="ghost"
              size="sm"
              disabled={row.original.status === 'VERIFIED'}
              onClick={(event) => {
                event.stopPropagation();
                verifyDocument.mutate(row.original.id);
              }}
            >
              Verify
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={(event) => {
                event.stopPropagation();
                toast.info('Rejection flow opened');
              }}
            >
              Reject
            </Button>
          </div>
        )
      }
    ],
    [verifyDocument]
  );

  if (documentsQuery.isLoading) {
    return <TableSkeleton rows={6} />;
  }

  if (documentsQuery.isError) {
    return <div className="panel-muted p-4 text-sm text-danger-500">{documentsQuery.error.message}</div>;
  }

  const checklist = documentsQuery.data?.checklist ?? [];
  const documents = documentsQuery.data?.documents ?? [];

  return (
    <div id="documents-panel" role="tabpanel" aria-labelledby="documents-tab" className="space-y-4">
      <DocumentChecklist requirements={checklist} />

      <Card>
        <CardHeader>
          <div>
            <CardTitle>Uploaded documents</CardTitle>
            <CardDescription>Versioned file set for underwriting and compliance</CardDescription>
          </div>
          <Button onClick={() => setUploadOpen(true)}>
            <Upload className="size-4" aria-hidden="true" />
            Upload document
          </Button>
        </CardHeader>

        {documents.length === 0 ? (
          <EmptyState
            icon={<FileSearch className="size-8" aria-hidden="true" />}
            title="No documents uploaded"
            description="Use the upload flow to attach borrower evidence and move verification forward."
            actionLabel="Upload now"
            onAction={() => setUploadOpen(true)}
          />
        ) : (
          <DataTable data={documents} columns={columns} rowId={(row) => row.id} height={460} />
        )}
      </Card>

      <Modal
        open={uploadOpen}
        onOpenChange={setUploadOpen}
        title="Upload document"
        description="Attach a new file version and map it to a document type"
        size="sm"
      >
        <form
          className="space-y-3"
          onSubmit={async (event) => {
            event.preventDefault();

            const fileInput = event.currentTarget.elements.namedItem('documentFile') as HTMLInputElement | null;
            const file = fileInput?.files?.[0];

            if (!file) {
              toast.error('Please select a file');
              return;
            }

            const base64Content = await fileToBase64(file);
            await uploadDocument.mutateAsync({
              fileName: file.name,
              type: uploadType,
              base64Content
            });
            setUploadOpen(false);
          }}
        >
          <FormField id="uploadType" label="Document type" required>
            <SelectInput id="uploadType" value={uploadType} onChange={(event) => setUploadType(event.target.value)}>
              <option value="KYC_PROOF">KYC Proof</option>
              <option value="INCOME_STATEMENT">Income Statement</option>
              <option value="BANK_STATEMENT">Bank Statement</option>
              <option value="PROPERTY_DOCUMENT">Property Document</option>
            </SelectInput>
          </FormField>
          <FormField id="documentFile" label="File" required>
            <TextInput id="documentFile" name="documentFile" type="file" />
          </FormField>
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="secondary" type="button" onClick={() => setUploadOpen(false)}>
              Cancel
            </Button>
            <Button type="submit" loading={uploadDocument.isPending}>
              Upload
            </Button>
          </div>
        </form>
      </Modal>

      <Modal
        open={viewerOpen}
        onOpenChange={setViewerOpen}
        title={selectedDocument?.fileName ?? 'Document viewer'}
        description="Preview and metadata"
        size="lg"
      >
        <div className="grid gap-4 lg:grid-cols-[1.4fr_0.8fr]">
          <div className="rounded-xl border border-neutral-200 bg-neutral-100 p-4 text-sm text-neutral-500 dark:border-neutral-700 dark:bg-neutral-800 dark:text-neutral-200">
            PDF and image previews render here when file content is available from the document API.
          </div>
          <aside className="panel-muted space-y-2 p-3 text-sm">
            <p>
              <span className="font-semibold">Type:</span> {selectedDocument?.type}
            </p>
            <p>
              <span className="font-semibold">Status:</span> {selectedDocument?.status}
            </p>
            <p>
              <span className="font-semibold">Uploaded:</span> {formatDateTime(selectedDocument?.uploadedAt)}
            </p>
            <p>
              <span className="font-semibold">Version:</span> v{selectedDocument?.version}
            </p>
          </aside>
        </div>
      </Modal>
    </div>
  );
}
