package domain

// ChannelLabelZH 渠道展示名（运营导出 / 控制台）
func ChannelLabelZH(c ChannelType) string {
	switch c {
	case ChannelInbox:
		return "站内信"
	case ChannelSMS:
		return "短信"
	case ChannelAppPush:
		return "App推送"
	case ChannelEmail:
		return "邮件"
	case ChannelWecom:
		return "企业微信"
	case ChannelDingtalk:
		return "钉钉"
	default:
		if c == "" {
			return "-"
		}
		return string(c)
	}
}

// PushStatusLabelZH 推送流水状态展示名
func PushStatusLabelZH(s PushStatus) string {
	switch s {
	case PushStatusQueued:
		return "排队中"
	case PushStatusSending:
		return "发送中"
	case PushStatusSent:
		return "已发送"
	case PushStatusDelivered:
		return "已送达"
	case PushStatusClicked:
		return "已点击"
	case PushStatusFailed:
		return "失败"
	case PushStatusCancelled:
		return "已取消"
	case PushStatusSuppressed:
		return "已抑制"
	case PushStatusUnreachable:
		return "不可达"
	case PushStatusExpired:
		return "已过期"
	case PushStatusQuotaRejected:
		return "配额拒绝"
	default:
		if s == "" {
			return "-"
		}
		return string(s)
	}
}
