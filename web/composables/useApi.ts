import type { ApiEnvelope, ApiErrorPayload } from '../types/api'

export interface FetchOptions {
  method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  credentials: 'same-origin'
  body?: unknown
  headers?: Record<string, string>
}

export interface ApiRequestOptions {
  headers?: Record<string, string>
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
  async function request<T>(method: FetchOptions['method'], path: string, body?: unknown, requestOptions?: ApiRequestOptions): Promise<T> {
    const options: FetchOptions = { method, credentials: 'same-origin' }
    if (body !== undefined) options.body = body
    if (requestOptions?.headers) options.headers = requestOptions.headers
    try {
      const response = await fetcher<T>(`/api/v1${normalizePath(path)}`, options)
      return response?.data as T
    } catch (error) {
      throw normalizeApiError(error)
    }
  }

  return {
    get: <T>(path: string) => request<T>('GET', path),
    post: <T>(path: string, body: unknown = {}, options?: ApiRequestOptions) => request<T>('POST', path, body, options),
    put: <T>(path: string, body: unknown, options?: ApiRequestOptions) => request<T>('PUT', path, body, options),
    patch: <T>(path: string, body: unknown, options?: ApiRequestOptions) => request<T>('PATCH', path, body, options),
    delete: <T>(path: string, body: unknown = {}, options?: ApiRequestOptions) => request<T>('DELETE', path, body, options),
  }
}

export function useApi() {
  // A plain signature on purpose: Nuxt's route-typed $fetch overloads make `nuxt typecheck`
  // fail with TS2321 (excessive stack depth) on this call.
  const plainFetch = $fetch as unknown as (url: string, options: FetchOptions) => Promise<unknown>
  const fetcher: FetchLike = (url, options) => plainFetch(url, options) as Promise<ApiEnvelope<never>>
  return createApiClient(fetcher)
}

function normalizePath(path: string) {
  return path.startsWith('/') ? path : `/${path}`
}

export const SIGN_IN_HINT = 'Sign in via Tailscale or run `couchfusion-server login-url` on the VPS.'

function normalizeApiError(error: unknown): ApiClientError {
  const candidate = error as {
    data?: { error?: ApiErrorPayload }
    response?: { _data?: { error?: ApiErrorPayload }; status?: number }
    status?: number
    statusCode?: number
  }
  const payload = candidate?.data?.error ?? candidate?.response?._data?.error
  const status = candidate?.statusCode ?? candidate?.status ?? candidate?.response?.status
  if (payload?.code === 'unauthorized' || (!payload && status === 401)) {
    const message = payload?.message ? `${payload.message} ${SIGN_IN_HINT}` : SIGN_IN_HINT
    return new ApiClientError({ ...(payload ?? {}), code: 'unauthorized', message })
  }
  if (payload?.code && payload.message) return new ApiClientError(payload)
  return new ApiClientError({ code: 'unavailable', message: 'Could not reach the CouchFusion server.' })
}
