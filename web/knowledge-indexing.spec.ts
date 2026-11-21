import { expect, test, type Page } from '@playwright/test'
import type { KnowledgeIndexing } from './src/lib/api/models/knowledge'

const revision = 'a'.repeat(40)
const doc = {
  id: 'dokumen',
  title: 'Pedoman',
  filename: 'pedoman.md',
  format: 'md',
  tenant_id: null,
  size_bytes: 12,
  char_count: 12,
  chunk_count: 4,
  uploaded_by: null,
  created_at: '2026-11-01T08:00:00Z',
  updated_at: '2026-11-01T08:00:00Z',
  extraction_metadata: null,
}
const base: KnowledgeIndexing = {
  status: 'ready',
  indexed_chunks: 3,
  eligible_chunks: 4,
  profile_revision: revision,
  updated_at: doc.updated_at,
}
async function fixture(
  page: Page,
  options: {
    manager?: boolean
    platform?: boolean
    indexing?: Partial<KnowledgeIndexing>
    storedTenant?: string
    noMemberships?: boolean
    embedding?: { model?: string; dimensions?: number; has_credential?: boolean }
  } = {},
) {
  let status = { ...base, ...options.indexing }
  let failures = 0
  let posts = 0
  const requests: string[] = []
  const knowledgeRequests: { path: string; tenant: string | undefined; stored: string | null }[] =
    []
  const saves: Record<string, unknown>[] = []
  await page
    .context()
    .addCookies([{ name: 'hsgAccessToken', value: 'fixture', url: 'http://127.0.0.1:3199' }])
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path.includes('/knowledge')) {
      knowledgeRequests.push({
        path,
        tenant: route.request().headers()['x-tenant-id'],
        stored: await page.evaluate(() => localStorage.getItem('hsgActiveTenant')),
      })
    }
    if (path.endsWith('/me'))
      return route.fulfill({
        json: {
          data: {
            id: 'person',
            name: 'Pengguna',
            email: 'user@example.test',
            role: options.platform ? 'super_user' : 'user',
          },
        },
      })
    if (path === '/api/v1/tenants')
      return route.fulfill({
        json: {
          data: (options.noMemberships ? [] : ['a', 'b']).map((id) => ({
            id: `tenant-${id}`,
            name: `Tenant ${id.toUpperCase()}`,
            role: options.manager ? 'tenant_admin' : 'reviewer',
          })),
        },
      })
    if (path.endsWith('/embedding-config')) {
      if (route.request().method() === 'PUT') {
        saves.push(route.request().postDataJSON())
        return route.fulfill({ json: { data: {} } })
      }
      return route.fulfill({
        json: {
          data: {
            endpoint: 'https://embedding.example.test/embed',
            model: 'BAAI/bge-m3',
            dimensions: 1024,
            revision,
            enabled: false,
            has_credential: true,
            updated_at: doc.updated_at,
            ...options.embedding,
          },
        },
      })
    }
    if (path.endsWith('/indexing')) {
      requests.push(path)
      return route.fulfill({ json: { data: status } })
    }
    if (path.endsWith('/reindex')) {
      posts++
      if (failures-- > 0) return route.fulfill({ status: 500, json: { message: 'Fixture error' } })
      status = { ...status, status: 'pending' }
      return route.fulfill({ json: { data: {} } })
    }
    if (path.endsWith('/preview'))
      return route.fulfill({
        json: {
          data: {
            document: doc,
            chunks: [{ position: 0, heading_path: '', locator: '', content: 'Isi dokumen' }],
          },
          metadata: { total: 1, offset: 0, limit: 10 },
        },
      })
    return route.fulfill({
      json: {
        data: path.includes('/knowledge')
          ? [
              {
                ...doc,
                tenant_id: options.platform
                  ? null
                  : path.includes('/tenant-b/')
                    ? 'tenant-b'
                    : 'tenant-a',
              },
            ]
          : [],
        metadata: { total: 1, offset: 0, limit: 10 },
      },
    })
  })
  await page.addInitScript((tenant) => {
    if (tenant) localStorage.setItem('hsgActiveTenant', tenant)
    else localStorage.removeItem('hsgActiveTenant')
  }, options.storedTenant ?? 'tenant-a')
  await page.goto('/knowledge')
  return {
    requests,
    knowledgeRequests,
    saves,
    fail: () => {
      failures = 1
    },
    posts: () => posts,
  }
}
test('stale tenant requires an explicit choice before knowledge requests', async ({ page }) => {
  const state = await fixture(page, { storedTenant: 'revoked-tenant' })
  const switcher = page.getByRole('combobox', { name: 'Ganti tenant' })
  await expect(switcher).toContainText('Pilih tenant')
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem('hsgActiveTenant')))
    .toBeNull()
  expect(state.knowledgeRequests).toHaveLength(0)
  await switcher.click()
  await page.getByRole('option', { name: 'Tenant A', exact: true }).click()
  await detail(page)
  expect(state.knowledgeRequests.length).toBeGreaterThan(0)
  expect(
    state.knowledgeRequests.every(
      (request) => request.tenant === 'tenant-a' && request.stored === 'tenant-a',
    ),
  ).toBe(true)
  await page.evaluate(async () => {
    const modulePath = '/src/lib/auth/active-tenant.ts'
    const { setActiveTenantId } = await import(modulePath)
    setActiveTenantId('tenant-b')
  })
  await expect
    .poll(() => state.knowledgeRequests.some((request) => request.path.includes('/tenant-b/')))
    .toBe(true)
  await expect(switcher).toContainText('Tenant B')
  expect(
    state.knowledgeRequests
      .filter((request) => request.path.includes('/tenant-b/'))
      .every((request) => request.tenant === 'tenant-b' && request.stored === 'tenant-b'),
  ).toBe(true)
})

test('platform knowledge omits stale tenant header', async ({ page }) => {
  const state = await fixture(page, { platform: true, storedTenant: 'revoked-tenant' })
  await detail(page)
  expect(state.knowledgeRequests.length).toBeGreaterThan(0)
  expect(
    state.knowledgeRequests.every(
      (request) => request.tenant === undefined && request.stored === null,
    ),
  ).toBe(true)
})

test('no memberships clears stale tenant without knowledge requests', async ({ page }) => {
  const state = await fixture(page, { noMemberships: true, storedTenant: 'revoked-tenant' })
  await expect(page.getByText('Belum ada tenant', { exact: true })).toBeVisible()
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem('hsgActiveTenant')))
    .toBeNull()
  expect(state.knowledgeRequests).toHaveLength(0)
})

test('embedding validation links errors and focuses the first invalid field', async ({ page }) => {
  const state = await fixture(page, { platform: true })
  await page.getByRole('button', { name: 'Konfigurasi embedding', exact: true }).click()
  const drawer = page.getByRole('dialog')
  const endpoint = drawer.getByLabel('Endpoint embedding')
  const revisionInput = drawer.getByLabel('Revisi model')
  const credential = drawer.getByLabel('Credential baru')
  const submit = drawer.getByRole('button', { name: 'Simpan konfigurasi' })
  await endpoint.fill('http://invalid.test/embed')
  await revisionInput.fill('main')
  await submit.click()
  await expect(endpoint).toBeFocused()
  await expect(endpoint).toHaveAttribute('aria-invalid', 'true')
  await expect(endpoint).toHaveAccessibleDescription(/HTTPS tanpa credential URL/)
  await endpoint.fill('https://new.example.test/embed')
  await submit.click()
  await expect(revisionInput).toBeFocused()
  await expect(revisionInput).toHaveAttribute('aria-invalid', 'true')
  await expect(revisionInput).toHaveAccessibleDescription(/Revisi harus berupa commit immutable/)
  await expect(endpoint).toHaveAttribute('aria-invalid', 'false')
  await revisionInput.fill(revision)
  await submit.click()
  await expect(credential).toBeFocused()
  await expect(credential).toHaveAttribute('aria-invalid', 'true')
  await expect(credential).toHaveAccessibleDescription(
    /Perubahan endpoint memerlukan credential baru/,
  )
  expect(state.saves).toHaveLength(0)
  await credential.fill('fixture-secret')
  await submit.click()
  await expect(drawer).toHaveCount(0)
  expect(state.saves).toHaveLength(1)
})

test('clear auth removes stale tenant, including blocked storage', async ({ page }) => {
  await fixture(page)
  const result = await page.evaluate(async () => {
    const tenantPath = '/src/lib/auth/active-tenant.ts'
    const authPath = '/src/lib/auth/token-storage.ts'
    const tenant = await import(tenantPath)
    const auth = await import(authPath)
    tenant.setActiveTenantId('tenant-b')
    auth.clearAuthTokens()
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)))
    const cleared = tenant.getActiveTenantId()
    tenant.setActiveTenantId('tenant-b')
    const removeItem = Storage.prototype.removeItem
    Storage.prototype.removeItem = () => {
      throw new Error('blocked')
    }
    auth.clearAuthTokens()
    const blocked = tenant.getActiveTenantId()
    Storage.prototype.removeItem = removeItem
    return { cleared, blocked }
  })
  expect(result).toEqual({ cleared: null, blocked: null })
})

test('knowledge header follows explicit request scope rather than active storage', async ({
  page,
}) => {
  const state = await fixture(page)
  await detail(page)
  await page.evaluate(async () => {
    const clientPath = '/src/lib/api/client-fetch.ts'
    const tenantPath = '/src/lib/auth/active-tenant.ts'
    const { api } = await import(clientPath)
    const { setActiveTenantId } = await import(tenantPath)
    setActiveTenantId('tenant-b')
    await api.get('/api/v1/tenants/tenant-a/knowledge/doc-1/indexing')
    await api.get('/api/v1/platform/knowledge/embedding-config')
  })
  expect(
    state.knowledgeRequests.some(
      (request) =>
        request.path.includes('/tenant-a/') &&
        request.stored === 'tenant-b' &&
        request.tenant === 'tenant-a',
    ),
  ).toBe(true)
  expect(
    state.knowledgeRequests.some(
      (request) =>
        request.path.includes('/platform/') &&
        request.stored === 'tenant-b' &&
        request.tenant === undefined,
    ),
  ).toBe(true)
})

async function detail(page: Page) {
  await page.getByRole('button', { name: 'Detail ekstraksi Pedoman' }).last().click()
  return page.getByRole('region', { name: 'Indeks semantik', exact: true })
}

for (const [status, label] of Object.entries({
  not_configured: 'Belum dikonfigurasi',
  pending: 'Menunggu indeks',
  indexing: 'Mengindeks',
  ready: 'Indeks siap',
  partial: 'Indeks sebagian',
  failed: 'Indeks gagal',
})) {
  test(`reader status ${status}`, async ({ page }) => {
    const state = await fixture(page, {
      indexing: { status: status as KnowledgeIndexing['status'] },
    })
    expect(state.requests).toHaveLength(0)
    const region = await detail(page)
    await expect(region.getByText(label, { exact: true })).toBeVisible()
    await expect(region.getByText('3 dari 4 potongan layak terindeks (75%)')).toBeVisible()
    await expect(region.getByRole('button', { name: /Indeks ulang|Coba indeks/ })).toHaveCount(0)
    await expect(
      page.getByRole('button', { name: 'Konfigurasi embedding', exact: true }),
    ).toHaveCount(0)
  })
}
for (const [indexed_chunks, eligible_chunks] of [
  [0, 0],
  [5, 4],
  [-1, 4],
  [1, -1],
  [1.5, 4],
  [1, Number.MAX_SAFE_INTEGER + 1],
]) {
  test(`invalid counters ${indexed_chunks}/${eligible_chunks}`, async ({ page }) => {
    await fixture(page, { indexing: { indexed_chunks, eligible_chunks } })
    const region = await detail(page)
    await expect(
      region.getByText('Persentase indeks tidak tersedia; penghitung potongan belum valid.'),
    ).toBeVisible()
    await expect(region).not.toContainText('%')
  })
}
test('manager retries failed reindex and cannot submit pending twice', async ({ page }) => {
  const state = await fixture(page, { manager: true, indexing: { status: 'failed' } })
  state.fail()
  const region = await detail(page)
  await region.getByRole('button', { name: 'Coba indeks ulang' }).click()
  await expect(region.getByRole('alert')).toContainText('Gagal mengajukan')
  await region.getByRole('button', { name: 'Coba indeks ulang' }).click()
  await expect(region.getByText('Menunggu indeks', { exact: true })).toBeVisible()
  await expect(region.getByRole('button', { name: 'Indeks ulang', exact: true })).toBeDisabled()
  expect(state.posts()).toBe(2)
})
test('scope switch closes detail and stops old polling', async ({ page }) => {
  const state = await fixture(page, { indexing: { status: 'indexing' } })
  await detail(page)
  await expect.poll(() => state.requests.length).toBe(1)
  await page.clock.install()
  await page.evaluate(() => {
    localStorage.setItem('hsgActiveTenant', 'tenant-b')
    window.dispatchEvent(new Event('hsg:active-tenant-changed'))
  })
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await page.clock.fastForward(20000)
  expect(state.requests).toHaveLength(1)
  await detail(page)
  await expect.poll(() => state.requests.length).toBe(2)
  expect(state.requests[1]).toContain('/tenant-b/')
})

test('pending manager disabled and polling bounded', async ({ page }) => {
  await page.clock.install()
  const state = await fixture(page, { manager: true, indexing: { status: 'pending' } })
  const region = await detail(page)
  await expect(region.getByRole('button', { name: 'Indeks ulang', exact: true })).toBeDisabled()
  await expect.poll(() => state.requests.length).toBe(1)
  for (let i = 1; i < 30; i++) {
    const response = page.waitForResponse((response) =>
      new URL(response.url()).pathname.endsWith('/indexing'),
    )
    await page.clock.fastForward(5100)
    await (await response).finished()
    await page.clock.fastForward(1)
    await expect.poll(() => state.requests.length).toBe(i + 1)
  }
  await page.clock.fastForward(60000)
  expect(state.requests).toHaveLength(30)
})

test('drawer layout on narrow screens and dark theme', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await fixture(page, { platform: true })
  await page.getByRole('button', { name: 'Konfigurasi embedding', exact: true }).click()
  await expect(page.getByLabel('Credential baru')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Simpan konfigurasi' })).toBeVisible()
  await page.screenshot({ path: '/tmp/hsg-embedding-config-mobile.png' })
  await page.getByRole('button', { name: 'Batal', exact: true }).click()
  await page.setViewportSize({ width: 1280, height: 900 })
  await page.evaluate(() => document.documentElement.classList.add('dark'))
  await detail(page)
  await expect(page.getByText('Indeks siap', { exact: true })).toBeVisible()
  await page.screenshot({ path: '/tmp/hsg-indexing-detail-dark.png' })
})

test('config validates immutable revision and endpoint credential replacement', async ({
  page,
}) => {
  const state = await fixture(page, { platform: true })
  await page.getByRole('button', { name: 'Konfigurasi embedding', exact: true }).click()
  const drawer = page.getByRole('dialog')
  await expect(drawer.getByLabel('Model (tetap)')).toHaveAttribute('readonly', '')
  await expect(drawer.getByLabel('Dimensi (tetap)')).toHaveValue('1024')
  await expect(drawer.getByLabel('Credential baru')).toHaveValue('')
  await expect(drawer.getByRole('switch', { name: 'Aktifkan embedding' })).not.toBeChecked()
  await drawer.getByLabel('Revisi model').fill('main')
  await drawer.getByRole('button', { name: 'Simpan konfigurasi' }).click()
  await expect(drawer.getByRole('alert')).toContainText('40 karakter')
  await drawer.getByLabel('Revisi model').fill(revision)
  await drawer.getByLabel('Endpoint embedding').fill('http://embedding.example.test')
  await drawer.getByRole('button', { name: 'Simpan konfigurasi' }).click()
  await expect(drawer.getByRole('alert')).toContainText('HTTPS')
  await drawer.getByLabel('Endpoint embedding').fill('https://new.example.test/embed')
  await drawer.getByRole('button', { name: 'Simpan konfigurasi' }).click()
  await expect(drawer.getByRole('alert')).toContainText('credential baru')
  expect(state.saves).toHaveLength(0)
  await drawer.getByLabel('Credential baru').fill('fixture-secret')
  await drawer.getByRole('button', { name: 'Simpan konfigurasi' }).click()
  await expect(drawer).toHaveCount(0)
  expect(state.saves[0]).toEqual({
    endpoint: 'https://new.example.test/embed',
    revision,
    enabled: false,
    clear_credential: false,
    credential: 'fixture-secret',
  })
})
test('model and dimensions display the initial config without submitting overrides', async ({
  page,
}) => {
  const state = await fixture(page, {
    platform: true,
    embedding: { model: 'fixture-server-model', dimensions: 768 },
  })
  await page.getByRole('button', { name: 'Konfigurasi embedding', exact: true }).click()
  const drawer = page.getByRole('dialog')
  await expect(drawer.getByLabel('Model (tetap)')).toHaveValue('fixture-server-model')
  await expect(drawer.getByLabel('Dimensi (tetap)')).toHaveValue('768')
  await drawer.getByRole('button', { name: 'Simpan konfigurasi' }).click()
  await expect(drawer).toHaveCount(0)
  expect(state.saves[0]).not.toHaveProperty('model')
  expect(state.saves[0]).not.toHaveProperty('dimensions')
  expect(state.saves[0].enabled).toBe(false)
})

test('enabling without a credential focuses its error and never submits', async ({ page }) => {
  const state = await fixture(page, { platform: true, embedding: { has_credential: false } })
  await page.getByRole('button', { name: 'Konfigurasi embedding', exact: true }).click()
  const drawer = page.getByRole('dialog')
  await drawer.getByRole('switch', { name: 'Aktifkan embedding' }).click()
  await drawer.getByRole('button', { name: 'Simpan konfigurasi' }).click()
  const credential = drawer.getByLabel('Credential baru')
  await expect(credential).toBeFocused()
  await expect(credential).toHaveAttribute('aria-invalid', 'true')
  await expect(credential).toHaveAccessibleDescription(/Isi credential sebelum mengaktifkan/)
  expect(state.saves).toHaveLength(0)
})

test('active indexing stops after 30 actual requests', async ({ page }) => {
  await page.clock.install()
  const state = await fixture(page, { indexing: { status: 'indexing' } })
  await detail(page)
  await expect.poll(() => state.requests.length).toBe(1)
  for (let count = 2; count <= 30; count++) {
    await page.clock.fastForward(5100)
    await expect.poll(() => state.requests.length).toBe(count)
  }
  await page.clock.fastForward(30_000)
  expect(state.requests).toHaveLength(30)
  await page.evaluate(async () => {
    const modulePath = '/src/lib/auth/active-tenant.ts'
    const { setActiveTenantId } = await import(modulePath)
    setActiveTenantId('tenant-b')
  })
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await detail(page)
  await expect.poll(() => state.requests.length).toBe(31)
  await page.clock.fastForward(5100)
  await expect.poll(() => state.requests.length).toBe(32)
  expect(state.requests.slice(30).every((path) => path.includes('/tenant-b/'))).toBe(true)
})

test('indexing failure stops polling without automatic retries', async ({ page }) => {
  await page.clock.install()
  await fixture(page)
  let attempts = 0
  await page.route('**/indexing', (route) => {
    attempts++
    return route.fulfill({ status: 500, json: { message: 'Fixture failure' } })
  })
  const region = await detail(page)
  await expect(region.getByText('Status indeks gagal dimuat.')).toBeVisible()
  await page.clock.fastForward(30_000)
  expect(attempts).toBe(1)
})

test('empty credential preserves it; explicit clear disables embedding', async ({ page }) => {
  const state = await fixture(page, { platform: true })
  await page.getByRole('button', { name: 'Konfigurasi embedding', exact: true }).click()
  await page.getByRole('button', { name: 'Simpan konfigurasi' }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(state.saves[0]).not.toHaveProperty('credential')
  expect(state.saves[0].clear_credential).toBe(false)
  await page.getByRole('button', { name: 'Konfigurasi embedding', exact: true }).click()
  await page.getByRole('switch', { name: 'Aktifkan embedding' }).click()
  await page.getByRole('switch', { name: 'Hapus credential tersimpan' }).click()
  await expect(page.getByRole('switch', { name: 'Aktifkan embedding' })).not.toBeChecked()
  await expect(page.getByLabel('Credential baru')).toBeDisabled()
  await page.getByLabel('Endpoint embedding').fill('https://new.example.test/embed')
  await page.getByRole('button', { name: 'Simpan konfigurasi' }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(state.saves[1]).toEqual({
    endpoint: 'https://new.example.test/embed',
    revision,
    enabled: false,
    clear_credential: true,
  })
})
