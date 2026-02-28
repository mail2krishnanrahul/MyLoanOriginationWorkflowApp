import { z } from 'zod';
import type { ApiErrorPayload } from '@/lib/api/types';

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? '';

export class ApiError extends Error {
  status: number;
  payload?: ApiErrorPayload;

  constructor(message: string, status: number, payload?: ApiErrorPayload) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.payload = payload;
  }
}

function buildUrl(path: string, params?: Record<string, unknown>) {
  const url = new URL(path, API_BASE_URL || window.location.origin);

  if (params) {
    Object.entries(params).forEach(([key, value]) => {
      if (value === undefined || value === null || value === '') {
        return;
      }

      if (Array.isArray(value)) {
        value.forEach((entry) => {
          url.searchParams.append(key, String(entry));
        });
        return;
      }

      url.searchParams.set(key, String(value));
    });
  }

  return API_BASE_URL ? url.toString() : `${url.pathname}${url.search}`;
}

export async function apiFetch<T>(
  path: string,
  options?: {
    method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
    body?: unknown;
    params?: Record<string, unknown>;
    schema?: z.ZodType<T>;
    signal?: AbortSignal;
  }
): Promise<T> {
  const response = await fetch(buildUrl(path, options?.params), {
    method: options?.method ?? 'GET',
    headers: {
      'Content-Type': 'application/json',
      'X-Tenant-ID': 'DEFAULT'
    },
    body: options?.body ? JSON.stringify(options.body) : undefined,
    signal: options?.signal
  });

  const text = await response.text();

  let payload: unknown;
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      // Response is not valid JSON (e.g. proxy error page, HTML error, etc.)
      throw new ApiError(
        `Server returned non-JSON response (HTTP ${response.status})`,
        response.status,
        { message: text.slice(0, 200), code: 'NON_JSON_RESPONSE' }
      );
    }
  }

  if (!response.ok) {
    const errorPayload = (payload as ApiErrorPayload | undefined) ?? undefined;
    throw new ApiError(
      errorPayload?.message ?? `Request failed with status ${response.status}`,
      response.status,
      errorPayload
    );
  }

  if (!options?.schema) {
    return payload as T;
  }

  const parsed = options.schema.safeParse(payload);
  if (!parsed.success) {
    throw new ApiError(`Unexpected response format for ${path}`, response.status, {
      message: parsed.error.issues.map((issue) => issue.message).join(', '),
      code: 'INVALID_PAYLOAD',
      details: parsed.error.flatten()
    });
  }

  return parsed.data;
}
