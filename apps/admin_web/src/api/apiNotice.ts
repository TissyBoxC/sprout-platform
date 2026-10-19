/// A successful response may carry a non-fatal warning when the business
/// operation committed but a required security record did not. This is a
/// notice, not an error: callers must keep the successful domain result and
/// surface the warning separately.
export interface ApiNotice {
  code: string
  message: string
}

export const AUDIT_WRITE_FAILED_NOTICE_CODE = 'audit_write_failed'

export const AUDIT_WRITE_FAILED_NOTICE =
  '操作已保存，但安全记录未完成。请联系管理员核对。'

/// Reads `notice` from either the standard `{ data, notice }` envelope or an
/// already-unwrapped payload. Unknown or empty notices are ignored so a server
/// cannot turn an arbitrary success response into a user-facing warning.
export function readApiNotice(envelope: unknown): ApiNotice | null {
  const root = recordValue(envelope)
  const notice = recordValue(root.notice)
  const code = stringValue(notice.code).trim()
  if (code === '') {
    return null
  }

  return {
    code,
    message: stringValue(notice.message).trim(),
  }
}

/// Returns the operator-facing warning for a notice. Non-audit notices use the
/// backend message when it is safe and useful, while audit failures always use
/// one consistent Chinese message across every management screen.
export function apiNoticeWarning(notice: ApiNotice | null | undefined): string {
  if (notice === null || notice === undefined) {
    return ''
  }
  if (notice.code === AUDIT_WRITE_FAILED_NOTICE_CODE) {
    return AUDIT_WRITE_FAILED_NOTICE
  }
  return notice.message.trim() || '操作已保存，但还有一项后续处理没有完成。请联系管理员核对。'
}

/// A mutation return value that preserves both the domain result and the
/// optional non-fatal notice. `value` is null only for operations whose API
/// intentionally returns no body.
export interface ApiMutationResult<T> {
  value: T
  notice: ApiNotice | null
}

function recordValue(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}

function stringValue(value: unknown): string {
  return typeof value === 'string' ? value : ''
}
