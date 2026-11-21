import { useMutation, useQuery } from '@tanstack/react-query'
import { useEffect, useState, type FormEvent } from 'react'
import { toast } from 'sonner'

import SectionCard from '@/components/block/common/section-card'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import {
  ExaConfigFormSchema,
  parseExaKeys,
  type ExaConfigFormDto,
} from '@/lib/api/dtos/platform/exa-config/schema'
import type { ExaConfig } from '@/lib/api/models/exa-config'
import { exaConfigQueries } from '@/lib/api/queries/exa-config'

export default function ExaConfigContent() {
  const config = useQuery(exaConfigQueries.get())
  if (config.isPending) return <p role="status">Memuat konfigurasi Exa…</p>
  if (config.isError && !config.data?.data)
    return (
      <div role="alert">
        <p>Konfigurasi Exa gagal dimuat.</p>
        <Button variant="outline" onClick={() => config.refetch()}>
          Coba lagi
        </Button>
      </div>
    )
  if (!config.data?.data) return <p role="status">Konfigurasi Exa belum tersedia.</p>
  return (
    <>
      {config.isError && (
        <div role="alert">
          <p>Konfigurasi terbaru gagal dimuat. Perubahan Anda tetap dipertahankan.</p>
          <Button
            variant="outline"
            disabled={config.isFetching}
            onClick={() => void config.refetch()}
          >
            Coba lagi
          </Button>
        </div>
      )}
      <ExaConfigForm initial={config.data.data} />
    </>
  )
}

function ExaConfigForm({ initial }: { initial: ExaConfig }) {
  const [enabled, setEnabled] = useState(initial.enabled)
  const [keys, setKeys] = useState('')
  const [clearKeys, setClearKeys] = useState(false)
  const [baseline, setBaseline] = useState(initial)
  const [validationError, setValidationError] = useState('')
  const [invalidFields, setInvalidFields] = useState<string[]>([])
  const save = useMutation(exaConfigQueries.update())
  const test = useMutation(exaConfigQueries.test())
  const dirty = enabled !== baseline.enabled || Boolean(keys.trim()) || clearKeys
  const changedOnServer = JSON.stringify(initial) !== JSON.stringify(baseline)

  function adopt(config: ExaConfig) {
    setBaseline(config)
    setEnabled(config.enabled)
    setKeys('')
    setClearKeys(false)
    setValidationError('')
    setInvalidFields([])
    test.reset()
  }

  useEffect(() => {
    if (changedOnServer && !dirty && !save.isPending && !test.isPending) adopt(initial)
  })

  function fieldChanged() {
    test.reset()
    setValidationError('')
    setInvalidFields([])
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setValidationError('')
    const validation = ExaConfigFormSchema.safeParse({
      enabled,
      keys,
      clear_keys: clearKeys,
    })
    if (!validation.success) {
      const issues = validation.error.issues
      setInvalidFields(issues.map((issue) => String(issue.path[0])))
      setValidationError(issues.map((issue) => issue.message).join('\n'))
      return
    }
    // Omit `keys` entirely when they are being preserved: an empty array would
    // instruct the server to drop every stored key.
    const added = parseExaKeys(keys)
    const body: ExaConfigFormDto = {
      enabled,
      clear_keys: clearKeys,
      ...(added.length > 0 ? { keys: added } : {}),
    }
    try {
      await save.mutateAsync(body)
      setKeys('')
      // PUT has no read model. Track the outcome locally; GET refreshes it.
      setBaseline({
        ...baseline,
        enabled,
        key_count: added.length > 0 ? added.length : clearKeys ? 0 : baseline.key_count,
        has_keys: added.length > 0 ? true : clearKeys ? false : baseline.has_keys,
      })
      setClearKeys(false)
      toast.success('Konfigurasi Exa berhasil disimpan.')
    } catch {
      setValidationError(
        'Konfigurasi Exa gagal disimpan. Periksa format kunci dan coba lagi. Masukkan kembali kunci jika menggantinya.',
      )
    } finally {
      save.reset()
    }
  }

  const busy = save.isPending || test.isPending
  const usingKeys = baseline.has_keys && !clearKeys

  return (
    <SectionCard
      title="Exa Web Access"
      description="Pencarian web untuk asisten. Akses tanpa kunci dicoba lebih dulu, lalu kunci cadangan dipakai bergiliran."
    >
      <form onSubmit={submit} onChange={fieldChanged} noValidate className="flex flex-col gap-5">
        {changedOnServer && dirty && (
          <div role="status" className="space-y-2 text-[13px]">
            <p>
              Konfigurasi di server berubah. Muat konfigurasi terbaru untuk membuang perubahan Anda.
            </p>
            <Button type="button" variant="outline" disabled={busy} onClick={() => adopt(initial)}>
              Muat konfigurasi terbaru
            </Button>
          </div>
        )}
        <fieldset disabled={busy} className="flex flex-col gap-5 disabled:opacity-60">
          <div className="flex items-center gap-3">
            <Switch
              id="exa-enabled"
              checked={enabled}
              onCheckedChange={(value) => {
                fieldChanged()
                setEnabled(value)
              }}
            />
            <Label htmlFor="exa-enabled">Aktifkan Exa Web Access</Label>
          </div>
          <p className="text-ink-2 text-[13px]">
            Tanpa kunci, asisten memakai akses anonim Exa. Tambahkan kunci untuk kuota yang lebih
            tinggi; kunci dirotasi otomatis saat ditolak atau terkena batas.
          </p>
          <div className="space-y-2">
            <Label htmlFor="exa-keys">Kunci API Exa</Label>
            <Textarea
              id="exa-keys"
              value={keys}
              onChange={(e) => setKeys(e.target.value)}
              disabled={clearKeys}
              rows={5}
              spellCheck={false}
              autoComplete="off"
              placeholder={'Satu kunci per baris\ncontoh-kunci-1\ncontoh-kunci-2'}
              aria-invalid={invalidFields.includes('keys')}
              aria-describedby={
                invalidFields.includes('keys') ? 'exa-keys-help exa-error' : 'exa-keys-help'
              }
              className="font-mono text-[13px]"
            />
            <p id="exa-keys-help" className="text-ink-2 text-[13px]">
              {usingKeys
                ? `${baseline.key_count} kunci tersimpan. Kosongkan untuk mempertahankannya, atau isi untuk mengganti seluruh daftar.`
                : 'Belum ada kunci tersimpan. Kosongkan untuk tetap memakai akses tanpa kunci.'}
            </p>
            {baseline.has_keys && (
              <div className="flex items-center gap-3">
                <Checkbox
                  id="exa-clear-keys"
                  checked={clearKeys}
                  aria-describedby="exa-clear-help"
                  onCheckedChange={(value) => {
                    fieldChanged()
                    setClearKeys(value === true)
                    if (value === true) setKeys('')
                  }}
                />
                <Label htmlFor="exa-clear-keys">Hapus kunci tersimpan</Label>
              </div>
            )}
            {baseline.has_keys && (
              <p id="exa-clear-help" className="text-ink-2 text-[13px]">
                Penghapusan kembali ke akses tanpa kunci dan berlaku setelah konfigurasi disimpan.
              </p>
            )}
          </div>
        </fieldset>
        {validationError && (
          <p id="exa-error" role="alert" className="text-red whitespace-pre-line text-[13px]">
            {validationError}
          </p>
        )}
        <div className="flex flex-wrap gap-3">
          <Button type="submit" disabled={busy}>
            {save.isPending ? 'Menyimpan…' : 'Simpan konfigurasi'}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={busy || dirty}
            onClick={() => test.mutate()}
          >
            {test.isPending ? 'Menguji…' : 'Uji pencarian tersimpan'}
          </Button>
        </div>
        {dirty && (
          <p className="text-ink-2 text-[13px]">Simpan perubahan sebelum menguji pencarian.</p>
        )}
        {test.isError && (
          <p role="alert" className="text-red text-[13px]">
            Uji pencarian gagal. Periksa konfigurasi dan coba lagi.
          </p>
        )}
        {test.isSuccess && !dirty && (
          <p role="status" className="text-[13px]">
            {
              {
                completed: 'Pencarian berhasil. Exa mengembalikan hasil.',
                disabled: 'Exa Web Access tersimpan sedang nonaktif.',
                failed: 'Exa tidak dapat dihubungi atau menolak permintaan.',
                timeout: 'Uji pencarian melewati batas waktu. Coba lagi.',
                provider_auth_failed: 'Exa menolak autentikasi. Periksa kunci API yang tersimpan.',
                rate_limited: 'Kuota atau batas permintaan Exa tercapai. Coba lagi nanti.',
                invalid_response: 'Exa merespons, tetapi hasil pencarian kosong atau tidak valid.',
              }[test.data.status]
            }
          </p>
        )}
      </form>
    </SectionCard>
  )
}
