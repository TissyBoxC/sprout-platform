import { expect, test, type Page, type Route } from '@playwright/test'

interface UiTextVersionPayload {
  package_id: string
  package_version: number
  language: string
  platform: string
  min_client_version: string
  status: string
  signature_key_id: string
  revision: number
  created_at: string
  updated_at: string
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
          },
        },
      }),
    })
  })
}

function jsonResponse(
  data: unknown,
  status = 200,
): { status: number; contentType: string; body: string } {
  return {
    status,
    contentType: 'application/json',
    body: JSON.stringify({
      schema_version: '1.0.0',
      request_id: 'test-ui-text',
      data,
      error: null,
    }),
  }
}

function versionSummary(
  overrides: Partial<UiTextVersionPayload> & {
    entry_count?: number
    is_published?: boolean
    is_newest?: boolean
  } = {},
): Record<string, unknown> {
  return {
    package: {
      package_id: 'home_welcome',
      package_version: 1,
      language: 'zh-CN',
      platform: 'parent_app',
      min_client_version: '',
      status: 'draft',
      signature_key_id: '',
      revision: 0,
      created_at: '2026-10-11T09:00:00Z',
      updated_at: '2026-10-11T09:00:00Z',
      ...overrides,
    },
    entry_count: overrides.entry_count ?? 1,
    is_published: overrides.is_published ?? false,
    is_newest: overrides.is_newest ?? true,
    published_for: false,
  }
}

async function setupRoutes(
  page: Page,
  options: {
    versions?: Array<Record<string, unknown>>
  } = {},
): Promise<{ createPayloads: Array<Record<string, unknown>> }> {
  const createPayloads: Array<Record<string, unknown>> = []

  await page.route('**/api/v1/admin/ui-text/packages/drafts', async (route: Route) => {
    createPayloads.push(route.request().postDataJSON() as Record<string, unknown>)
    await route.fulfill(
      jsonResponse({
        package: versionSummary().package,
        entries: [
          {
            ui_text_key: 'home.title',
            value: '欢迎回家',
            max_length: 40,
            variables: [],
          },
        ],
      }),
    )
  })

  await page.route('**/api/v1/admin/ui-text/packages?**', async (route) => {
    await route.fulfill(
      jsonResponse({
        versions: options.versions ?? [versionSummary()],
        page: 1,
        page_size: 20,
        total: (options.versions ?? [versionSummary()]).length,
      }),
    )
  })
  await page.route('**/api/v1/admin/ui-text/packages', async (route) => {
    await route.fulfill(
      jsonResponse({
        versions: options.versions ?? [versionSummary()],
        page: 1,
        page_size: 20,
        total: (options.versions ?? [versionSummary()]).length,
      }),
    )
  })

  await page.route('**/api/v1/admin/ui-text/packages/*/*/*/*', async (route) => {
    await route.fulfill(
      jsonResponse({
        package: versionSummary().package,
        entries: [
          {
            ui_text_key: 'home.title',
            value: '欢迎回家',
            max_length: 40,
            variables: [],
          },
        ],
      }),
    )
  })

  return { createPayloads }
}

test('lists remote ui text versions with status', async ({ page }) => {
  await useAdminSession(page)
  await setupRoutes(page, {
    versions: [
      versionSummary({ package_version: 2, status: 'in_review', is_published: false }),
      versionSummary({ package_version: 1, status: 'published', is_published: true }),
    ],
  })

  await page.goto('/ui-text')

  await expect(page.getByRole('heading', { name: '界面文案管理' })).toBeVisible()
  await expect(page.getByText('home_welcome').first()).toBeVisible()
  await expect(page.getByText('待审核')).toBeVisible()
  await expect(page.getByText('已发布')).toBeVisible()
})

test('creates a draft with entries', async ({ page }) => {
  await useAdminSession(page)
  const { createPayloads } = await setupRoutes(page)

  await page.goto('/ui-text')
  await page.getByRole('button', { name: '新建文案草稿' }).click()

  await page.getByLabel('文案标识').fill('home_welcome')
  await page.getByLabel('键名').fill('home.title')
  await page.getByLabel('文案').fill('欢迎回家')
  await page.getByRole('button', { name: '创建草稿' }).click()

  await expect.poll(() => createPayloads.length).toBe(1)
  expect(createPayloads[0]).toMatchObject({
    package_id: 'home_welcome',
    language: 'zh-CN',
    platform: 'parent_app',
  })
  const entries = createPayloads[0]?.entries as Array<Record<string, unknown>>
  expect(entries[0]).toMatchObject({ ui_text_key: 'home.title', value: '欢迎回家' })
})

test('submits a draft for review', async ({ page }) => {
  await useAdminSession(page)
  const actionPayloads: string[] = []
  await setupRoutes(page)
  await page.route('**/api/v1/admin/ui-text/packages/*/*/*/*/submit', async (route: Route) => {
    actionPayloads.push(route.request().url())
    await route.fulfill(jsonResponse({ package: versionSummary({ status: 'in_review' }).package }))
  })

  await page.goto('/ui-text')
  await page.getByRole('button', { name: '提交审核' }).first().click()

  await expect.poll(() => actionPayloads.length).toBe(1)
  expect(actionPayloads[0]).toContain('/home_welcome/zh-CN/parent_app/1/submit')
})
