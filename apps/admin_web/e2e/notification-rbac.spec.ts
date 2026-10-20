import { expect, test, type Page } from '@playwright/test'

async function useAdminSessionWithPermissions(
  page: Page,
  permissions: string[],
): Promise<void> {
  await page.addInitScript(() => {
    globalThis.sessionStorage.setItem(
      'sprout.admin.accessToken',
      'test-access-token',
    )
    globalThis.sessionStorage.setItem(
      'sprout.admin.refreshToken',
      'test-refresh-token',
    )
  })
  await page.route('**/api/v1/auth/me', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          account: {
            id: 'test-admin',
            email: 'admin@sprout.local',
            display_name: '管理员',
            role: 'admin',
            admin_permissions: permissions,
          },
        },
      }),
    })
  })
}

const notification = {
  id: 'notification-1',
  category: 'system_announcement',
  severity: 'info',
  title: '服务维护提醒',
  body: '今晚将进行例行维护。',
  action_path: '',
  action_label: '',
  audience: 'all_parents',
  parent_account_id: '',
  device_id: '',
  display_duration_seconds: 0,
  channels: ['in_app'],
  source: 'administrator',
  created_by: 'test-admin',
  publish_at: '2026-10-18T09:00:00Z',
  expires_at: '',
  created_at: '2026-10-18T09:00:00Z',
  updated_at: '2026-10-18T09:00:00Z',
  delivery_total: 1,
  delivery_pending: 0,
  delivery_delivered: 1,
  delivery_read: 0,
  delivery_failed: 0,
}

async function mockNotificationSurface(
  page: Page,
  onWrite: (method: string, path: string) => void,
): Promise<void> {
  await page.route('**/api/v1/admin/notifications**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (request.method() === 'GET' && path.endsWith('/stats')) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            total: 1,
            broadcast_count: 1,
            pending_delivery_count: 0,
            failed_delivery_count: 0,
          },
        }),
      })
      return
    }
    if (request.method() === 'GET') {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            items: [notification],
            total: 1,
            page: 1,
            page_size: 20,
            total_pages: 1,
          },
        }),
      })
      return
    }
    onWrite(request.method(), path)
    if (request.method() === 'POST') {
      await route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({ data: { notification } }),
      })
      return
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: {} }),
    })
  })
}

test('keeps notification mutations hidden with only notifications.read', async ({
  page,
}) => {
  await useAdminSessionWithPermissions(page, ['notifications.read', 'family.read'])
  const writes: string[] = []
  let familyRequests = 0
  await mockNotificationSurface(page, (method, path) => {
    writes.push(`${method} ${path}`)
  })
  await page.route('**/api/v1/admin/families**', async (route) => {
    familyRequests += 1
    await route.fulfill({ status: 500 })
  })

  await page.goto('/notifications')

  await expect(page.getByRole('heading', { name: '消息通知' })).toBeVisible()
  await expect(
    page.getByText(/你当前只有查看权限。可以查看通知、筛选记录和送达情况/),
  ).toBeVisible()
  await expect(page.getByRole('button', { name: '发布通知' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '删除' })).toHaveCount(0)
  await expect(page.getByText('服务维护提醒')).toBeVisible()
  await expect(page.getByText('只读', { exact: true })).toBeVisible()
  expect(writes).toEqual([])
  expect(familyRequests).toBe(0)
})

test('publishes and deletes with notifications.manage and can choose a family', async ({
  page,
}) => {
  await useAdminSessionWithPermissions(page, [
    'notifications.read',
    'notifications.manage',
    'family.read',
  ])
  const writes: string[] = []
  let familyRequests = 0
  await mockNotificationSurface(page, (method, path) => {
    writes.push(`${method} ${path}`)
  })
  await page.route('**/api/v1/admin/families**', async (route) => {
    familyRequests += 1
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          accounts: [
            {
              parent_account_id: 'parent-1',
              display_name: '小芽家长',
              phone: '13800001111',
            },
          ],
          total: 1,
          page: 1,
          page_size: 100,
          total_pages: 1,
        },
      }),
    })
  })

  await page.goto('/notifications')

  await page.getByRole('button', { name: '发布通知' }).click()
  const composer = page.getByRole('dialog', { name: '发布通知' })
  await expect(composer).toBeVisible()
  await expect(
    composer.getByRole('button', { name: '指定家长' }),
  ).toBeVisible()
  await expect
    .poll(() => familyRequests)
    .toBeGreaterThan(0)

  await composer.getByLabel('标题').fill('周末活动提醒')
  await composer
    .getByRole('textbox', { name: '内容', exact: true })
    .fill('周六上午十点开始亲子阅读活动。')
  await composer.getByRole('button', { name: '发布通知' }).click()

  await expect(composer).toHaveCount(0)
  expect(writes.filter((entry) => entry.startsWith('POST '))).toHaveLength(1)
})

test('does not request families when notifications.manage lacks family.read', async ({
  page,
}) => {
  await useAdminSessionWithPermissions(page, [
    'notifications.read',
    'notifications.manage',
  ])
  let familyRequests = 0
  await mockNotificationSurface(page, () => undefined)
  await page.route('**/api/v1/admin/families**', async (route) => {
    familyRequests += 1
    await route.fulfill({ status: 500 })
  })

  await page.goto('/notifications')
  await page.getByRole('button', { name: '发布通知' }).click()

  const composer = page.getByRole('dialog', { name: '发布通知' })
  await expect(composer).toBeVisible()
  await expect(
    composer.getByRole('button', { name: '指定家长' }),
  ).toHaveCount(0)
  await expect(
    composer.getByText(/没有读取家长账号的权限/),
  ).toBeVisible()
  expect(familyRequests).toBe(0)
})
