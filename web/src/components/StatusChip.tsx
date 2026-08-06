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
  const labelMap: Record<string, string> = {
    draft: '草稿',
    pending_review: '待审核',
    approved: '已通过',
    rejected: '已拒绝',
    disabled: '已停用',
    pending: '待执行',
    running: '进行中',
    paused: '已暂停',
    success: '成功',
    partial: '部分成功',
    failed: '失败',
    cancelled: '已取消',
    retrying: '重试中',
  }
  return <Chip tone={toneMap[status] ?? 'muted'}>{labelMap[status] ?? status}</Chip>
}
