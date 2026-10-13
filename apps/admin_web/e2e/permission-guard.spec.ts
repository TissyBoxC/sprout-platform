import { expect, test, type Page } from '@playwright/test'

async function useAdminSession(page: Page, permissions: string[]): Promise<void> {
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
            admin_permissions: permissions,
          },
        },
      }),
    })
  })
}

test('hides navigation and blocks direct URL access without permission', async ({ page }) => {
  await useAdminSession(page, ['content.read'])

  await page.goto('/settings')

  await expect(page.getByRole('heading', { name: '你没有权限访问此页面' })).toBeVisible()
  await expect(page.getByRole('link', { name: '系统设置' })).toHaveCount(0)
  await expect(page.getByRole('link', { name: '内容库' })).toBeVisible()
})

test('shows only the navigation and pages granted to the administrator', async ({ page }) => {
  await useAdminSession(page, ['settings.read', 'feature_center.read'])

  await page.goto('/settings')

  await expect(page.getByRole('heading', { name: '系统设置' })).toBeVisible()
  await expect(page.getByRole('link', { name: '系统设置' })).toBeVisible()
  await expect(page.getByRole('link', { name: '功能中心' })).toBeVisible()
  await expect(page.getByRole('link', { name: '家长账号' })).toHaveCount(0)
})
