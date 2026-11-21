import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { Components } from 'react-markdown'

import { cn } from '@/lib/utils'

// Renders model text with GitHub-flavoured markdown. react-markdown does not
// parse raw HTML by default, so this stays XSS-safe without dangerouslySetInnerHTML.
const components: Components = {
  a: ({ href, children, ...props }) => (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer nofollow"
      className="text-ink decoration-line-strong font-medium underline underline-offset-2 hover:decoration-ink"
      {...props}
    >
      {children}
    </a>
  ),
  ul: ({ children, ...props }) => (
    <ul className="my-2 list-disc space-y-1 pl-5" {...props}>
      {children}
    </ul>
  ),
  ol: ({ children, ...props }) => (
    <ol className="my-2 list-decimal space-y-1 pl-5" {...props}>
      {children}
    </ol>
  ),
  li: ({ children, ...props }) => (
    <li className="leading-relaxed" {...props}>
      {children}
    </li>
  ),
  p: ({ children, ...props }) => (
    <p className="my-2 leading-relaxed first:mt-0 last:mb-0" {...props}>
      {children}
    </p>
  ),
  h1: ({ children, ...props }) => (
    <h1 className="mt-3 mb-2 text-[15px] font-semibold tracking-tight" {...props}>
      {children}
    </h1>
  ),
  h2: ({ children, ...props }) => (
    <h2 className="mt-3 mb-2 text-[14px] font-semibold tracking-tight" {...props}>
      {children}
    </h2>
  ),
  h3: ({ children, ...props }) => (
    <h3 className="mt-2 mb-1.5 text-[13px] font-semibold" {...props}>
      {children}
    </h3>
  ),
  strong: ({ children, ...props }) => (
    <strong className="font-semibold" {...props}>
      {children}
    </strong>
  ),
  code: ({ className, children, ...props }) => {
    const isBlock = className?.includes('language-')
    if (isBlock) {
      return (
        <code
          className="bg-field text-ink block overflow-x-auto rounded-[10px] border border-line p-3 text-[12px] leading-relaxed"
          {...props}
        >
          {children}
        </code>
      )
    }
    return (
      <code
        className="bg-field text-ink rounded-[6px] border border-line px-1 py-0.5 text-[12px]"
        {...props}
      >
        {children}
      </code>
    )
  },
  pre: ({ children, ...props }) => (
    <pre className="my-2" {...props}>
      {children}
    </pre>
  ),
  blockquote: ({ children, ...props }) => (
    <blockquote className="border-line-strong text-ink-2 my-2 border-l-2 pl-3 italic" {...props}>
      {children}
    </blockquote>
  ),
  table: ({ children, ...props }) => (
    <div className="my-2 overflow-x-auto rounded-[10px] border border-line">
      <table className="w-full border-collapse text-[12px]" {...props}>
        {children}
      </table>
    </div>
  ),
  th: ({ children, ...props }) => (
    <th
      className="bg-field text-ink border-b border-line px-3 py-1.5 text-left font-semibold"
      {...props}
    >
      {children}
    </th>
  ),
  td: ({ children, ...props }) => (
    <td className="border-b border-line px-3 py-1.5" {...props}>
      {children}
    </td>
  ),
  hr: ({ ...props }) => <hr className="border-line my-3" {...props} />,
}

interface MarkdownProps {
  children: string
  className?: string
}

export default function Markdown({ children, className }: MarkdownProps) {
  return (
    <div className={cn('text-[13px]', className)}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {children}
      </ReactMarkdown>
    </div>
  )
}
