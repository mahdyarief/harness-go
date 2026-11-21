import { useMutation, useQuery } from '@tanstack/react-query'
import { useEffect, useState, type FormEvent } from 'react'
import { toast } from 'sonner'

import SectionCard from '@/components/block/common/section-card'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { ChevronDown } from 'lucide-react'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { LlmConfigFormSchema } from '@/lib/api/dtos/platform/llm-config/schema'
import type { LlmConfig } from '@/lib/api/models/llm-config'
import { llmConfigQueries } from '@/lib/api/queries/llm-config'
import { authQueries } from '@/lib/api/queries/auth'
import ExaConfigContent from './exa-config'

export default function LlmConfigPage() {
  const profile = useQuery(authQueries.profile())
  if (profile.isPending) return <p role="status">Memeriksa akses…</p>
  if (profile.isError)
    return (
      <div role="alert">
        <p>Gagal memeriksa akses.</p>
        <Button
          variant="outline"
          disabled={profile.isFetching}
          onClick={() => void profile.refetch()}
        >
          Coba lagi
        </Button>
      </div>
    )
  if (profile.data?.data?.role !== 'super_user')
    return <p role="alert">Anda tidak memiliki akses ke halaman ini.</p>
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h1 className="text-ink text-2xl font-semibold tracking-tight">Konfigurasi AI</h1>
        <p className="text-ink-2 text-[13px]">Pengaturan integrasi untuk administrator platform.</p>
      </div>
      <Tabs defaultValue="llm">
        <TabsList>
          <TabsTrigger value="llm">LLM</TabsTrigger>
          <TabsTrigger value="exa">Exa Web Access</TabsTrigger>
        </TabsList>
        <TabsContent value="llm">
          <LlmConfigContent />
        </TabsContent>
        <TabsContent value="exa">
          <ExaConfigContent />
        </TabsContent>
      </Tabs>
    </div>
  )
}

function LlmConfigContent() {
  const config = useQuery(llmConfigQueries.get())
  if (config.isPending) return <p role="status">Memuat konfigurasi LLM…</p>
  if (config.isError && !config.data?.data)
    return (
      <div role="alert">
        <p>Konfigurasi LLM gagal dimuat.</p>
        <Button variant="outline" onClick={() => config.refetch()}>
          Coba lagi
        </Button>
      </div>
    )
  if (!config.data?.data) return <p role="status">Konfigurasi LLM belum tersedia.</p>
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
      <LlmConfigForm initial={config.data.data} />
    </>
  )
}

function LlmConfigForm({ initial }: { initial: LlmConfig }) {
  const [enabled, setEnabled] = useState(initial.enabled)
  const [endpoint, setEndpoint] = useState(initial.endpoint)
  const [model, setModel] = useState(initial.model)
  const [timeout, setTimeout] = useState(String(initial.timeout_seconds))
  const [tokens, setTokens] = useState(String(initial.max_tokens))
  const [credential, setCredential] = useState('')
  const [clearCredential, setClearCredential] = useState(false)
  const [baseline, setBaseline] = useState(initial)
  const [validationError, setValidationError] = useState('')
  const [invalidFields, setInvalidFields] = useState<string[]>([])
  const save = useMutation(llmConfigQueries.update())
  const test = useMutation(llmConfigQueries.test())
  const dirty =
    enabled !== baseline.enabled ||
    endpoint !== baseline.endpoint ||
    model !== baseline.model ||
    timeout !== String(baseline.timeout_seconds) ||
    tokens !== String(baseline.max_tokens) ||
    Boolean(credential) ||
    clearCredential
  const changedOnServer = JSON.stringify(initial) !== JSON.stringify(baseline)

  function adopt(config: LlmConfig) {
    setBaseline(config)
    setEnabled(config.enabled)
    setEndpoint(config.endpoint)
    setModel(config.model)
    setTimeout(String(config.timeout_seconds))
    setTokens(String(config.max_tokens))
    setCredential('')
    setClearCredential(false)
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
    const validation = LlmConfigFormSchema.safeParse({
      enabled,
      endpoint,
      model,
      timeout_seconds: Number(timeout),
      max_tokens: Number(tokens),
      credential,
      clear_credential: clearCredential,
    })
    const endpointNeedsCredential =
      baseline.has_credential &&
      endpoint.trim() !== baseline.endpoint &&
      !clearCredential &&
      !credential.trim()
    const missingCredential = enabled && !baseline.has_credential && !credential.trim()
    if (!validation.success || endpointNeedsCredential || missingCredential) {
      const issues = validation.success ? [] : validation.error.issues
      const fields = issues.map((issue) => String(issue.path[0]))
      const messages = issues.map((issue) => issue.message)
      if (endpointNeedsCredential || missingCredential) {
        fields.push('credential')
        messages.push(
          endpointNeedsCredential
            ? 'Ganti atau hapus kredensial saat mengubah endpoint.'
            : 'Kredensial wajib diisi untuk mengaktifkan integrasi.',
        )
      }
      setInvalidFields(fields)
      setValidationError(messages.join('\n'))
      const ids: Record<string, string> = {
        endpoint: 'llm-url',
        model: 'llm-model',
        timeout_seconds: 'llm-timeout',
        max_tokens: 'llm-tokens',
        credential: 'llm-key',
        enabled: 'llm-enabled',
      }
      document.getElementById(ids[fields[0]])?.focus()
      return
    }
    setCredential('')
    try {
      await save.mutateAsync({ ...validation.data, language: initial.language })
      // PUT has no read model. GET refreshes has_credential and updated_at.
      setBaseline({
        enabled: validation.data.enabled,
        endpoint: validation.data.endpoint,
        model: validation.data.model,
        language: initial.language,
        timeout_seconds: validation.data.timeout_seconds,
        max_tokens: validation.data.max_tokens,
        has_credential: !clearCredential && (baseline.has_credential || Boolean(credential.trim())),
        updated_at: baseline.updated_at,
      })
      setEndpoint(validation.data.endpoint)
      setModel(validation.data.model)
      setClearCredential(false)
      toast.success('Konfigurasi LLM berhasil disimpan.')
    } catch {
      // Never render server messages or Axios request/response details.
      setValidationError(
        'Konfigurasi LLM gagal disimpan. Periksa konfigurasi dan izin host endpoint. Masukkan kembali kredensial jika menggantinya.',
      )
    } finally {
      save.reset()
    }
  }

  const busy = save.isPending || test.isPending

  return (
    <>
      <LlmAiSettings initial={initial} busy={busy} />
      <SectionCard
        title="Koneksi penyedia"
        description="Kunci API bersifat tulis-saja dan tidak pernah ditampilkan kembali."
      >
        <form onSubmit={submit} onChange={fieldChanged} noValidate className="flex flex-col gap-5">
          {changedOnServer && dirty && (
            <div role="status" className="space-y-2 text-[13px]">
              <p>
                Konfigurasi di server berubah. Muat konfigurasi terbaru untuk membuang perubahan
                Anda.
              </p>
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={() => adopt(initial)}
              >
                Muat konfigurasi terbaru
              </Button>
            </div>
          )}
          <fieldset disabled={busy} className="flex flex-col gap-5 disabled:opacity-60">
            <div className="flex items-center gap-3">
              <Checkbox
                id="llm-enabled"
                aria-invalid={invalidFields.includes('enabled')}
                aria-describedby={invalidFields.includes('enabled') ? 'llm-error' : undefined}
                checked={enabled}
                onCheckedChange={(value) => {
                  fieldChanged()
                  setEnabled(value === true)
                }}
              />
              <Label htmlFor="llm-enabled">Aktifkan integrasi LLM</Label>
            </div>
            <p className="text-ink-2 text-[13px]">
              Integrasi dinonaktifkan secara bawaan. Aktifkan hanya setelah konfigurasi ditinjau.
            </p>
            <div className="grid gap-5 md:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="llm-url">Endpoint chat completions</Label>
                <Input
                  id="llm-url"
                  aria-invalid={invalidFields.includes('endpoint')}
                  aria-describedby={invalidFields.includes('endpoint') ? 'llm-error' : undefined}
                  type="url"
                  value={endpoint}
                  onChange={(e) => setEndpoint(e.target.value)}
                  placeholder="https://penyedia.example/v1/chat/completions"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="llm-model">Nama model</Label>
                <Input
                  id="llm-model"
                  aria-invalid={invalidFields.includes('model')}
                  aria-describedby={invalidFields.includes('model') ? 'llm-error' : undefined}
                  value={model}
                  onChange={(e) => setModel(e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="llm-timeout">Waktu tunggu (detik)</Label>
                <Input
                  id="llm-timeout"
                  aria-invalid={invalidFields.includes('timeout_seconds')}
                  aria-describedby={
                    invalidFields.includes('timeout_seconds') ? 'llm-error' : undefined
                  }
                  type="number"
                  min="1"
                  max="300"
                  step="1"
                  required
                  value={timeout}
                  onChange={(e) => setTimeout(e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="llm-tokens">Maksimum token keluaran</Label>
                <Input
                  id="llm-tokens"
                  aria-invalid={invalidFields.includes('max_tokens')}
                  aria-describedby={invalidFields.includes('max_tokens') ? 'llm-error' : undefined}
                  type="number"
                  min="1"
                  max="16384"
                  step="1"
                  required
                  value={tokens}
                  onChange={(e) => setTokens(e.target.value)}
                />
              </div>
            </div>
            <div className="space-y-2">
              <Label htmlFor="llm-key">Kredensial baru</Label>
              <Input
                id="llm-key"
                type="password"
                autoComplete="new-password"
                disabled={clearCredential}
                value={credential}
                onChange={(e) => setCredential(e.target.value)}
                aria-invalid={invalidFields.includes('credential')}
                aria-describedby={
                  invalidFields.includes('credential') ? 'llm-key-help llm-error' : 'llm-key-help'
                }
              />
              <p id="llm-key-help" className="text-ink-2 text-[13px]">
                {baseline.has_credential
                  ? 'Kredensial tersimpan tidak pernah ditampilkan. Kosongkan untuk mempertahankannya.'
                  : 'Belum ada kredensial tersimpan. Isi untuk mengaktifkan integrasi.'}
              </p>
              {baseline.has_credential && (
                <div className="flex items-center gap-3">
                  <Checkbox
                    id="llm-clear-credential"
                    checked={clearCredential}
                    aria-describedby="llm-clear-help"
                    onCheckedChange={(value) => {
                      fieldChanged()
                      setClearCredential(value === true)
                      if (value === true) {
                        setCredential('')
                        setEnabled(false)
                      }
                    }}
                  />
                  <Label htmlFor="llm-clear-credential">Hapus kredensial tersimpan</Label>
                </div>
              )}
              {baseline.has_credential && (
                <p id="llm-clear-help" className="text-ink-2 text-[13px]">
                  Penghapusan menonaktifkan integrasi dan berlaku setelah konfigurasi disimpan.
                </p>
              )}
            </div>
          </fieldset>
          {validationError && (
            <p id="llm-error" role="alert" className="text-red whitespace-pre-line text-[13px]">
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
              {test.isPending ? 'Menguji…' : 'Uji koneksi tersimpan'}
            </Button>
          </div>
          {dirty && (
            <p className="text-ink-2 text-[13px]">Simpan perubahan sebelum menguji koneksi.</p>
          )}
          {test.isError && (
            <p role="alert" className="text-red text-[13px]">
              Uji koneksi gagal. Periksa konfigurasi dan coba lagi.
            </p>
          )}
          {test.isSuccess && !dirty && (
            <p role="status" className="text-[13px]">
              {
                {
                  completed: 'Koneksi berhasil. Penyedia mengembalikan respons.',
                  disabled: 'Integrasi LLM tersimpan sedang nonaktif.',
                  failed:
                    'Penyedia tidak dapat dihubungi atau menolak permintaan. Periksa endpoint dan model.',
                  timeout: 'Uji koneksi melewati batas waktu. Coba lagi atau sesuaikan timeout.',
                  provider_auth_failed:
                    'Penyedia menolak autentikasi atau akses. Periksa kunci API dan izin model.',
                  rate_limited:
                    'Batas permintaan atau kuota penyedia tercapai. Periksa kuota dan coba lagi.',
                  invalid_response:
                    'Penyedia merespons, tetapi respons chat kosong atau tidak valid.',
                }[test.data.status]
              }
            </p>
          )}
        </form>
      </SectionCard>
    </>
  )
}

const LANGUAGE_OPTIONS = [
  { value: 'id-ID', label: 'Indonesia' },
  { value: 'en', label: 'English' },
] as const

function LlmAiSettings({ initial, busy }: { initial: LlmConfig; busy: boolean }) {
  const save = useMutation(llmConfigQueries.update())
  const [value, setValue] = useState(initial.language || 'id-ID')
  const dirty = value !== (initial.language || 'id-ID')

  useEffect(() => {
    setValue(initial.language || 'id-ID')
  }, [initial.language])

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      await save.mutateAsync({
        enabled: initial.enabled,
        endpoint: initial.endpoint,
        model: initial.model,
        language: value,
        timeout_seconds: initial.timeout_seconds,
        max_tokens: initial.max_tokens,
        credential: undefined,
        clear_credential: false,
      })
      toast.success('Pengaturan AI berhasil disimpan.')
    } catch {
      toast.error('Pengaturan AI gagal disimpan. Coba lagi.')
    }
  }

  return (
    <SectionCard
      title="Pengaturan AI"
      description="Bahasa respons asisten, terpisah dari koneksi penyedia."
    >
      <Collapsible asChild>
        <form onSubmit={submit} className="flex flex-col gap-4">
          <CollapsibleTrigger asChild>
            <Button type="button" variant="ghost" className="w-full justify-between p-0 text-sm">
              Bahasa
              <ChevronDown className="h-4 w-4" />
            </Button>
          </CollapsibleTrigger>
          <CollapsibleContent className="space-y-3">
            <div className="space-y-2">
              <Label htmlFor="llm-language">Bahasa yang digunakan</Label>
              <Select value={value} onValueChange={setValue}>
                <SelectTrigger id="llm-language" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {LANGUAGE_OPTIONS.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-ink-2 text-[13px]">
                Bahasa menentukan bahasa jawaban asisten. Tidak memengaruhi endpoint atau
                kredensial.
              </p>
            </div>
            <div className="flex gap-3">
              <Button type="submit" size="sm" disabled={busy || !dirty}>
                {save.isPending ? 'Menyimpan…' : 'Simpan bahasa'}
              </Button>
              {dirty && <p className="text-ink-2 text-[13px]">Simpan perubahan bahasa.</p>}
            </div>
          </CollapsibleContent>
        </form>
      </Collapsible>
    </SectionCard>
  )
}
