export function taskStatusLabel(status: string): string {
  const map: Record<string, string> = {
    draft: '草稿',
    pending: '待执行',
    running: '进行中',
    paused: '已暂停',
    success: '成功',
    partial: '部分成功',
    failed: '失败',
    cancelled: '已取消',
    retrying: '重试中',
  }
  return map[status] ?? status
}

export function channelLabel(channel: string): string {
  const map: Record<string, string> = {
    app_push: 'App推送',
    sms: '短信',
    email: '邮件',
    inbox: '站内信',
    wecom: '企业微信',
    dingtalk: '钉钉',
  }
  return map[channel] ?? (channel || '-')
}

export function priorityLabel(p: string): string {
  if (p === 'high') return '高优'
  if (p === 'normal') return '普通'
  return p || '-'
}

export function pushStatusLabel(status: string): string {
  const map: Record<string, string> = {
    queued: '已入队',
    sending: '发送中',
    sent: '已发送',
    delivered: '已送达',
    clicked: '已点击',
    failed: '失败',
    suppressed: '已抑制',
    unreachable: '不可达',
    cancelled: '已取消',
    expired: '已过期',
    quota_rejected: '配额拒绝',
  }
  return map[status] ?? status
}

export function channelModeLabel(mode: string): string {
  const map: Record<string, string> = {
    single: '单渠道',
    fallback: '降级',
    parallel: '并行',
    all_success: '全成功',
    conditional: '条件路由',
    cost_priority: '成本优先',
  }
  return map[mode] ?? mode
}

/** 业务场景可选项（配置约定 + 文档示例，非强类型枚举） */
export const BIZ_SCENE_OPTIONS: { value: string; label: string; hint: string }[] = [
  { value: 'demo', label: 'demo · 联调演示', hint: '内置 Demo 人群；本地联调默认场景' },
  { value: 'dev', label: 'dev · 开发调试', hint: '内置 Demo 人群；开发环境常用' },
  { value: 'marketing', label: 'marketing · 营销促销', hint: '普通营销队列；可对接真实圈人' },
  { value: 'txn', label: 'txn · 事务通知', hint: '未显式指定优先级时自动走高优队列' },
  { value: 'otp', label: 'otp · 验证码', hint: '未显式指定优先级时自动走高优队列' },
  { value: 'security', label: 'security · 安全通知', hint: '未显式指定优先级时自动走高优队列' },
  { value: 'payment', label: 'payment · 支付通知', hint: '未显式指定优先级时自动走高优队列' },
  { value: 'transactional', label: 'transactional · 事务型', hint: '未显式指定优先级时自动走高优队列' },
]

export function auditActionLabel(action: string): string {
  const map: Record<string, string> = {
    'auth.login': '登录',
    'auth.logout': '登出',
    'campaign.create': '创建活动',
    'campaign.update': '更新活动',
    'campaign.publish': '发布活动',
    'campaign.cancel': '取消活动',
    'campaign.pause': '暂停活动',
    'campaign.resume': '恢复活动',
    'campaign.retry': '失败重推',
    'campaign.copy': '复制活动',
    'campaign.batch': '批量操作',
    'campaign.preflight': '活动预检',
    'campaign.dry_run': 'Dry-run / 测试发送',
    'audience.estimate': '人群试算',
    'export.create': '创建导出',
    'template.create': '创建模板',
    'template.update': '更新模板',
    'template.delete': '删除模板',
    'template.submit': '提交审核',
    'template.approve': '审核通过',
    'template.reject': '审核驳回',
    'template.disable': '停用模板',
    'template.enable': '启用模板',
    'template.rollback': '模板回滚',
    'template.preview': '模板预览',
    'notification.read': '通知已读',
    'notification.read_all': '全部已读',
    'callback.receipt': '渠道回执',
  }
  return map[action] ?? action
}
