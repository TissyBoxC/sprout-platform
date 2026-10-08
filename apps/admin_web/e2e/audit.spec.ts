import { expect, test, type Page } from '@playwright/test'

async function useAdminSession(page: Page): Promise<void> {
  await page.addInitScript(() => {
    globalThis.sessionStorage.setItem('sprout.admin.accessToken', 'test-access-token')
    globalThis.sessionStorage.setItem('sprout.admin.refreshToken', 'test-refresh-token')
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
          },
        },
      }),
    })
  })
}

test('filters, pages, and redacts administrator operation records', async ({ page }) => {
  await useAdminSession(page)

  const requestedQueries: URLSearchParams[] = []
  await page.route('**/api/v1/admin/audit**', async (route) => {
    const requestUrl = new URL(route.request().url())
    requestedQueries.push(requestUrl.searchParams)
    const pageNumber = Number(requestUrl.searchParams.get('page') ?? '1')
    const hasFilters =
      requestUrl.searchParams.get('action') === 'admin.settings.updated' &&
      requestUrl.searchParams.get('target_account_id') === 'parent-1'

    if (hasFilters) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            items: [
              {
                id: 'audit-21',
                actor_account_id: 'admin-1',
                actor_display_name: '品牌管理员',
                target_account_id: 'parent-1',
                target_display_name: '小芽家长',
                action: 'admin.settings.updated',
                detail: {
                  scope: 'account',
                  status: 'saved',
                },
                created_at: '2026-10-07T09:00:00Z',
              },
            ],
            total: 1,
            page: 1,
            page_size: 20,
            actions: ['admin.settings.updated', 'privacy.export.requested'],
          },
        }),
      })
      return
    }

    if (pageNumber >= 2) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            items: [
              {
                id: 'audit-21',
                actor_account_id: 'admin-1',
                actor_display_name: '品牌管理员',
                target_account_id: '',
                target_display_name: '',
                action: 'admin.settings.updated',
                detail: {
                  scope: 'account',
                  status: 'saved',
                },
                created_at: '2026-10-07T09:00:00Z',
              },
            ],
            total: 21,
            page: 2,
            page_size: 20,
            actions: ['admin.settings.updated', 'privacy.export.requested'],
          },
        }),
      })
      return
    }

    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          items: [
            {
              id: 'audit-1',
              actor_account_id: 'admin-1',
              actor_display_name: '品牌管理员',
              target_account_id: 'parent-1',
              target_display_name: '小芽家长',
              action: 'privacy.export.requested',
              detail: {
                scope: 'personal_data',
                status: 'completed',
                token: 'must-not-render',
                password: 'must-not-render',
                request_body: { chat: 'must-not-render' },
                audio_url: 'https://example.test/audio.wav',
                image: 'data:image/png;base64,must-not-render',
                authorization: 'Bearer must-not-render',
                nested: { safe: 'hidden-because-not-scalar' },
                items: ['also-hidden'],
              },
              created_at: '2026-10-07T08:00:00Z',
            },
          ],
          total: 21,
          page: 1,
          page_size: 20,
          actions: ['privacy.export.requested', 'admin.settings.updated'],
        },
      }),
    })
  })

  await page.goto('/audit')

  await expect(page.getByRole('heading', { name: '操作审计' })).toBeVisible()
  await expect(page.getByText('共 21 条')).toBeVisible()
  const firstRow = page.locator('table tbody tr').first()
  await expect(firstRow.locator('.action-cell strong')).toHaveText('导出个人数据')
  await expect(firstRow.getByText('personal_data')).toBeVisible()
  await expect(page.getByText('must-not-render')).toHaveCount(0)
  await expect(page.getByText('hidden-because-not-scalar')).toHaveCount(0)
  await expect(page.getByText('also-hidden')).toHaveCount(0)

  await page.getByRole('button', { name: '下一页' }).click()
  await expect(page.getByText('第 21–21 条，共 21 条')).toBeVisible()
  expect(requestedQueries.at(-1)?.get('page')).toBe('2')

  await page.getByLabel('操作类型').selectOption('admin.settings.updated')
  await page.getByLabel('目标账号').fill('parent-1')
  await page.getByLabel('开始日期').fill('2026-10-01')
  await page.getByLabel('结束日期').fill('2026-10-07')
  await page.getByRole('button', { name: '查询记录' }).click()

  await expect(page.getByText('第 1–1 条，共 1 条')).toBeVisible()
  const filters = requestedQueries.at(-1)
  expect(filters?.get('action')).toBe('admin.settings.updated')
  expect(filters?.get('target_account_id')).toBe('parent-1')
  expect(filters?.get('page')).toBe('1')
  expect(filters?.get('from')).toBe(new Date('2026-10-01T00:00:00').toISOString())
  expect(filters?.get('to')).toBe(new Date('2026-10-07T23:59:59.999').toISOString())
})

test('shows audit loading failures and empty results without leaking errors', async ({
  page,
}) => {
  await useAdminSession(page)

  let requests = 0
  await page.route('**/api/v1/admin/audit**', async (route) => {
    requests += 1
    if (requests === 1) {
      await route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({
          data: null,
          error: {
            code: 'temporarily_unavailable',
            message: 'internal trace and secret value',
            retryable: true,
          },
        }),
      })
      return
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          items: [],
          total: 0,
          page: 1,
          page_size: 20,
          actions: [],
        },
      }),
    })
  })

  await page.goto('/audit')

  await expect(page.getByText('服务暂时不可用，请稍后重试')).toBeVisible()
  await expect(page.getByText('internal trace and secret value')).toHaveCount(0)
  await page.getByRole('button', { name: '重新加载' }).click()
  await expect(page.getByText('还没有操作记录')).toBeVisible()
  expect(requests).toBe(2)
})
