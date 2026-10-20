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

async function mockReleaseList(
  page: Page,
  onWrite: (method: string, path: string) => void = () => undefined,
): Promise<void> {
  await page.route('**/api/v1/admin/releases**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (request.method() === 'GET') {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            releases: [
              {
                id: 'release-1',
                version: '1.4.0',
                channel: 'stable',
                kind: 'client',
                platform: 'android',
                download_url: 'https://download.example.test/1.4.0/app.apk',
                sha256: 'a'.repeat(64),
                release_notes: '修复启动稳定性',
                is_mandatory: false,
                min_supported_version: '1.0.0',
                status: 'draft',
                published_at: '',
                created_at: '2026-10-18T09:00:00Z',
              },
            ],
            pagination: {
              page: 1,
              page_size: 20,
              total: 1,
              total_pages: 1,
              has_more: false,
            },
          },
        }),
      })
      return
    }
    onWrite(request.method(), path)
    await route.fulfill({
      status: 500,
      contentType: 'application/json',
      body: JSON.stringify({ error: { message: '写请求不应发生' } }),
    })
  })
}

function otaRelease(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    release_id: 'firmware-1',
    firmware_version: '1.2.0',
    hardware_revision: 'rev-a',
    channel: 'stable',
    status: 'draft',
    artifact_key: 'firmware/1.2.0.bin',
    artifact_url: 'https://download.example.test/firmware/1.2.0.bin',
    sha256: 'b'.repeat(64),
    size_bytes: 2048,
    signature_key_id: 'release-key',
    signature_algorithm: 'ed25519',
    signature: 'signed',
    signature_status: 'verified',
    rollback_allowed: true,
    min_source_version: '1.0.0',
    published_at: '',
    canary_percent: 0,
    target_type: 'all',
    target_id: '',
    release_notes: '改进设备稳定性',
    record_version: 1,
    created_at: '2026-10-18T09:00:00Z',
    updated_at: '2026-10-18T09:00:00Z',
    ...overrides,
  }
}

async function mockOtaSurface(
  page: Page,
  onWrite: (method: string, path: string) => void,
): Promise<void> {
  await page.route('**/api/v1/admin/ota/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (request.method() === 'GET') {
      if (path === '/api/v1/admin/ota/statistics') {
        await route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({
            data: {
              published_release_count: 0,
              canary_release_count: 0,
              active_deployment_count: 0,
              succeeded_deployment_count: 0,
              failed_deployment_count: 0,
              rollback_count: 0,
              failure_rate: 0,
              rollback_rate: 0,
            },
          }),
        })
        return
      }
      if (path === '/api/v1/admin/ota/releases') {
        await route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({
            data: {
              releases: [otaRelease()],
              total: 1,
              page: 1,
              page_size: 20,
            },
          }),
        })
        return
      }
      if (path === '/api/v1/admin/ota/releases/firmware-1') {
        await route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({
            data: {
              release: otaRelease(),
              versions: [otaRelease()],
              rollback: { allowed: true, target_version: '1.1.0' },
            },
          }),
        })
        return
      }
      if (path.endsWith('/deployments')) {
        await route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({
            data: { deployments: [], total: 0, page: 1, page_size: 20 },
          }),
        })
        return
      }
    }
    onWrite(request.method(), path)
    if (path === '/api/v1/admin/ota/releases') {
      await route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({ data: { release: otaRelease() } }),
      })
      return
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { release: otaRelease() } }),
    })
  })
}

test('keeps release mutations hidden with only storage.read', async ({ page }) => {
  await useAdminSessionWithPermissions(page, ['storage.read'])
  const writes: string[] = []
  await mockReleaseList(page, (method, path) => {
    writes.push(`${method} ${path}`)
  })

  await page.goto('/releases')

  await expect(page.getByRole('heading', { name: '更新发布' })).toBeVisible()
  await expect(
    page.getByText(/你当前只有查看权限。可以查看更新记录和发布状态/),
  ).toBeVisible()
  await expect(page.getByRole('button', { name: '登记新版本' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '发布' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '补充更新说明' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '删除' })).toHaveCount(0)
  await expect(page.getByText('1.4.0')).toBeVisible()
  expect(writes).toEqual([])
})

test('opens the release registration flow with storage.manage', async ({ page }) => {
  await useAdminSessionWithPermissions(page, ['storage.read', 'storage.manage'])
  await mockReleaseList(page)

  await page.goto('/releases')

  await expect(page.getByText(/你当前只有查看权限/)).toHaveCount(0)
  await page.locator('header').getByRole('button', { name: '登记新版本' }).click()
  await expect(
    page.getByRole('dialog', { name: '登记新版本' }),
  ).toBeVisible()
})

test('keeps all OTA mutations hidden with only ota.read', async ({ page }) => {
  await useAdminSessionWithPermissions(page, ['ota.read', 'storage.manage'])
  const writes: string[] = []
  await mockOtaSurface(page, (method, path) => {
    writes.push(`${method} ${path}`)
  })

  await page.goto('/device-ota')

  await expect(page.getByText(/你当前只有查看权限。可以查看固件版本/)).toBeVisible()
  await expect(page.getByRole('button', { name: '新建固件版本' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '编辑' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '发布' })).toHaveCount(0)
  await expect(
    page.getByRole('button', { name: '详情', exact: true }),
  ).toBeVisible()
  expect(writes).toEqual([])
})

test('allows OTA records without storage permissions but keeps file selection read-gated', async ({
  page,
}) => {
  await useAdminSessionWithPermissions(page, ['ota.read', 'ota.manage'])
  let storageRequests = 0
  await mockOtaSurface(page, () => undefined)
  await page.route('**/api/v1/admin/storage/files**', async (route) => {
    storageRequests += 1
    await route.fulfill({
      status: 500,
      contentType: 'application/json',
      body: JSON.stringify({ error: { message: '文件请求不应发生' } }),
    })
  })

  await page.goto('/device-ota')
  await page.getByRole('button', { name: '新建固件版本' }).click()

  const dialog = page.getByRole('dialog', { name: '新建固件版本' })
  await expect(dialog).toBeVisible()
  await expect(
    dialog.getByRole('button', { name: '从下载服务器选择固件' }),
  ).toHaveCount(0)
  await expect(dialog.getByRole('button', { name: '上传新固件' })).toHaveCount(0)
  await expect(dialog.getByText(/当前没有下载服务器权限/)).toBeVisible()
  expect(storageRequests).toBe(0)
})

test('opens the firmware file picker when ota.manage and storage.read are present', async ({
  page,
}) => {
  await useAdminSessionWithPermissions(page, ['ota.read', 'ota.manage', 'storage.read'])
  let storageRequests = 0
  await mockOtaSurface(page, () => undefined)
  await page.route('**/api/v1/admin/storage/files**', async (route) => {
    storageRequests += 1
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          files: [
            {
              id: 'firmware-file',
              file_name: 'sprout-firmware-1.3.0.bin',
              relative_path: '1.3.0/stable/esp32_s3/firmware/sprout-firmware-1.3.0.bin',
              directory: '1.3.0/stable/esp32_s3/firmware',
              size_bytes: 4096,
              modified_at: '2026-10-18T09:00:00Z',
              sha256: 'c'.repeat(64),
              download_url:
                'https://download.example.test/1.3.0/stable/esp32_s3/firmware/sprout-firmware-1.3.0.bin',
              release: {
                version: '1.3.0',
                channel: 'stable',
                kind: 'firmware',
                platform: 'esp32_s3',
              },
              is_indexed: true,
            },
          ],
        },
      }),
    })
  })

  await page.goto('/device-ota')
  await page.getByRole('button', { name: '新建固件版本' }).click()
  const formDialog = page.getByRole('dialog', { name: '新建固件版本' })
  await formDialog.getByRole('button', { name: '从下载服务器选择固件' }).click()

  const fileDialog = page.getByRole('dialog', { name: '选择固件文件' })
  await expect(fileDialog).toBeVisible()
  await expect(
    fileDialog.getByText('sprout-firmware-1.3.0.bin', { exact: true }),
  ).toBeVisible()
  expect(storageRequests).toBeGreaterThan(0)
})
