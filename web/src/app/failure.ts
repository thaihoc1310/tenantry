// showFailure tells the person why a change was refused: the plain words of
// its error code, the server's own message and the request id to quote.
import { ElNotification } from 'element-plus'
import { ApiError } from '@/api/client'
import { errorText } from '@/i18n/errors'

export function showFailure(err: unknown): void {
  const e = err instanceof ApiError ? err : new ApiError(0, 'INTERNAL', String(err))
  ElNotification.error({
    title: errorText[e.code].en,
    message: `${e.message} (${e.code}${e.requestId ? ` · request ${e.requestId}` : ''})`,
  })
}
