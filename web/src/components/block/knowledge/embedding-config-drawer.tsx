import { useMutation, useQuery } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import type { KnowledgeEmbeddingConfig } from '@/lib/api/models/knowledge'
import { queries } from '@/lib/api/queries'

export default function EmbeddingConfigDrawer({ onClose }: { onClose: () => void }) {
  const config = useQuery(queries.knowledge.embeddingConfig())
  return (
    <Sheet
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <SheetContent side="right" size="md">
        <SheetHeader>
          <SheetTitle>Konfigurasi embedding</SheetTitle>
          <SheetDescription>
            Pengaturan global indeks semantik. Nonaktif secara default; tidak ada provider pengganti
            otomatis.
          </SheetDescription>
        </SheetHeader>
        {config.isPending ? (
          <p role="status" className="px-6">
            Memuat konfigurasi…
          </p>
        ) : config.isError ? (
          <div role="alert" className="space-y-3 px-6">
            <p>Konfigurasi gagal dimuat.</p>
            <Button
              variant="outline"
              disabled={config.isFetching}
              onClick={() => void config.refetch()}
            >
              Coba lagi
            </Button>
          </div>
        ) : config.data ? (
          <EmbeddingForm initial={config.data} onClose={onClose} />
        ) : null}
      </SheetContent>
    </Sheet>
  )
}

function EmbeddingForm({
  initial,
  onClose,
}: {
  initial: KnowledgeEmbeddingConfig
  onClose: () => void
}) {
  const [endpoint, setEndpoint] = useState(initial.endpoint)
  const [enabled, setEnabled] = useState(initial.enabled)
  const [credential, setCredential] = useState('')
  const [clear, setClear] = useState(false)
  const [error, setError] = useState<{
    field: 'endpoint' | 'credential'
    message: string
  } | null>(null)
  const endpointRef = useRef<HTMLInputElement>(null)
  const credentialRef = useRef<HTMLInputElement>(null)
  const clearRef = useRef<HTMLButtonElement>(null)
  const invalidate = (field: 'endpoint' | 'credential', message: string) => {
    setError({ field, message })
    const target = field === 'endpoint' ? endpointRef : clear ? clearRef : credentialRef
    target.current?.focus()
  }
  const save = useMutation(queries.knowledge.saveEmbeddingConfig())
  return (
    <form
      className="flex min-h-0 flex-1 flex-col"
      onSubmit={(event) => {
        event.preventDefault()
        if (save.isPending) return
        let url: URL
        try {
          url = new URL(endpoint.trim())
        } catch {
          invalidate('endpoint', 'Endpoint harus berupa URL HTTP atau HTTPS yang valid.')
          return
        }
        if (
          (url.protocol !== 'https:' && url.protocol !== 'http:') ||
          url.username ||
          url.password ||
          url.hash
        ) {
          invalidate(
            'endpoint',
            'Gunakan endpoint HTTP atau HTTPS tanpa credential URL atau fragmen. HTTP hanya untuk host internal yang diizinkan server.',
          )
          return
        }
        if (
          initial.has_credential &&
          endpoint.trim() !== initial.endpoint &&
          !clear &&
          !credential.trim()
        ) {
          invalidate(
            'credential',
            'Perubahan endpoint memerlukan credential baru atau penghapusan credential lama secara eksplisit.',
          )
          return
        }
        if (enabled && (clear || (!initial.has_credential && !credential.trim()))) {
          invalidate('credential', 'Isi credential sebelum mengaktifkan embedding.')
          return
        }
        setError(null)
        save.mutate(
          {
            endpoint: endpoint.trim(),
            revision: initial.revision,
            enabled,
            clear_credential: clear,
            ...(credential.trim() && !clear ? { credential: credential.trim() } : {}),
          },
          {
            onSuccess: () => {
              setCredential('')
              onClose()
            },
            onError: () => setCredential(''),
          },
        )
      }}
    >
      <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-6 pb-6 text-sm">
        <fieldset disabled={save.isPending} className="space-y-5">
          <div className="space-y-2">
            <Label htmlFor="embedding-endpoint">Endpoint embedding</Label>
            <Input
              id="embedding-endpoint"
              ref={endpointRef}
              aria-invalid={error?.field === 'endpoint'}
              aria-describedby={
                error?.field === 'endpoint'
                  ? 'embedding-endpoint-help embedding-error'
                  : 'embedding-endpoint-help'
              }
              value={endpoint}
              onChange={(event) => setEndpoint(event.target.value)}
              autoComplete="off"
            />
            <p id="embedding-endpoint-help" className="text-ink-2">
              URL lengkap. Host harus diizinkan oleh administrator server. HTTP hanya untuk host
              internal tepercaya.
            </p>
          </div>
          <div className="space-y-2">
            <Label htmlFor="embedding-model">Model (tetap)</Label>
            <Input id="embedding-model" value={initial.model} readOnly />
          </div>
          <div className="space-y-2">
            <Label htmlFor="embedding-dimensions">Dimensi (tetap)</Label>
            <Input id="embedding-dimensions" value={initial.dimensions} readOnly />
          </div>
          <div className="space-y-2">
            <Label htmlFor="embedding-revision">Revisi model (tetap)</Label>
            <Input id="embedding-revision" value={initial.revision} readOnly />
            <p id="embedding-revision-help" className="text-ink-2">
              Commit immutable 40 karakter heksadesimal, bukan main atau latest. Perubahan profil
              memerlukan indeks ulang.
            </p>
          </div>
          <div className="space-y-2">
            <Label htmlFor="embedding-credential">Credential baru</Label>
            <Input
              id="embedding-credential"
              ref={credentialRef}
              aria-invalid={error?.field === 'credential'}
              aria-describedby={
                error?.field === 'credential'
                  ? 'embedding-credential-help embedding-error'
                  : 'embedding-credential-help'
              }
              type="password"
              autoComplete="new-password"
              value={credential}
              disabled={clear}
              onChange={(event) => setCredential(event.target.value)}
            />
            <p id="embedding-credential-help" className="text-ink-2">
              {initial.has_credential
                ? 'Credential tersimpan. Kosongkan untuk mempertahankannya.'
                : 'Belum ada credential tersimpan.'}{' '}
              Credential tidak pernah ditampilkan kembali.
            </p>
          </div>
          <div className="flex items-center justify-between gap-3">
            <Label htmlFor="embedding-clear">Hapus credential tersimpan</Label>
            <Switch
              id="embedding-clear"
              ref={clearRef}
              aria-invalid={clear && error?.field === 'credential'}
              aria-describedby={
                clear && error?.field === 'credential' ? 'embedding-error' : undefined
              }
              checked={clear}
              onCheckedChange={(checked) => {
                setClear(checked)
                setCredential('')
                if (checked) setEnabled(false)
              }}
            />
          </div>
          <div className="flex items-center justify-between gap-3">
            <Label htmlFor="embedding-enabled">Aktifkan embedding</Label>
            <Switch id="embedding-enabled" checked={enabled} onCheckedChange={setEnabled} />
          </div>
        </fieldset>
        {error && (
          <p id="embedding-error" role="alert" className="text-destructive">
            {error.message}
          </p>
        )}
        {save.isError && (
          <p role="alert" className="text-destructive">
            Konfigurasi gagal disimpan. Periksa pengaturan dan izin server. Isi ulang credential
            jika diperlukan.
          </p>
        )}
      </div>
      <SheetFooter>
        <Button type="button" variant="outline" disabled={save.isPending} onClick={onClose}>
          Batal
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? 'Menyimpan…' : 'Simpan konfigurasi'}
        </Button>
      </SheetFooter>
    </form>
  )
}
