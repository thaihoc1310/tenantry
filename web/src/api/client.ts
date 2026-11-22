// The one way the portal talks to the Platform API (IMPLEMENTATION-PLAN §6.2):
// typed from the OpenAPI document, the CSRF header on every write, a 401 sends
// the browser to log in, and every failure becomes an ApiError.
import createClient, { type Middleware } from 'openapi-fetch'
import type { components, paths } from './schema'

export type Schemas = components['schemas']
export type ErrorCode = Schemas['ErrorCode']

// Set by GET /auth/callback; readable by this page only (__Host- prefix).
const csrfCookie = '__Host-tenantry_csrf'

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: ErrorCode | 'NETWORK',
    message: string,
    readonly requestId = '',
    readonly field = '',
  ) {
    super(message)
  }
}

export function csrfToken(): string {
  const prefix = csrfCookie + '='
  const c = document.cookie.split('; ').find((c) => c.startsWith(prefix))
  return c ? decodeURIComponent(c.slice(prefix.length)) : ''
}

// login sends the browser to Keycloak and back to where it is now.
export function login(returnTo = location.pathname + location.search): void {
  location.assign('/auth/login?returnTo=' + encodeURIComponent(returnTo))
}

const safe = new Set(['GET', 'HEAD', 'OPTIONS'])

const session: Middleware = {
  onRequest({ request }) {
    if (!safe.has(request.method)) request.headers.set('X-CSRF-Token', csrfToken())
    return request
  },
  onResponse({ response }) {
    // The session ended (logged out elsewhere, expired, or the account was
    // disabled): log in again and come back here.
    if (response.status === 401) login()
    return response
  },
}

export const api = createClient<paths>({ baseUrl: '/api/v1' })
api.use(session)

// unwrap turns an openapi-fetch result into its data or an ApiError.
export async function unwrap<T>(call: Promise<{ data?: T; error?: unknown; response: Response }>): Promise<T> {
  let r: Awaited<typeof call>
  try {
    r = await call
  } catch {
    throw new ApiError(0, 'NETWORK', 'The platform could not be reached. Check your connection and try again.')
  }
  if (r.response.ok) return r.data as T
  const e = r.error as Partial<Schemas['ErrorResponse']> | undefined
  const field = typeof e?.details?.field === 'string' ? e.details.field : ''
  throw new ApiError(r.response.status, e?.code ?? 'INTERNAL', e?.message ?? r.response.statusText, e?.requestId, field)
}

// A new key per form opening: a repeated click or a flaky network then
// replays the first answer instead of doing the work twice (§6.3).
export function idempotencyKey(): string {
  return crypto.randomUUID()
}
