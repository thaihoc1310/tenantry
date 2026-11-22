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
  ENTITLEMENT_EXCEEDED: {
    en: 'This Organization has reached its limit. Ask a Platform Admin for more.',
    vi: 'Organization đã chạm giới hạn được cấp. Hãy xin Platform Admin nâng thêm.',
  },
  INVITATION_INVALID: {
    en: 'This invitation link no longer works: it expired, was used or was revoked.',
    vi: 'Link mời không còn dùng được: đã hết hạn, đã dùng hoặc đã bị thu hồi.',
  },
  INVITATION_EMAIL_MISMATCH: {
    en: 'This invitation is for another email. Log in with the invited address.',
    vi: 'Lời mời này dành cho email khác. Hãy đăng nhập bằng đúng email được mời.',
  },
  TRANSFER_NOT_ALLOWED: {
    en: 'Only the owner can hand the Organization over.',
    vi: 'Chỉ Owner mới chuyển giao được Organization.',
  },
  RECIPIENT_NOT_ORG_ADMIN: {
    en: 'The new owner must be an OrgAdmin of this Organization.',
    vi: 'Người nhận phải là OrgAdmin của Organization này.',
  },
  TRANSFER_INVALID: {
    en: 'This handover can no longer be accepted.',
    vi: 'Yêu cầu chuyển giao này không còn chấp nhận được.',
  },
  LEAVE_NOT_ALLOWED: {
    en: 'The owner cannot leave. Hand the Organization over first.',
    vi: 'Owner không thể rời đi. Hãy chuyển giao Organization trước.',
  },
  OWNER_USER_NOT_FOUND: {
    en: 'The owner has never logged in to the portal. Ask them to log in once, then try again.',
    vi: 'Người làm owner chưa từng đăng nhập portal. Nhờ họ đăng nhập một lần rồi thử lại.',
  },
  ENTITLEMENT_BELOW_USAGE: {
    en: 'A limit cannot go below what the Organization already uses, and it keeps at least 3 groups.',
    vi: 'Giới hạn không thể thấp hơn mức Organization đang dùng, và phải có ít nhất 3 group.',
  },
  CIDR_OVERLAP: {
    en: 'This address range overlaps a range already in use.',
    vi: 'Dải địa chỉ này trùng với một dải đang được dùng.',
  },
  CLUSTER_NOT_GRANTED: {
    en: 'The Organization has no grant on this cluster.',
    vi: 'Organization chưa được cấp quyền dùng cluster này.',
  },
  LAST_OWNER_BINDING: {
    en: 'At least one group must keep the TenantOwner role.',
    vi: 'Phải có ít nhất một group giữ vai trò TenantOwner.',
  },
  GROUP_SYNC_PENDING: {
    en: 'A group has not reached the identity provider yet. Try again in a moment.',
    vi: 'Một group chưa đồng bộ sang Keycloak. Hãy thử lại sau giây lát.',
  },
  WORKSPACE_NOT_READY: {
    en: 'The Workspace is busy with another change. Try again once it is Ready.',
    vi: 'Workspace đang bận với một thay đổi khác. Hãy thử lại khi nó Ready.',
  },
  CLUSTER_GRANT_QUOTA_EXCEEDED: {
    en: 'This does not fit in what the Organization was granted on the cluster.',
    vi: 'Yêu cầu vượt phần Organization được cấp trên cluster này.',
  },
  CLUSTER_GRANT_BELOW_ALLOCATION: {
    en: 'The grant cannot go below what the Workspaces already hold.',
    vi: 'Không thể hạ grant xuống dưới phần các Workspace đang giữ.',
  },
  WORKSPACE_QUOTA_BELOW_USAGE: {
    en: 'The Workspace already uses more than this. Free some first.',
    vi: 'Workspace đang dùng nhiều hơn mức này. Hãy giải phóng bớt trước.',
  },
  ORGANIZATION_NOT_EMPTY: {
    en: 'Delete its Workspaces first, and ask a Platform Admin to take back its dedicated nodes.',
    vi: 'Hãy xóa các Workspace trước và nhờ Platform Admin thu hồi node dedicated.',
  },
  COMPUTE_POOL_CAPACITY_EXCEEDED: {
    en: 'The shared pool has no room for this. Ask less, or ask a Platform Admin for more nodes.',
    vi: 'Shared pool không còn đủ chỗ. Hãy xin ít hơn, hoặc nhờ Platform Admin thêm node.',
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
