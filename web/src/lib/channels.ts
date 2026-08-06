import type { ChannelType } from '../api/types'

const LABELS: Record<string, string> = {
  inbox: '站内信',
  sms: '短信',
  app_push: 'App 推送',
  email: '邮件',
  wecom: '企业微信',
  dingtalk: '钉钉',
}

export function channelLabel(channel: ChannelType | string): string {
  return LABELS[channel] || String(channel)
}
