import { expect, test, type Page, type Route } from '@playwright/test'

interface OfflinePolicyPayload {
  offline_enabled: boolean
  max_cached_packages: number
  max_cache_bytes: number
  cache_ttl_hours: number
  download_over_wifi_only: boolean
  revision: number
}

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
            admin_permissions: ['content.read'],
          },
        },
      }),
    })
  })
}

function jsonResponse(
  data: unknown,
  status = 200,
): {
  status: number
  contentType: string
  body: string
} {
  return {
    status,
    contentType: 'application/json',
    body: JSON.stringify({
      schema_version: '1.0.0',
      request_id: 'test-offline-policy',
      data,
      error: null,
    }),
  }
}

async function setupRoutes(
  page: Page,
  options: {
    policy?: Partial<OfflinePolicyPayload>
    acknowledgements?: Array<{ device_id: string; revision: number; up_to_date: boolean }>
  } = {},
): Promise<{ updatePayloads: Array<Record<string, unknown>> }> {
  const updatePayloads: Array<Record<string, unknown>> = []

  await page.route('**/api/v1/admin/families', async (route) => {
    await route.fulfill(
      jsonResponse({
        accounts: [
          {
            parent_account_id: 'family-1',
            phone: '13800000000',
            display_name: '李女士',
            guardian_family_name: '李家庭',
            status: 'active',
          },
        ],
      }),
    )
  })

  await page.route('**/api/v1/admin/content/offline-policy/acknowledgements**', async (route) => {
    await route.fulfill(jsonResponse({ devices: options.acknowledgements ?? [] }))
  })

  await page.route('**/api/v1/admin/content/offline-policy', async (route: Route) => {
    if (route.request().method() === 'PUT') {
      updatePayloads.push(route.request().postDataJSON() as Record<string, unknown>)
      const incoming = route.request().postDataJSON() as Record<string, unknown>
      await route.fulfill(jsonResponse({ ...incoming, revision: 8 }))
      return
    }
    await route.fulfill(
      jsonResponse({
        offline_enabled: true,
        max_cached_packages: 20,
        max_cache_bytes: 512 * 1024 * 1024,
        cache_ttl_hours: 720,
        download_over_wifi_only: true,
        revision: 7,
        ...options.policy,
      }),
    )
  })

  return { updatePayloads }
}

test('loads the published policy and saves a new revision', async ({ page }) => {
  await useAdminSession(page)
  const { updatePayloads } = await setupRoutes(page)

  await page.goto('/content/offline-policy')

  await expect(page.getByRole('heading', { name: '离线下载策略' })).toBeVisible()
  await expect(page.getByText('当前版本')).toBeVisible()

  await page.getByLabel('可缓存内容包数量').fill('30')
  await page.getByRole('button', { name: '保存并下发' }).click()

  await expect(page.getByText('离线下载策略已更新到版本 8')).toBeVisible()
  expect(updatePayloads).toHaveLength(1)
  expect(updatePayloads[0]).toEqual({
    offline_enabled: true,
    max_cached_packages: 30,
    max_cache_bytes: 512 * 1024 * 1024,
    cache_ttl_hours: 720,
    download_over_wifi_only: true,
  })
})

test('lists each device revision after selecting a family', async ({ page }) => {
  await useAdminSession(page)
  await setupRoutes(page, {
    acknowledgements: [
      { device_id: 'device-online', revision: 7, up_to_date: true },
      { device_id: 'device-stale', revision: 3, up_to_date: false },
    ],
  })

  await page.goto('/content/offline-policy')

  await page.getByRole('combobox').selectOption('family-1')
  await page.getByRole('button', { name: '查询' }).click()

  await expect(page.getByText('device-online')).toBeVisible()
  await expect(page.getByText('已是最新')).toBeVisible()
  await expect(page.getByText('device-stale')).toBeVisible()
  await expect(page.getByText('落后版本（当前 3）')).toBeVisible()
})

test('flags out-of-range cache values before saving', async ({ page }) => {
  await useAdminSession(page)
  const { updatePayloads } = await setupRoutes(page)

  await page.goto('/content/offline-policy')

  await page.getByLabel('缓存空间上限（MB）').fill('4')
  await page.getByRole('button', { name: '保存并下发' }).click()

  await expect(page.getByText('缓存空间需在 16 MB - 8192 MB 之间')).toBeVisible()
  expect(updatePayloads).toHaveLength(0)
})
