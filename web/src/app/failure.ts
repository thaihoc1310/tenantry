// showFailure tells the person why a change was refused: the plain words of
// its error code in English and in Vietnamese, the server's own message and
// the request id to quote (IMPLEMENTATION-PLAN §6.3, M10 step 8).
import { ElNotification } from 'element-plus'
import { h } from 'vue'
import { ApiError } from '@/api/client'
import { errorText } from '@/i18n/errors'

export function showFailure(err: unknown): void {
  const e = err instanceof ApiError ? err : new ApiError(0, 'INTERNAL', String(err))
  const text = errorText[e.code]
  ElNotification.error({
    title: text.en,
    message: h('div', [
      h('p', { lang: 'vi', class: 'small' }, text.vi),
      h('p', { class: 'muted small' }, `${e.message} (${e.code}${e.requestId ? ` · request ${e.requestId}` : ''})`),
    ]),
  })
}
