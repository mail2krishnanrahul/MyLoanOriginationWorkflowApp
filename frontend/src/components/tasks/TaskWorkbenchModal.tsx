import { zodResolver } from '@hookform/resolvers/zod';
import { Braces, ClipboardCopy, PanelRightClose, PanelRightOpen } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { Controller, useForm, type DefaultValues } from 'react-hook-form';
import { z } from 'zod';
import { Button } from '@/components/ui/Button';
import {
  FormField,
  NumberInput,
  SelectInput,
  TextArea,
  TextInput
} from '@/components/ui/FormField';
import { Modal } from '@/components/ui/Modal';
import { SkeletonCard } from '@/components/ui/LoadingSkeleton';
import { useCompleteTask, useSaveTask, useTask } from '@/hooks/use-tasks';
import type { TaskDefinitionField } from '@/lib/api/types';
import { formatDateTime, formatMinutesRemaining } from '@/lib/utils/date';
import { toast } from 'sonner';
import { PriorityBadge } from '@/components/domain/PriorityBadge';
import { SLAIndicator } from '@/components/domain/SLAIndicator';
import { StatusBadge } from '@/components/domain/StatusBadge';

interface TaskWorkbenchModalProps {
  taskId?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

function buildSchemaFromFields(fields: TaskDefinitionField[]) {
  const schemaShape: Record<string, z.ZodTypeAny> = {};

  fields.forEach((field) => {
    let fieldSchema: z.ZodTypeAny;

    switch (field.type) {
      case 'number': {
        let numberSchema = z.coerce.number().refine((value) => !Number.isNaN(value), {
          message: `${field.label} must be numeric`
        });
        if (field.min !== undefined) {
          numberSchema = numberSchema.min(field.min, `${field.label} must be >= ${field.min}`);
        }
        if (field.max !== undefined) {
          numberSchema = numberSchema.max(field.max, `${field.label} must be <= ${field.max}`);
        }
        fieldSchema = numberSchema;
        break;
      }
      case 'date':
        fieldSchema = z.string().min(1, `${field.label} is required`).refine((value) => !Number.isNaN(Date.parse(value)), {
          message: `${field.label} must be a valid date`
        });
        break;
      default:
        fieldSchema = z.string();
    }

    if (!field.required) {
      fieldSchema = fieldSchema.optional();
    } else {
      fieldSchema = fieldSchema.refine((value: unknown) => `${value ?? ''}`.trim().length > 0, {
        message: `${field.label} is required`
      });
    }

    schemaShape[field.key] = fieldSchema;
  });

  return z.object(schemaShape);
}

function isVisible(field: TaskDefinitionField, values: Record<string, unknown>) {
  if (!field.dependsOn) {
    return true;
  }

  return values[field.dependsOn.field] === field.dependsOn.equals;
}

function computeValue(field: TaskDefinitionField, values: Record<string, unknown>) {
  if (!field.formula || !field.formulaFields?.length) {
    return undefined;
  }

  const numbers = field.formulaFields.map((key) => Number(values[key] ?? 0));

  if (numbers.some((value) => Number.isNaN(value))) {
    return undefined;
  }

  if (field.formula === 'SUM') {
    return numbers.reduce((sum, value) => sum + value, 0);
  }

  if (field.formula === 'DIFF') {
    return numbers.slice(1).reduce((result, value) => result - value, numbers[0] ?? 0);
  }

  if (field.formula === 'MULTIPLY') {
    return numbers.reduce((result, value) => result * value, 1);
  }

  return undefined;
}

function readFileAsBase64(file: File) {
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = reject;
    reader.readAsDataURL(file);
  });
}

export function TaskWorkbenchModal({ taskId, open, onOpenChange }: TaskWorkbenchModalProps) {
  const taskQuery = useTask(taskId ?? '');
  const [inputPanelOpen, setInputPanelOpen] = useState(true);

  const outputSchema = taskQuery.data?.outputSchema ?? [];

  const validationSchema = useMemo(() => buildSchemaFromFields(outputSchema), [outputSchema]);

  const defaultValues = useMemo<DefaultValues<Record<string, unknown>>>(() => {
    const values = taskQuery.data?.outputPayload ?? {};
    return values;
  }, [taskQuery.data?.outputPayload]);

  const form = useForm<Record<string, unknown>>({
    resolver: zodResolver(validationSchema),
    mode: 'onChange',
    defaultValues
  });

  useEffect(() => {
    form.reset(defaultValues);
  }, [defaultValues, form]);

  const values = form.watch();

  useEffect(() => {
    outputSchema.forEach((field) => {
      const computedValue = computeValue(field, values);
      if (computedValue !== undefined && values[field.key] !== computedValue) {
        form.setValue(field.key, computedValue, {
          shouldDirty: true,
          shouldValidate: true
        });
      }
    });
  }, [outputSchema, values, form]);

  const saveTask = useSaveTask(taskId ?? '');
  const completeTask = useCompleteTask(taskId ?? '', taskQuery.data?.caseId ?? '');

  const visibleFields = outputSchema.filter((field) => isVisible(field, values));

  const submit = form.handleSubmit(async (submittedValues) => {
    if (!taskId) {
      return;
    }

    await completeTask.mutateAsync({ outputPayload: submittedValues });
    onOpenChange(false);
  });

  const saveDraft = form.handleSubmit(async (submittedValues) => {
    if (!taskId) {
      return;
    }

    await saveTask.mutateAsync({ outputPayload: submittedValues });
  });

  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      size="full"
      title={taskQuery.data?.name ?? 'Task workbench'}
      description="Complete task outputs with real-time validation"
      footer={
        <div className="sticky bottom-0 flex items-center justify-between gap-3">
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <div className="flex items-center gap-2">
            <Button variant="secondary" onClick={() => void saveDraft()} loading={saveTask.isPending}>
              Save Draft
            </Button>
            <Button onClick={() => void submit()} loading={completeTask.isPending} disabled={!form.formState.isValid}>
              Complete Task
            </Button>
          </div>
        </div>
      }
    >
      {taskQuery.isLoading ? (
        <SkeletonCard className="h-96" />
      ) : taskQuery.isError || !taskQuery.data ? (
        <div className="panel-muted p-4 text-sm text-danger-500">{taskQuery.error?.message ?? 'Task not found'}</div>
      ) : (
        <div className="grid h-full gap-4 lg:grid-cols-[1.7fr_0.9fr]">
          <section className="space-y-4">
            <header className="panel-muted grid gap-3 p-4 md:grid-cols-3">
              <div>
                <p className="text-xs uppercase tracking-wide text-neutral-500 dark:text-neutral-300">Case</p>
                <p className="font-mono text-sm font-semibold text-brand-700 dark:text-brand-200">
                  {taskQuery.data.caseReference}
                </p>
              </div>
              <div className="space-y-2">
                <StatusBadge status={taskQuery.data.status} />
                <PriorityBadge priority={taskQuery.data.priority} />
              </div>
              <div className="space-y-1 text-sm text-neutral-600 dark:text-neutral-200">
                <SLAIndicator
                  status={taskQuery.data.slaStatus}
                  remainingMinutes={taskQuery.data.dueAt ? Math.max(1, Math.round((new Date(taskQuery.data.dueAt).getTime() - Date.now()) / 60000)) : 120}
                />
                <p>{taskQuery.data.dueAt ? formatMinutesRemaining(Math.round((new Date(taskQuery.data.dueAt).getTime() - Date.now()) / 60000)) : 'No due date'}</p>
                <p>Created: {formatDateTime(taskQuery.data.createdAt)}</p>
              </div>
            </header>

            <form className="grid gap-4" onSubmit={(event) => event.preventDefault()}>
              {visibleFields.map((field) => {
                const error = form.formState.errors[field.key]?.message as string | undefined;

                if (field.type === 'textarea') {
                  return (
                    <FormField
                      key={field.key}
                      id={field.key}
                      label={field.label}
                      required={field.required}
                      hint={field.helpText}
                      error={error}
                    >
                      <TextArea
                        id={field.key}
                        rows={4}
                        placeholder={field.placeholder}
                        hasError={Boolean(error)}
                        {...form.register(field.key)}
                      />
                    </FormField>
                  );
                }

                if (field.type === 'select') {
                  return (
                    <FormField
                      key={field.key}
                      id={field.key}
                      label={field.label}
                      required={field.required}
                      hint={field.helpText}
                      error={error}
                    >
                      <SelectInput id={field.key} hasError={Boolean(error)} {...form.register(field.key)}>
                        <option value="">Select an option</option>
                        {field.options?.map((option) => (
                          <option key={option.value} value={option.value}>
                            {option.label}
                          </option>
                        ))}
                      </SelectInput>
                    </FormField>
                  );
                }

                if (field.type === 'number') {
                  return (
                    <FormField
                      key={field.key}
                      id={field.key}
                      label={field.label}
                      required={field.required}
                      hint={field.helpText}
                      error={error}
                    >
                      <NumberInput
                        id={field.key}
                        hasError={Boolean(error)}
                        placeholder={field.placeholder}
                        {...form.register(field.key)}
                      />
                    </FormField>
                  );
                }

                if (field.type === 'date') {
                  return (
                    <FormField
                      key={field.key}
                      id={field.key}
                      label={field.label}
                      required={field.required}
                      hint={field.helpText}
                      error={error}
                    >
                      <TextInput id={field.key} type="date" hasError={Boolean(error)} {...form.register(field.key)} />
                    </FormField>
                  );
                }

                if (field.type === 'file') {
                  return (
                    <FormField
                      key={field.key}
                      id={field.key}
                      label={field.label}
                      required={field.required}
                      hint={field.helpText}
                      error={error}
                    >
                      <Controller
                        control={form.control}
                        name={field.key}
                        render={({ field: controlledField }) => (
                          <input
                            id={field.key}
                            type="file"
                            className="block w-full text-sm file:mr-4 file:rounded-md file:border-0 file:bg-brand-600 file:px-3 file:py-2 file:text-white hover:file:bg-brand-700"
                            onChange={async (event) => {
                              const file = event.target.files?.[0];
                              if (!file) {
                                controlledField.onChange(undefined);
                                return;
                              }

                              const base64Content = await readFileAsBase64(file);
                              controlledField.onChange(base64Content);
                              toast.success(`${file.name} attached`);
                            }}
                          />
                        )}
                      />
                    </FormField>
                  );
                }

                const computed = Boolean(field.formula);

                return (
                  <FormField
                    key={field.key}
                    id={field.key}
                    label={field.label}
                    required={field.required}
                    hint={computed ? `Computed using ${field.formulaFields?.join(', ')}` : field.helpText}
                    error={error}
                  >
                    <TextInput
                      id={field.key}
                      hasError={Boolean(error)}
                      placeholder={field.placeholder}
                      readOnly={computed}
                      {...form.register(field.key)}
                    />
                  </FormField>
                );
              })}
            </form>

            {taskQuery.data.validationErrors?.length ? (
              <div className="rounded-lg border border-danger-200 bg-danger-50 p-3 text-sm text-danger-700 dark:border-danger-500/30 dark:bg-danger-500/10 dark:text-red-200">
                <p className="font-semibold">Schema validation issues</p>
                <ul className="mt-1 list-inside list-disc space-y-1">
                  {taskQuery.data.validationErrors.map((error) => (
                    <li key={error}>{error}</li>
                  ))}
                </ul>
              </div>
            ) : null}
          </section>

          <aside className="space-y-3">
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-semibold text-neutral-900 dark:text-neutral-100">Input payload</h3>
              <Button
                variant="ghost"
                size="sm"
                aria-label={inputPanelOpen ? 'Collapse input panel' : 'Expand input panel'}
                onClick={() => setInputPanelOpen((current) => !current)}
              >
                {inputPanelOpen ? (
                  <PanelRightClose className="size-4" aria-hidden="true" />
                ) : (
                  <PanelRightOpen className="size-4" aria-hidden="true" />
                )}
              </Button>
            </div>
            {inputPanelOpen ? (
              <div className="panel-muted space-y-2 p-3">
                <div className="flex items-center justify-between">
                  <span className="inline-flex items-center gap-1 text-xs font-medium text-neutral-500 dark:text-neutral-300">
                    <Braces className="size-3" aria-hidden="true" />
                    Read-only JSON
                  </span>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      void navigator.clipboard.writeText(JSON.stringify(taskQuery.data?.inputPayload ?? {}, null, 2));
                      toast.success('Input JSON copied');
                    }}
                  >
                    <ClipboardCopy className="size-3" aria-hidden="true" />
                    Copy
                  </Button>
                </div>
                <pre className="max-h-[28rem] overflow-auto rounded-lg bg-neutral-950 p-3 text-xs text-neutral-100">
                  {JSON.stringify(taskQuery.data.inputPayload ?? {}, null, 2)}
                </pre>
              </div>
            ) : null}
          </aside>
        </div>
      )}
    </Modal>
  );
}
