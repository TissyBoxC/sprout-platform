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
            admin_permissions: ['*'],
          },
        },
      }),
    })
  })
}

test('shows a retry state instead of empty dashboard data', async ({ page }) => {
  await useAdminSession(page)
  await page.route('**/api/v1/admin/overview', async (route) => {
    await route.fulfill({
      status: 503,
      contentType: 'application/json',
      body: JSON.stringify({
        error: { code: 'service_unavailable', message: 'service unavailable' },
      }),
    })
  })

  await page.goto('/')

  await expect(page.getByText('服务暂时不可用，请稍后重试')).toBeVisible()
  await expect(page.getByRole('button', { name: '重新加载' })).toBeVisible()
  await expect(page.getByText('暂时没有可显示的数据，请稍后重新加载。')).toHaveCount(0)
})

test('shows a retry state instead of empty device data', async ({ page }) => {
  await useAdminSession(page)
  await page.route('**/api/v1/admin/devices', async (route) => {
    await route.fulfill({
      status: 503,
      contentType: 'application/json',
      body: JSON.stringify({
        error: { code: 'service_unavailable', message: 'service unavailable' },
      }),
    })
  })

  await page.goto('/devices')

  await expect(page.getByText('服务暂时不可用，请稍后重试')).toBeVisible()
  await expect(page.getByRole('button', { name: '重新加载' })).toBeVisible()
  await expect(page.getByText('还没有绑定设备。家长完成设备连接后，设备会显示在这里。')).toHaveCount(
    0,
  )
})
