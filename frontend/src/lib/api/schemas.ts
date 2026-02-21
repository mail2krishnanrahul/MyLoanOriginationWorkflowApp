import { z } from 'zod';

const caseStatusSchema = z.enum([
  'DRAFT',
  'IN_PROGRESS',
  'PENDING_APPROVAL',
  'SUSPENDED',
  'WITHDRAWN',
  'CLOSED',
  'REJECTED'
]);

const prioritySchema = z.enum(['CRITICAL', 'HIGH', 'NORMAL', 'LOW']);
const slaStatusSchema = z.enum(['ON_TRACK', 'WARNING', 'BREACHED']);

export const userSummarySchema = z.object({
  id: z.string(),
  displayName: z.string(),
  email: z.string().optional(),
  avatarUrl: z.string().optional()
});

export const caseListItemSchema = z.object({
  id: z.string(),
  referenceNumber: z.string(),
  borrowerName: z.string(),
  caseType: z.string(),
  stage: z.string(),
  status: caseStatusSchema,
  priority: prioritySchema,
  assignedTo: userSummarySchema.optional(),
  slaStatus: slaStatusSchema,
  slaRemainingMinutes: z.number(),
  tags: z.array(z.string()).optional(),
  createdAt: z.string(),
  updatedAt: z.string()
});

export const paginatedSchema = <T extends z.ZodTypeAny>(itemSchema: T) =>
  z.object({
    items: z.array(itemSchema),
    page: z.number(),
    limit: z.number(),
    total: z.number()
  });

export const timelineEventSchema = z.object({
  id: z.string(),
  type: z.string(),
  actor: userSummarySchema,
  timestamp: z.string(),
  description: z.string(),
  before: z.record(z.string(), z.unknown()).optional(),
  after: z.record(z.string(), z.unknown()).optional()
});
