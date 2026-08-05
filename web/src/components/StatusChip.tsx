import type { TemplateStatus, TaskStatus } from '../api/types'

export function statusChip(status: TemplateStatus | TaskStatus | string) {
  const map: Record<string, string> = {
    draft: 'chip-muted',
    pending_review: 'chip-warn',
    approved: 'chip-ok',
    rejected: 'chip-danger',
    disabled: 'chip-muted',
    pending: 'chip-muted',
    running: 'chip-teal',
    paused: 'chip-warn',
    success: 'chip-ok',
    partial: 'chip-warn',
    failed: 'chip-danger',
    cancelled: 'chip-muted',
    retrying: 'chip-teal',
  }
  return map[status] ?? 'chip-muted'
}

export function StatusChip({ status }: { status: string }) {
  return <span className={`chip ${statusChip(status)}`}>{status}</span>
}
