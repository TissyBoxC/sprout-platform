import { expect, test, type Page } from '@playwright/test'

async function useFamilyAdmin(page: Page): Promise<void> {
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
            admin_permissions: ['family.read', 'family.manage'],
          },
        },
      }),
    })
  })
}

test('shows a security warning after a family profile is saved', async ({ page }) => {
  await useFamilyAdmin(page)
  await page.route('**/api/v1/admin/families**', async (route) => {
    if (route.request().method() === 'PUT' && route.request().url().endsWith('/profile')) {
      await route.fulfill({
        status: 202,
        contentType: 'application/json',
        body: JSON.stringify({
          data: {},
          notice: {
            code: 'audit_write_failed',
            message: 'technical detail is replaced by the stable operator copy',
          },
        }),
      })
      return
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          accounts: [
            {
              parent_account_id: 'parent-audit',
              display_name: '安全记录测试家长',
              guardian_family_name: '测试家庭',
              phone: '13800000000',
              status: 'active',
            },
          ],
          total: 1,
          page: 1,
          page_size: 20,
          total_pages: 1,
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/ai-models', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { models: [] } }),
    })
  })

  await page.goto('/families')
  await page.getByRole('button', { name: '管理', exact: true }).click()
  await page.getByRole('button', { name: '保存家长资料' }).click()

  const warning = page.getByRole('alert').filter({
    hasText: '操作已保存，但安全记录未完成。请联系管理员核对。',
  })
  await expect(warning).toBeVisible()
  await expect(page.getByText('家长资料已保存。')).toBeVisible()
  await expect(page.getByText('technical detail is replaced')).toHaveCount(0)
})
