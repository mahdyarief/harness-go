import { useState } from 'react'
import { IconCheck, IconChevronDown, IconX } from '@tabler/icons-react'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import type { ToolActivity } from './message-list'

// Collapsible tool-call rows adapted from Beautiful UI "Tool Chips" and "Task Rows" (MIT, slev12397/beautiful-ui).
const EASE = 'cubic-bezier(0.23, 1, 0.32, 1)'

const STATE_LABEL: Record<ToolActivity['state'], string> = {
  running: 'berjalan',
  done: 'selesai',
  error: 'gagal',
}

function StateMark({ state }: { state: ToolActivity['state'] }) {
  if (state === 'running') {
    return (
      <svg aria-hidden width="16" height="16" viewBox="0 0 24 24" className="shrink-0">
        <circle cx="12" cy="12" r="10" fill="none" stroke="var(--line)" strokeWidth="2.5" />
        <circle
          cx="12"
          cy="12"
          r="10"
          fill="none"
          stroke="var(--ink-3)"
          strokeWidth="2.5"
          strokeLinecap="round"
          strokeDasharray="17.6 45.2"
          style={{ animation: 'spin 1.1s linear infinite', transformOrigin: 'center' }}
        />
      </svg>
    )
  }
  return (
    <span
      aria-hidden
      className={cn(
        'flex size-4 shrink-0 items-center justify-center rounded-full text-on-accent',
        state === 'error' ? 'bg-red' : 'bg-green',
      )}
      style={{ animation: `pop-in 300ms ${EASE} both` }}
    >
      {state === 'error' ? (
        <IconX className="size-2.5" stroke={3.5} />
      ) : (
        <IconCheck className="size-2.5" stroke={3.5} />
      )}
    </span>
  )
}

export default function ToolChips({ tools }: { tools: ToolActivity[] }) {
  const [open, setOpen] = useState(true)
  const running = tools.filter((tool) => tool.state === 'running').length
  const failed = tools.filter((tool) => tool.state === 'error').length

  const summary =
    running > 0
      ? `${running} dari ${tools.length} alat sedang berjalan`
      : failed > 0
        ? `${tools.length} alat dipanggil, ${failed} gagal`
        : `${tools.length} alat dipanggil`

  return (
    <div className="w-full max-w-md">
      <Button
        type="button"
        variant="dim"
        size="sm"
        aria-expanded={open}
        onClick={() => setOpen((current) => !current)}
        className="-mx-1.5 h-auto min-h-9 w-fit px-1.5 py-1 text-[12px] hover:bg-hover-2 md:min-h-0"
      >
        <IconChevronDown
          className="size-3 transition-transform duration-200"
          style={{ transform: open ? 'rotate(0deg)' : 'rotate(-90deg)' }}
          aria-hidden
        />
        <span className="tabular-nums">{summary}</span>
      </Button>

      <div
        className="grid transition-[grid-template-rows,opacity] duration-300"
        style={{
          gridTemplateRows: open ? '1fr' : '0fr',
          opacity: open ? 1 : 0,
          transitionTimingFunction: EASE,
        }}
      >
        <div className="overflow-hidden">
          <ul className="mt-1 flex flex-col gap-0.5 pb-0.5" aria-live="polite">
            {tools.map((tool) => (
              <li
                key={tool.callId}
                className="rounded-control flex h-7 min-w-0 items-center gap-2"
                style={{ animation: `fade-up 300ms ${EASE} both` }}
              >
                <StateMark state={tool.state} />
                <span className="bg-field text-ink-2 rounded-chip inline-flex h-5.5 min-w-0 items-center truncate px-1.5 font-mono text-[11px] shadow-hairline">
                  {tool.name}
                </span>
                <span
                  className={cn(
                    'shrink-0 text-[11px]',
                    tool.state === 'error' ? 'text-red' : 'text-ink-3',
                  )}
                >
                  {STATE_LABEL[tool.state]}
                </span>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </div>
  )
}
