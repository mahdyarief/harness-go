import { IconArrowUp, IconBook, IconSquare, IconWorld } from '@tabler/icons-react'
import { useLayoutEffect, useRef, useState, type FormEvent, type KeyboardEvent } from 'react'

import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'

interface ComposerProps {
  disabled: boolean
  streaming: boolean
  webSearch: boolean
  webSearchAvailable: boolean
  onWebSearchChange: (value: boolean) => void
  knowledgeSearch: boolean
  onKnowledgeSearchChange: (value: boolean) => void
  onSend: (content: string) => void
  onStop: () => void
}

const MAX_LENGTH = 8000
// The textarea grows from one line to six; the button sits at 36px on touch
// screens so the send/stop control clears the 44px target with the form's
// 6px padding, and drops back to 28px on pointer-driven layouts.
const MIN_HEIGHT = 28
const MAX_HEIGHT = 160

// Prompt bar grammar adapted from Beautiful UI "Prompt Bar" (MIT, slev12397/beautiful-ui).
export default function Composer({
  disabled,
  streaming,
  webSearch,
  webSearchAvailable,
  onWebSearchChange,
  knowledgeSearch,
  onKnowledgeSearchChange,
  onSend,
  onStop,
}: ComposerProps) {
  const [value, setValue] = useState('')
  const areaRef = useRef<HTMLTextAreaElement>(null)
  const canSend = !disabled && !streaming && value.trim() !== ''

  useLayoutEffect(() => {
    const el = areaRef.current
    if (!el) return
    el.style.height = `${MIN_HEIGHT}px`
    el.style.height = `${Math.min(Math.max(el.scrollHeight, MIN_HEIGHT), MAX_HEIGHT)}px`
  }, [value])

  const submit = (event?: FormEvent) => {
    event?.preventDefault()
    const content = value.trim()
    if (!content || disabled || streaming) return
    onSend(content)
    setValue('')
  }

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    // Enter sends; Shift+Enter inserts a newline.
    if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault()
      submit()
    }
  }

  return (
    <form onSubmit={submit} className="mx-auto w-full max-w-3xl px-4 pt-2 pb-3 sm:px-6">
      <div className="border-line bg-surface shadow-card focus-within:border-line-strong relative isolate flex items-end gap-1.5 rounded-[22px] border p-1.5 transition-colors duration-150">
        <label htmlFor="chat-input" className="sr-only">
          Pesan
        </label>
        <Textarea
          id="chat-input"
          ref={areaRef}
          value={value}
          onChange={(event) => setValue(event.target.value.slice(0, MAX_LENGTH))}
          onKeyDown={onKeyDown}
          placeholder="Tulis pesan…"
          rows={1}
          disabled={disabled}
          className="text-ink placeholder:text-ink-3 min-h-9 min-w-0 flex-1 resize-none bg-transparent px-0 py-2 pl-3 text-[16px] leading-5 shadow-none focus-visible:ring-0 disabled:cursor-not-allowed disabled:opacity-60 md:min-h-7 md:py-[5px] md:text-[13px] md:leading-[18px]"
          style={{ height: MIN_HEIGHT }}
        />
        {streaming ? (
          <Button
            type="button"
            size="icon"
            radius="full"
            onClick={onStop}
            aria-label="Hentikan"
            className="md:size-7"
          >
            <IconSquare className="size-3" fill="currentColor" />
          </Button>
        ) : (
          <Button
            type="submit"
            size="icon"
            radius="full"
            disabled={!canSend}
            aria-label="Kirim"
            className={cn('md:size-7', !canSend && 'bg-line-strong cursor-not-allowed')}
          >
            <IconArrowUp className="size-4" stroke={2.5} />
          </Button>
        )}
      </div>
      <div className="mt-2 flex flex-wrap items-center justify-center gap-1.5 px-1">
        <Button
          type="button"
          size="xs"
          radius="full"
          variant={knowledgeSearch ? 'primary' : 'ghost'}
          aria-pressed={knowledgeSearch}
          aria-label={
            knowledgeSearch
              ? 'Nonaktifkan pencarian basis pengetahuan'
              : 'Aktifkan pencarian basis pengetahuan'
          }
          title="Cari di basis pengetahuan untuk jawaban berdasarkan dokumen yang Anda unggah"
          disabled={disabled || streaming}
          onClick={() => onKnowledgeSearchChange(!knowledgeSearch)}
          className={cn('h-8 shrink-0 md:h-7', knowledgeSearch && 'gap-1.5')}
        >
          <IconBook className="size-3.5" />
          {knowledgeSearch ? <span>KB aktif</span> : <span>KB</span>}
        </Button>
        {webSearchAvailable ? (
          <Button
            type="button"
            size="xs"
            radius="full"
            variant={webSearch ? 'primary' : 'ghost'}
            aria-pressed={webSearch}
            aria-label={webSearch ? 'Nonaktifkan pencarian web' : 'Aktifkan pencarian web'}
            title="Cari di web untuk jawaban dengan informasi terbaru"
            disabled={disabled || streaming}
            onClick={() => onWebSearchChange(!webSearch)}
            className={cn('h-8 shrink-0 md:h-7', webSearch && 'gap-1.5')}
          >
            <IconWorld className="size-3.5" />
            {webSearch ? <span>Web aktif</span> : <span>Web</span>}
          </Button>
        ) : null}
      </div>
      <div className="text-ink-3 mt-1.5 space-y-1 px-1 text-center text-[11px]">
        <p>
          Enter untuk kirim, Shift+Enter baris baru. Periksa kembali jawaban AI sebelum digunakan.
        </p>
        <p>
          <span className="text-ink-2 font-medium">KB</span> mencari di dokumen basis pengetahuan
          Anda beserta kutipannya. <span className="text-ink-2 font-medium">Web</span> mencari
          informasi terbaru di internet. Aktifkan keduanya bila jawaban butuh rujukan.
        </p>
      </div>
    </form>
  )
}
