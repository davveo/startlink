package push

import (
	"context"
	"errors"
	"testing"

	"github.com/starlink/push/internal/domain"
)

type stubPrefs struct {
	pref *domain.UserPreference
	err  error
}

func (s stubPrefs) Resolve(context.Context, string) (*domain.UserPreference, error) {
	return s.pref, s.err
}

func (s stubPrefs) Invalidate(string) {}

func strptr(s string) *string { return &s }

// 偏好库抖动时营销消息必须 fail-closed，且要包装成 deferred 哨兵留 PEL；
// 若退化成普通错误，重试耗尽后进 DLQ，fail-closed 反而变成丢消息。
func TestPreferenceUnavailableIsDeferred(t *testing.T) {
	g := &Gateway{prefs: stubPrefs{err: errors.New("db down")}}
	msg := domain.PushMessage{UserID: "u1", Priority: domain.PriorityNormal}

	err := g.checkUnsubscribed(context.Background(), msg, domain.ChannelSMS)
	if !errors.Is(err, domain.ErrPreferenceUnavailable) {
		t.Fatalf("期望 preference-unavailable 哨兵，实际 %v", err)
	}
	if !isRetryableSendErr(err) {
		t.Fatal("preference-unavailable 必须可重试")
	}
}

func TestPreferenceUnavailableFailsOpenForHighPriority(t *testing.T) {
	g := &Gateway{prefs: stubPrefs{err: errors.New("db down")}}
	msg := domain.PushMessage{UserID: "u1", Priority: domain.PriorityHigh}
	if err := g.checkUnsubscribed(context.Background(), msg, domain.ChannelSMS); err != nil {
		t.Fatalf("事务消息应 fail-open，实际 %v", err)
	}
}

func TestPreferenceUnavailableFailsOpenWhenConfigured(t *testing.T) {
	g := &Gateway{prefs: stubPrefs{err: errors.New("db down")}, prefOpen: true}
	msg := domain.PushMessage{UserID: "u1", Priority: domain.PriorityNormal}
	if err := g.checkUnsubscribed(context.Background(), msg, domain.ChannelSMS); err != nil {
		t.Fatalf("显式 fail_open 应放行，实际 %v", err)
	}
}

// 无偏好记录是绝大多数用户的情况，绝不能被拦。
func TestNoPreferenceRecordDoesNotBlock(t *testing.T) {
	g := &Gateway{prefs: stubPrefs{pref: nil}}
	if err := g.checkUnsubscribed(context.Background(), domain.PushMessage{UserID: "u1"}, domain.ChannelSMS); err != nil {
		t.Fatalf("无偏好记录应放行，实际 %v", err)
	}
}

func TestPreferenceBlocksOptedOutChannel(t *testing.T) {
	g := &Gateway{prefs: stubPrefs{pref: &domain.UserPreference{
		UserID:             "u1",
		OptOutChannelsJSON: strptr(`["sms"]`),
	}}}
	msg := domain.PushMessage{UserID: "u1", Priority: domain.PriorityNormal}

	err := g.checkUnsubscribed(context.Background(), msg, domain.ChannelSMS)
	if !errors.Is(err, domain.ErrUnsubscribed) {
		t.Fatalf("退订渠道应被拦，实际 %v", err)
	}
	if isRetryableSendErr(err) {
		t.Fatal("退订是终态，不可重试")
	}
	// 未退订的渠道不受影响
	if err := g.checkUnsubscribed(context.Background(), msg, domain.ChannelEmail); err != nil {
		t.Fatalf("未退订渠道不应被拦，实际 %v", err)
	}
}

func TestPreferenceBlocksMarketingButNotTransactional(t *testing.T) {
	g := &Gateway{prefs: stubPrefs{pref: &domain.UserPreference{
		UserID:          "u1",
		MarketingOptOut: true,
	}}}
	if err := g.checkUnsubscribed(context.Background(),
		domain.PushMessage{UserID: "u1", Priority: domain.PriorityNormal}, domain.ChannelSMS); !errors.Is(err, domain.ErrUnsubscribed) {
		t.Fatalf("营销退订应拦住 normal 消息，实际 %v", err)
	}
	// 验证码走 high，不能因为营销退订发不出去
	if err := g.checkUnsubscribed(context.Background(),
		domain.PushMessage{UserID: "u1", Priority: domain.PriorityHigh}, domain.ChannelSMS); err != nil {
		t.Fatalf("事务消息不受营销退订影响，实际 %v", err)
	}
}

// biz_scene 应能在活动未显式指定 topic 时兜底充当主题，否则按品类退订形同虚设。
func TestPreferenceBlocksOptedOutTopicFromBizScene(t *testing.T) {
	g := &Gateway{prefs: stubPrefs{pref: &domain.UserPreference{
		UserID:           "u1",
		OptOutTopicsJSON: strptr(`["promotion"]`),
	}}}
	msg := domain.PushMessage{UserID: "u1", BizScene: "promotion", Priority: domain.PriorityNormal}
	if err := g.checkUnsubscribed(context.Background(), msg, domain.ChannelSMS); !errors.Is(err, domain.ErrUnsubscribed) {
		t.Fatalf("biz_scene 退化的主题应被拦，实际 %v", err)
	}

	other := domain.PushMessage{UserID: "u1", BizScene: "billing", Priority: domain.PriorityNormal}
	if err := g.checkUnsubscribed(context.Background(), other, domain.ChannelSMS); err != nil {
		t.Fatalf("未退订主题不应被拦，实际 %v", err)
	}
}

// 显式 topic 优先于 biz_scene。
func TestPreferenceTopicOverridesBizScene(t *testing.T) {
	g := &Gateway{prefs: stubPrefs{pref: &domain.UserPreference{
		UserID:           "u1",
		OptOutTopicsJSON: strptr(`["promotion"]`),
	}}}
	msg := domain.PushMessage{UserID: "u1", Topic: "billing", BizScene: "promotion", Priority: domain.PriorityNormal}
	if err := g.checkUnsubscribed(context.Background(), msg, domain.ChannelSMS); err != nil {
		t.Fatalf("显式 topic 应覆盖 biz_scene，实际 %v", err)
	}
}

// 拦截原因要落进 error_msg，否则运营只看到一个笼统的 unsubscribed，
// 分不清是名单退订还是用户自己关的营销开关。
func TestPreferenceBlockReasonIsSpecific(t *testing.T) {
	g := &Gateway{prefs: stubPrefs{pref: &domain.UserPreference{UserID: "u1", MarketingOptOut: true}}}
	err := g.checkUnsubscribed(context.Background(),
		domain.PushMessage{UserID: "u1", Priority: domain.PriorityNormal}, domain.ChannelSMS)
	if err == nil || err.Error() == domain.ErrUnsubscribed.Error() {
		t.Fatalf("拦截原因应更具体，实际 %v", err)
	}
}

func TestUserQuietHours(t *testing.T) {
	pref := &domain.UserPreference{
		UserID:     "u1",
		Timezone:   "UTC",
		QuietStart: "22:00",
		QuietEnd:   "08:00",
	}
	g := &Gateway{prefs: stubPrefs{pref: pref}}

	// high 优先级不受用户免打扰约束
	quiet, err := g.inUserQuietHours(context.Background(),
		domain.PushMessage{UserID: "u1", Priority: domain.PriorityHigh})
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if quiet {
		t.Fatal("事务消息不应被用户免打扰拦下")
	}

	// 无免打扰配置时放行
	g2 := &Gateway{prefs: stubPrefs{pref: &domain.UserPreference{UserID: "u1"}}}
	quiet, err = g2.inUserQuietHours(context.Background(),
		domain.PushMessage{UserID: "u1", Priority: domain.PriorityNormal})
	if err != nil || quiet {
		t.Fatalf("未配置免打扰应放行，quiet=%v err=%v", quiet, err)
	}
}

// 偏好不可判定时免打扰判断要把错误透出去，不能当作「不在免打扰内」放行。
func TestUserQuietHoursPropagatesResolveError(t *testing.T) {
	g := &Gateway{prefs: stubPrefs{err: errors.New("db down")}}
	_, err := g.inUserQuietHours(context.Background(),
		domain.PushMessage{UserID: "u1", Priority: domain.PriorityNormal})
	if !errors.Is(err, domain.ErrPreferenceUnavailable) {
		t.Fatalf("期望 preference-unavailable，实际 %v", err)
	}
}

func TestClampErrMsg(t *testing.T) {
	long := make([]byte, maxErrMsgLen+50)
	for i := range long {
		long[i] = 'x'
	}
	if got := clampErrMsg(string(long)); len(got) != maxErrMsgLen {
		t.Fatalf("超长文案应被截断到 %d，实际 %d", maxErrMsgLen, len(got))
	}
	if got := clampErrMsg("short"); got != "short" {
		t.Fatalf("短文案不应被改动，实际 %q", got)
	}
}
