import assert from 'node:assert/strict'
import test from 'node:test'

import {
  AUDIT_WRITE_FAILED_NOTICE,
  apiNoticeWarning,
  readApiNotice,
} from '../../src/api/apiNotice.js'

test('reads the shared notice alongside a successful payload', () => {
  const notice = readApiNotice({
    data: { updated: true },
    notice: {
      code: 'audit_write_failed',
      message: 'backend detail must not replace the stable copy',
    },
  })

  assert.deepEqual(notice, {
    code: 'audit_write_failed',
    message: 'backend detail must not replace the stable copy',
  })
})

test('uses one stable operator warning for audit failures', () => {
  assert.equal(
    apiNoticeWarning({
      code: 'audit_write_failed',
      message: 'database trigger failed',
    }),
    AUDIT_WRITE_FAILED_NOTICE,
  )
})

test('ignores missing or malformed notices', () => {
  assert.equal(readApiNotice({ data: {} }), null)
  assert.equal(readApiNotice({ notice: null }), null)
  assert.equal(readApiNotice({ notice: { code: '  ', message: 'ignored' } }), null)
  assert.equal(apiNoticeWarning(null), '')
})

test('preserves a safe message for other non-fatal notices', () => {
  assert.equal(
    apiNoticeWarning({
      code: 'delivery_partially_failed',
      message: '通知已发布，部分送达未完成。',
    }),
    '通知已发布，部分送达未完成。',
  )
})
