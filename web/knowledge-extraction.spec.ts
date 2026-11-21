import { expect, test } from '@playwright/test'
import type { KnowledgeDocument, KnowledgeExtractionMetadata } from './src/lib/api/models/knowledge'
import {
  coverageText,
  extractionStatus,
  extractionWarning,
} from './src/components/block/knowledge/extraction'

const metadata: KnowledgeExtractionMetadata = {
  status: 'limited',
  extractor_version: 'test-v1',
  warnings: [
    'sheet_limit',
    'row_limit',
    'column_limit',
    'cell_truncated',
    'header_truncated',
    'hidden_sheets',
    'images_or_objects',
    'unsupported_parts',
    'unsupported_content',
    'heading_truncated',
    'front_matter_omitted',
    'text_normalized',
    '<script>unknown_warning</script>',
  ],
  coverage: Array.from({ length: 22 }, (_, i) => ({
    unit: 'baris',
    scope: `Lembar ${i}`,
    processed: 5,
    total: i === 0 ? 10 : null,
  })),
}
const document = (
  id: string,
  extraction: KnowledgeExtractionMetadata | null,
): KnowledgeDocument => ({
  id,
  title: id,
  tenant_id: null,
  category_id: null,
  category_name: null,
  filename: `${id}.md`,
  format: 'md',
  size_bytes: 20,
  char_count: 60,
  chunk_count: 11,
  uploaded_by: null,
  created_at: '2026-11-01T08:00:00Z',
  updated_at: '2026-11-01T08:00:00Z',
  extraction_metadata: extraction,
  status: 'published',
})

test('status, warning fallback, and percentage guards', () => {
  expect(extractionStatus(null)).toBe('Metadata ekstraksi belum tersedia')
  expect(extractionStatus({ ...metadata, status: 'complete' })).toBe('Siap digunakan')
  expect(extractionStatus(metadata)).toBe('Siap · Ekstraksi terbatas')
  for (const code of metadata.warnings.slice(0, -1))
    expect(extractionWarning(code)).not.toContain('Catatan ekstraksi:')
  expect(extractionWarning('toString')).toBe('Catatan ekstraksi: toString')
  for (const total of [null, 0, 3, -1, Infinity])
    expect(coverageText({ unit: 'x', scope: 'x', processed: 5, total })).not.toContain('%')
  expect(coverageText({ unit: 'x', scope: 'x', processed: 5, total: 10 })).toContain('50%')
})

test('reader details, legacy metadata, escaped preview, pagination, retry, and scope isolation', async ({
  page,
  context,
}) => {
  let failed = false
  let delayed = false
  const previewRequests: string[] = []
  await context.addCookies([
    { name: 'hsgAccessToken', value: 'test', url: 'http://127.0.0.1:3199' },
  ])
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    if (url.pathname.endsWith('/me'))
      return route.fulfill({
        json: {
          data: { id: 'reader', name: 'Pembaca', email: 'reader@example.test', role: 'user' },
        },
      })
    if (url.pathname === '/api/v1/tenants')
      return route.fulfill({
        json: {
          data: [
            { id: 'tenant-a', name: 'Tenant A', role: 'reviewer' },
            { id: 'tenant-b', name: 'Tenant B', role: 'reviewer' },
          ],
        },
      })
    if (url.pathname.includes('/knowledge')) {
      if (url.pathname.endsWith('/preview')) {
        previewRequests.push(url.pathname + url.search)
        if (delayed) await new Promise((resolve) => setTimeout(resolve, 700))
        if (failed) return route.fulfill({ status: 500, json: { message: 'Test failure' } })
        const offset = Number(url.searchParams.get('offset'))
        const id = url.pathname.split('/').at(-2)!
        return route.fulfill({
          json: {
            data: {
              document: document(id, id === 'legacy' ? null : metadata),
              chunks: [
                {
                  position: offset,
                  heading_path: 'Bagian <b>judul</b>',
                  locator: 'Lembar A, baris 1',
                  content: offset
                    ? 'Halaman kedua'
                    : `${url.pathname}\n<script>window.previewInjected=true</script>\n**teks biasa**`,
                },
              ],
            },
            metadata: { offset, limit: 10, total: 11 },
          },
        })
      }
      return route.fulfill({
        json: {
          data: [
            document('limited', metadata),
            document('legacy', null),
            document('complete', { ...metadata, status: 'complete' }),
          ],
          metadata: { total: 3, offset: 0, limit: 10 },
        },
      })
    }
    return route.fulfill({ json: { data: [], metadata: { total: 0 } } })
  })
  await page.goto('/knowledge')
  await page.getByRole('combobox', { name: 'Ganti tenant' }).click()
  await page.getByRole('option', { name: 'Tenant A', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Detail ekstraksi limited' })).toBeVisible()
  await expect(page.getByRole('button', { name: /Hapus/ })).toHaveCount(0)
  await expect(page.getByText('Siap digunakan', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Detail ekstraksi limited' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText('Menampilkan 20 dari 22 rincian cakupan.')).toBeVisible()
  await expect(dialog.getByText('Lembar 20 · baris', { exact: true })).toHaveCount(0)
  await expect(dialog.getByText('5 dari 10 diproses (50%)')).toBeVisible()
  await expect(
    dialog.getByText('Catatan ekstraksi: <script>unknown warning</script>'),
  ).toBeVisible()
  await expect(dialog.getByText('Bagian <b>judul</b>', { exact: false })).toBeVisible()
  await expect(dialog.getByText('Lokasi: Lembar A, baris 1')).toBeVisible()
  await expect(dialog.locator('article p').last()).toContainText(
    '<script>window.previewInjected=true</script>',
  )
  expect(await page.evaluate(() => Reflect.get(window, 'previewInjected'))).toBeUndefined()
  await expect(dialog.locator('article script, article strong, article b')).toHaveCount(0)
  await dialog.getByRole('button', { name: 'Berikutnya' }).click()
  await expect(dialog.getByText('Halaman kedua')).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Berikutnya' })).toBeDisabled()
  await dialog.getByRole('button', { name: 'Sebelumnya' }).click()
  await expect(dialog.getByText('1–10 dari 11 potongan')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  failed = true
  await page.getByRole('button', { name: 'Detail ekstraksi legacy' }).click()
  await expect(dialog.getByRole('alert')).toContainText('Pratinjau gagal dimuat', {
    timeout: 15000,
  })
  await expect(dialog.getByText('Metadata ekstraksi belum tersedia')).toBeVisible()
  failed = false
  await dialog.getByRole('button', { name: 'Coba lagi' }).click()
  await expect(dialog.locator('article')).toHaveCount(1)
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  delayed = true
  await page.getByRole('button', { name: 'Detail ekstraksi complete' }).click()
  await expect(dialog.getByRole('status')).toHaveText('Memuat pratinjau…')
  await expect(dialog.locator('article')).toHaveCount(0)
  await expect(dialog.locator('article')).toHaveCount(1)
  await page.evaluate(() => {
    localStorage.setItem('hsgActiveTenant', 'tenant-b')
    window.dispatchEvent(new Event('hsg:active-tenant-changed'))
  })
  await expect(dialog).toHaveCount(0)
  await page.getByRole('button', { name: 'Detail ekstraksi limited' }).click()
  await expect(dialog.locator('article')).toHaveCount(0)
  await expect(dialog.locator('article')).toContainText(
    '/tenants/tenant-b/knowledge/limited/preview',
  )
  expect(previewRequests.some((url) => url.includes('offset=10') && url.includes('limit=10'))).toBe(
    true,
  )
})
