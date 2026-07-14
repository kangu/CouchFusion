import type { ApiEnvelope, ApiErrorPayload } from '../types/api'

export interface FetchOptions {
  method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  credentials: 'same-origin'
  body?: unknown
}

export type FetchLike = <T>(url: string, options: FetchOptions) => Promise<ApiEnvelope<T> | undefined>

export class ApiClientError extends Error implements ApiErrorPayload {
  readonly code: ApiErrorPayload['code']
  readonly fields?: Record<string, string>

  constructor(payload: ApiErrorPayload) {
    super(payload.message)
    this.name = 'ApiClientError'
    this.code = payload.code
    if (payload.fields) this.fields = payload.fields
  }
}

export function createApiClient(fetcher: FetchLike) {
  async function request<T>(method: FetchOptions['method'], path: string, body?: unknown): Promise<T> {
    const options: FetchOptions = { method, credentials: 'same-origin' }
    if (body !== undefined) options.body = body
    try {
      const response = await fetcher<T>(`/api/v1${normalizePath(path)}`, options)
      return response?.data as T
    } catch (error) {
      throw normalizeApiError(error)
    }
  }

  return {
    get: <T>(path: string) => request<T>('GET', path),
    post: <T>(path: string, body: unknown = {}) => request<T>('POST', path, body),
    put: <T>(path: string, body: unknown) => request<T>('PUT', path, body),
    patch: <T>(path: string, body: unknown) => request<T>('PATCH', path, body),
    delete: <T>(path: string, body: unknown = {}) => request<T>('DELETE', path, body),
  }
}

export function useApi() {
  const fetcher: FetchLike = (url, options) =>
    $fetch(url, options as Parameters<typeof $fetch>[1]) as Promise<ApiEnvelope<never>>
  return createApiClient(fetcher)
}

function normalizePath(path: string) {
  return path.startsWith('/') ? path : `/${path}`
}

function normalizeApiError(error: unknown): ApiClientError {
  const candidate = error as {
    data?: { error?: ApiErrorPayload }
    response?: { _data?: { error?: ApiErrorPayload } }
  }
  const payload = candidate?.data?.error ?? candidate?.response?._data?.error
  if (payload?.code && payload.message) return new ApiClientError(payload)
  return new ApiClientError({ code: 'unavailable', message: 'Could not reach the CouchFusion server.' })
}
