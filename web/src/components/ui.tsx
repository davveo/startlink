import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode, SelectHTMLAttributes, TextareaHTMLAttributes } from 'react'
import { Link, type LinkProps } from 'react-router-dom'
import { cn } from '../lib/cn'

const btnBase =
  'inline-flex items-center justify-center gap-1.5 rounded-full border border-transparent px-4.5 py-2.5 text-sm font-semibold transition duration-150 enabled:hover:-translate-y-px disabled:cursor-not-allowed disabled:opacity-55'

const btnVariants = {
  primary: 'bg-teal text-[#042422] hover:enabled:bg-teal-deep hover:enabled:text-white',
  ink: 'bg-ink text-white hover:enabled:bg-ink-soft',
  ghost: 'border-line bg-transparent text-ink hover:enabled:bg-white/60',
  danger: 'border-rose/35 bg-transparent text-rose hover:enabled:bg-rose/5',
} as const

type BtnVariant = keyof typeof btnVariants

export function Button({
  variant = 'primary',
  className,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: BtnVariant }) {
  return <button className={cn(btnBase, btnVariants[variant], className)} {...props} />
}

export function ButtonLink({
  variant = 'primary',
  className,
  ...props
}: LinkProps & { variant?: BtnVariant }) {
  return <Link className={cn(btnBase, btnVariants[variant], className)} {...props} />
}

export function Panel({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <section
      className={cn(
        'animate-rise rounded-[10px] border border-line/90 bg-white/72 p-5 shadow-panel backdrop-blur-sm',
        className,
      )}
    >
      {children}
    </section>
  )
}

export function PanelTitle({ children, className }: { children: ReactNode; className?: string }) {
  return <h2 className={cn('mb-4 text-lg font-semibold text-ink', className)}>{children}</h2>
}

export function PageHead({
  title,
  description,
  actions,
}: {
  title: string
  description?: ReactNode
  actions?: ReactNode
}) {
  return (
    <div className="mb-6 flex animate-rise flex-wrap items-end justify-between gap-4">
      <div>
        <h1 className="text-3xl font-bold tracking-tight text-ink md:text-4xl">{title}</h1>
        {description ? <p className="mt-1.5 max-w-2xl text-muted">{description}</p> : null}
      </div>
      {actions ? <div className="flex flex-wrap gap-2">{actions}</div> : null}
    </div>
  )
}

export function Field({
  label,
  hint,
  children,
  className,
}: {
  label: ReactNode
  hint?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <label className={cn('mb-3.5 grid gap-1.5', className)}>
      <span className="text-[0.82rem] font-semibold text-ink-soft">{label}</span>
      {children}
      {hint ? <small className="text-muted">{hint}</small> : null}
    </label>
  )
}

const control =
  'w-full rounded-lg border border-line bg-white px-3 py-2.5 text-ink outline-none transition focus:border-teal focus:ring-2 focus:ring-teal/20'

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={cn(control, className)} {...props} />
}

export function Select({ className, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select className={cn(control, className)} {...props} />
}

export function Textarea({ className, ...props }: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      className={cn(control, 'min-h-[110px] resize-y font-mono text-sm', className)}
      {...props}
    />
  )
}

export function Toast({ kind, children }: { kind: 'ok' | 'error'; children: ReactNode }) {
  return (
    <div
      className={cn(
        'mb-4 rounded-lg border px-4 py-3 text-sm',
        kind === 'error' && 'border-rose/25 bg-rose/8 text-rose',
        kind === 'ok' && 'border-teal/28 bg-teal/10 text-teal-deep',
      )}
    >
      {children}
    </div>
  )
}

export function Stat({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="rounded-lg border border-line bg-linear-to-br from-white to-paper-deep p-4">
      <div className="text-xs text-muted">{label}</div>
      <div className="mt-1 font-display text-[1.7rem] font-semibold leading-tight text-ink">{children}</div>
    </div>
  )
}

export function Chip({
  tone = 'muted',
  children,
  className,
}: {
  tone?: 'ok' | 'warn' | 'muted' | 'danger' | 'teal'
  children: ReactNode
  className?: string
}) {
  const tones = {
    ok: 'bg-ok/12 text-ok',
    warn: 'bg-amber/14 text-amber',
    muted: 'bg-muted/12 text-muted',
    danger: 'bg-rose/12 text-rose',
    teal: 'bg-teal/14 text-teal-deep',
  }
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 rounded-full px-2.5 py-0.5 text-xs font-semibold',
        tones[tone],
        className,
      )}
    >
      {children}
    </span>
  )
}

export function Mono({ children, className }: { children: ReactNode; className?: string }) {
  return <span className={cn('font-mono text-[0.86rem]', className)}>{children}</span>
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="py-4 text-muted">{children}</div>
}

export function BtnRow({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn('flex flex-wrap gap-2', className)}>{children}</div>
}

export function ProgressBar({ percent }: { percent: number }) {
  return (
    <div className="my-3 h-2.5 overflow-hidden rounded-full bg-paper-deep">
      <div
        className="h-full rounded-full bg-linear-to-r from-teal to-[#35d0c3] transition-[width] duration-300"
        style={{ width: `${Math.max(0, Math.min(100, percent))}%` }}
      />
    </div>
  )
}

export function TableWrap({ children }: { children: ReactNode }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse text-[0.92rem]">
        {children}
      </table>
    </div>
  )
}

export function Th({ children, className }: { children?: ReactNode; className?: string }) {
  return (
    <th
      className={cn(
        'border-b border-line px-2 py-3 text-left text-xs font-semibold uppercase tracking-wider text-muted',
        className,
      )}
    >
      {children}
    </th>
  )
}

export function Td({
  children,
  className,
  colSpan,
  title,
}: {
  children?: ReactNode
  className?: string
  colSpan?: number
  title?: string
}) {
  return (
    <td colSpan={colSpan} title={title} className={cn('border-b border-line px-2 py-3 align-top', className)}>
      {children}
    </td>
  )
}
