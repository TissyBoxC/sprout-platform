import { expect, test, type Page, type Route } from '@playwright/test'

interface SessionOptions {
  permissions: string[]
}

async function useAdminSession(page: Page, options: SessionOptions): Promise<void> {
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
            admin_permissions: options.permissions,
          },
        },
      }),
    })
  })
}

function jsonResponse(data: unknown, status = 200): {
  status: number
  contentType: string
  body: string
} {
  return {
    status,
    contentType: 'application/json',
    body: JSON.stringify({
      schema_version: '1.0.0',
      request_id: 'test-family-rbac',
      data,
      error: null,
    }),
  }
}

const familyPayload = {
  accounts: [
    {
      parent_account_id: 'family-rbac-1',
      phone: '13800000001',
      display_name: '林妈妈',
      guardian_family_name: '林',
      child_nickname: '小芽',
      child_birthday: '2022-01-02',
      status: 'active',
      created_at: '2026-10-01T08:00:00Z',
      ai_account: {
        provider_account_id: 'ai-rbac-1',
        status: 'active',
        balance_usd: 8,
        concurrency_limit: 2,
        available_models: ['model-a'],
        selected_models: ['model-a'],
        allowed_models: ['model-a'],
        credential_ready: true,
        updated_at: '2026-10-02T08:00:00Z',
      },
    },
  ],
  pagination: {
    page: 1,
    page_size: 20,
    total: 1,
    total_pages: 1,
  },
}

async function setupReadRoutes(page: Page): Promise<{ writeRequests: string[] }> {
  const writeRequests: string[] = []

  await page.route('**/api/v1/admin/families**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (request.method() !== 'GET') {
      writeRequests.push(`${request.method()} ${path}`)
      await route.fulfill({ status: 500 })
      return
    }
    if (path === '/api/v1/admin/families') {
      await route.fulfill(jsonResponse(familyPayload))
      return
    }
    if (path.endsWith('/devices')) {
      await route.fulfill(
        jsonResponse({
          devices: [
            {
              device_id: 'device-rbac-1',
              device_name: '初芽一号',
              hardware_model: '初芽',
              firmware_version: '1.2.3',
              capabilities: ['display'],
              bound_at: '2026-10-03T08:00:00Z',
              updated_at: '2026-10-03T08:00:00Z',
              runtime: {
                is_online: true,
                connection_state: 'online',
                transport: 'mqtt',
                reported_at: '2026-10-03T09:00:00Z',
                received_at: '2026-10-03T09:00:00Z',
              },
            },
          ],
        }),
      )
      return
    }
    if (path.endsWith('/ai-usage')) {
      await route.fulfill(
        jsonResponse({
          usage: {
            provider_account_id: 'ai-rbac-1',
            start_time: '2026-10-03T00:00:00Z',
            end_time: '2026-10-04T00:00:00Z',
            requests: 3,
            input_tokens: 20,
            output_tokens: 30,
            total_tokens: 50,
            cost_usd: 0.2,
            actual_cost_usd: 0.1,
            average_duration_ms: 210,
            models: [],
          },
        }),
      )
      return
    }
    if (path.endsWith('/members')) {
      await route.fulfill(jsonResponse({ members: [], invitations: [], revision: 1 }))
      return
    }
    await route.fulfill(jsonResponse({}))
  })

  await page.route('**/api/v1/admin/ai-models', async (route) => {
    await route.fulfill(
      jsonResponse({
        models: [{ id: 'model-a', latency_ms: 120 }],
      }),
    )
  })
  await page.route('**/api/v1/admin/ai-account-defaults', async (route) => {
    await route.fulfill(
      jsonResponse({
        default_balance_usd: 8,
        default_concurrency: 2,
        source: 'sub2api',
      }),
    )
  })
  await page.route('**/api/v1/admin/ai-gateway/child-policy', async (route) => {
    await route.fulfill(jsonResponse({}))
  })

  return { writeRequests }
}

test('hides every family and AI mutation for a read-only parent viewer', async ({ page }) => {
  await useAdminSession(page, { permissions: ['family.read', 'ai_accounts.read'] })
  const { writeRequests } = await setupReadRoutes(page)
  const browserErrors: string[] = []
  page.on('pageerror', (error) => {
    browserErrors.push(error.message)
  })
  page.on('console', (message) => {
    if (message.type() === 'error') {
      browserErrors.push(message.text())
    }
  })

  await page.goto('/families')
  await page.waitForTimeout(1000)
  if (browserErrors.length > 0) {
    console.log(`browser-errors=${JSON.stringify(browserErrors)} url=${page.url()}`)
  }
  await expect(page.getByRole('heading', { name: '家长账号' })).toBeVisible()
  await expect(page.getByRole('button', { name: '新建家长账号' })).toHaveCount(0)
  await expect(
    page.getByText('你当前只有家长查看权限，不能创建家长、修改资料或重置密码。'),
  ).toBeVisible()

  await page.getByRole('button', { name: '管理' }).click()
  const drawer = page.locator('.family-drawer')
  await expect(drawer).toBeVisible()
  await expect(drawer.getByLabel('家长称呼')).toBeDisabled()
  await expect(drawer.getByLabel('剩余额度')).toBeDisabled()
  await expect(drawer.getByRole('button', { name: '保存家长资料' })).toHaveCount(0)
  await expect(drawer.getByRole('button', { name: '保存 AI 设置' })).toHaveCount(0)
  await expect(drawer.getByRole('button', { name: '重置密码' })).toHaveCount(0)
  expect(writeRequests).toEqual([])
})

test('requires both family.manage and ai_accounts.manage to create a parent', async ({ page }) => {
  await useAdminSession(page, {
    permissions: ['family.read', 'ai_accounts.read', 'family.manage'],
  })
  const { writeRequests } = await setupReadRoutes(page)

  await page.goto('/families')

  await expect(page.getByRole('button', { name: '新建家长账号' })).toHaveCount(0)
  await expect(
    page.getByText('创建家长会自动开通 AI 服务，因此还需要 AI 账号管理权限。'),
  ).toBeVisible()
  expect(writeRequests).toEqual([])
})

test('lets AI-only managers inspect family data without issuing writes', async ({ page }) => {
  await useAdminSession(page, {
    permissions: ['family.read', 'ai_accounts.read', 'ai_accounts.manage'],
  })
  const { writeRequests } = await setupReadRoutes(page)

  await page.goto('/families')
  await expect(page.getByRole('button', { name: '新建家长账号' })).toHaveCount(0)
  await page.getByRole('button', { name: '管理' }).click()

  const drawer = page.locator('.family-drawer')
  await expect(drawer.getByLabel('剩余额度')).toBeEnabled()
  await expect(drawer.getByRole('button', { name: '保存 AI 设置' })).toBeVisible()
  await expect(drawer.getByRole('button', { name: '保存家长资料' })).toHaveCount(0)
  await expect(drawer.getByRole('button', { name: '重置密码' })).toHaveCount(0)
  expect(writeRequests).toEqual([])
})
