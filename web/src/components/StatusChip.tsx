import type { TemplateStatus, TaskStatus } from '../api/types'
import { Chip } from './ui'

const toneMap: Record<string, 'ok' | 'warn' | 'muted' | 'danger' | 'teal'> = {
  draft: 'muted',
  pending_review: 'warn',
  approved: 'ok',
  rejected: 'danger',
  disabled: 'muted',
  pending: 'muted',
  running: 'teal',
  paused: 'warn',
  success: 'ok',
  partial: 'warn',
  failed: 'danger',
  cancelled: 'muted',
  retrying: 'teal',
}

export function StatusChip({ status }: { status: TemplateStatus | TaskStatus | string }) {
  return <Chip tone={toneMap[status] ?? 'muted'}>{status}</Chip>
}
