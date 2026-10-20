import { expect, test, type Page } from '@playwright/test'

async function useAdminSession(page: Page, permissions: string[] = ['admins.read']): Promise<void> {
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

test('grants operation permissions and sends the selected set', async ({ page }) => {
  await useAdminSession(page, ['admins.read', 'admins.manage'])

  const updatePayloads: unknown[] = []
  await page.route('**/api/v1/admin/permissions**', async (route) => {
    const request = route.request()
    if (request.method() === 'PUT') {
      updatePayloads.push(request.postDataJSON())
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            account: {
              account_id: 'admin-2',
              email: 'review@sprout.local',
              display_name: '内容审核',
              status: 'active',
              created_at: '2026-10-01T00:00:00Z',
              permissions: ['content.read', 'content.review'],
            },
          },
        }),
      })
      return
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          catalog: ['admins.read', 'content.read', 'content.review', 'settings.manage'],
          accounts: [
            {
              account_id: 'admin-1',
              email: 'owner@sprout.local',
              display_name: '品牌管理员',
              status: 'active',
              created_at: '2026-10-01T00:00:00Z',
              permissions: ['*'],
            },
            {
              account_id: 'admin-2',
              email: 'review@sprout.local',
              display_name: '内容审核',
              status: 'active',
              created_at: '2026-10-02T00:00:00Z',
              permissions: ['content.read'],
            },
          ],
        },
      }),
    })
  })

  await page.goto('/admin-permissions')

  await expect(page.getByRole('heading', { name: '管理员权限' })).toBeVisible()
  await page.getByRole('button', { name: /内容审核/ }).click()

  const saveButton = page.getByRole('button', { name: '保存权限' })
  await expect(saveButton).toBeDisabled()

  await page.getByLabel('审核与发布内容').check()
  await expect(saveButton).toBeEnabled()
  await saveButton.click()

  await expect(page.getByText('权限已保存。')).toBeVisible()
  expect(updatePayloads).toHaveLength(1)
  expect(updatePayloads[0]).toEqual({
    permissions: ['content.read', 'content.review'],
  })
})

test('shows the wildcard explanation and disables fine-grained choices', async ({ page }) => {
  await useAdminSession(page, ['admins.read', 'admins.manage'])

  await page.route('**/api/v1/admin/permissions**', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          catalog: ['admins.read', 'content.read', 'content.review'],
          accounts: [
            {
              account_id: 'admin-1',
              email: 'owner@sprout.local',
              display_name: '品牌管理员',
              status: 'active',
              created_at: '2026-10-01T00:00:00Z',
              permissions: ['*'],
            },
          ],
        },
      }),
    })
  })

  await page.goto('/admin-permissions')

  await expect(page.getByText('保存后该管理员可以访问全部管理功能。')).toBeVisible()
  await expect(page.getByLabel('审核与发布内容')).toBeDisabled()
})

test('locks every control for a read-only administrator', async ({ page }) => {
  // admins.read alone may open the page, but changing an assignment needs
  // admins.manage. The page must not offer a mutation it cannot complete.
  await useAdminSession(page, ['admins.read'])

  const updateRequests: string[] = []
  await page.route('**/api/v1/admin/permissions**', async (route) => {
    if (route.request().method() === 'PUT') {
      updateRequests.push(route.request().url())
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          catalog: ['admins.read', 'admins.manage', 'content.read', 'content.review'],
          accounts: [
            {
              account_id: 'admin-2',
              email: 'review@sprout.local',
              display_name: '内容审核',
              status: 'active',
              created_at: '2026-10-02T00:00:00Z',
              permissions: ['content.read'],
            },
          ],
        },
      }),
    })
  })

  await page.goto('/admin-permissions')

  // A view-only administrator must not be offered a mutation. The absence of
  // the save action is the behaviour that matters; the explanatory notice is
  // asserted only once the list has rendered.
  await page.getByRole('button', { name: /内容审核/ }).click()
  await expect(page.getByRole('heading', { name: '内容审核' })).toBeVisible()

  await expect(page.getByRole('button', { name: '保存权限' })).toHaveCount(0)
  await expect(page.getByLabel('审核与发布内容')).toBeDisabled()
  await expect(page.locator('.wildcard-row input[type="checkbox"]')).toBeDisabled()
  await expect(page.getByText('你只能查看权限分配', { exact: false })).toBeVisible()
  expect(updateRequests).toHaveLength(0)
})
