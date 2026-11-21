import { useEffect, useState } from 'react'

import { TypingAnimation } from '@/components/ui/typing-animation'

// Pixel-grid loader adapted from Beautiful UI "Loading State" (MIT, slev12397/beautiful-ui).
const DELAYS = Array.from({ length: 9 }, (_, i) => {
  const r = Math.floor(i / 3)
  const c = i % 3
  return (c + Math.abs(r - 1)) * 90
})

function useElapsed() {
  const [ds, setDs] = useState(0)
  useEffect(() => {
    const timer = setInterval(() => setDs((d) => d + 1), 100)
    return () => clearInterval(timer)
  }, [])
  const total = ds / 10
  if (total < 60) return `${total.toFixed(1)}s`
  return `${Math.floor(total / 60)}m ${(total % 60).toFixed(1)}s`
}

export default function LoadingState({ label }: { label: string }) {
  const elapsed = useElapsed()
  return (
    <div role="status" className="flex w-fit items-center gap-2.5">
      <span aria-hidden className="grid shrink-0 grid-cols-[repeat(3,4px)] gap-[1.5px]">
        {DELAYS.map((delay, index) => (
          <span
            key={index}
            className="bg-ink size-[4px] rounded-[1px] opacity-15"
            style={{ animation: `pixel-on 650ms ease-in-out ${delay}ms infinite` }}
          />
        ))}
      </span>
      <span className="relative inline-block">
        <span className="sr-only">{label}</span>
        <span aria-hidden className="invisible text-[13px] font-medium whitespace-nowrap">
          {label}
        </span>
        <TypingAnimation
          aria-hidden
          duration={45}
          showCursor={false}
          startOnView={false}
          className="absolute inset-0 bg-clip-text text-[13px] font-medium whitespace-nowrap text-transparent"
          style={{
            backgroundImage:
              'linear-gradient(90deg, var(--ink-3) 35%, var(--ink) 50%, var(--ink-3) 65%)',
            backgroundSize: '200% 100%',
            animation: 'shimmer-text 1.4s linear infinite',
          }}
        >
          {label}
        </TypingAnimation>
      </span>
      <span className="text-ink-3 font-mono text-[12px] tabular-nums">{elapsed}</span>
    </div>
  )
}
