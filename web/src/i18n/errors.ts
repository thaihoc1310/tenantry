// What each stable error code means to a person (IMPLEMENTATION-PLAN §6.3).
// Screens are in English; the explanation comes in Vietnamese too. The codes
// are the ErrorCode enum of the OpenAPI document, so a new code that is not
// listed here fails the type check.
import type { ErrorCode } from '@/api/client'

type Text = { en: string; vi: string }

export const errorText: Record<ErrorCode | 'NETWORK', Text> = {
  VALIDATION_FAILED: { en: 'Some values are not valid.', vi: 'Một số giá trị chưa hợp lệ.' },
  UNAUTHENTICATED: { en: 'Your session has ended. Log in again.', vi: 'Phiên đăng nhập đã hết, hãy đăng nhập lại.' },
  FORBIDDEN: { en: 'You do not have permission to do this.', vi: 'Bạn không có quyền làm việc này.' },
  NOT_FOUND: { en: 'This no longer exists, or you cannot see it.', vi: 'Mục này không còn, hoặc bạn không xem được.' },
  CONFLICT: {
    en: 'Someone changed this in the meantime. Reload and try again.',
    vi: 'Đã có thay đổi khác trong lúc đó. Tải lại rồi thử lại.',
  },
  METHOD_NOT_ALLOWED: { en: 'This action is not supported here.', vi: 'Thao tác này không được hỗ trợ ở đây.' },
  IDEMPOTENCY_KEY_REUSED: {
    en: 'This form was already sent with other values. Open it again.',
    vi: 'Biểu mẫu này đã gửi với giá trị khác. Hãy mở lại biểu mẫu.',
  },
  RATE_LIMITED: {
    en: 'Too many requests. Wait a moment and try again.',
    vi: 'Quá nhiều yêu cầu. Chờ một lát rồi thử lại.',
  },
  SLUG_TAKEN: {
    en: 'This slug is already taken. Choose another one.',
    vi: 'Slug này đã có người dùng. Hãy chọn slug khác.',
  },
  ORG_REQUEST_PENDING: {
    en: 'You already have a request waiting for review.',
    vi: 'Bạn đang có một yêu cầu chờ duyệt.',
  },
  CLUSTER_NOT_READY: {
    en: 'The cluster is not Ready, so it cannot be granted now.',
    vi: 'Cluster chưa ở trạng thái Ready nên chưa cấp được.',
  },
  ORG_NOT_ACTIVE: {
    en: 'This Organization is not approved yet.',
    vi: 'Organization này chưa được duyệt.',
  },
  INTERNAL: {
    en: 'Something went wrong on our side. Your data is safe; try again in a moment.',
    vi: 'Hệ thống gặp lỗi. Dữ liệu của bạn vẫn an toàn; thử lại sau ít phút.',
  },
  NETWORK: {
    en: 'The platform could not be reached. Check your connection and try again.',
    vi: 'Không kết nối được tới hệ thống. Kiểm tra mạng rồi thử lại.',
  },
}
