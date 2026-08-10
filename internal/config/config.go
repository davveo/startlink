package config

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server       ServerConfig       `yaml:"server"`
	Log          LogConfig          `yaml:"log"`
	Auth         AuthConfig         `yaml:"auth"`
	MySQL        MySQLConfig        `yaml:"mysql"`
	Redis        RedisConfig        `yaml:"redis"`
	MQ           MQConfig           `yaml:"mq"`
	Scheduler    SchedulerConfig    `yaml:"scheduler"`
	Pusher       PusherConfig       `yaml:"pusher"`
	Campaign     CampaignConfig     `yaml:"campaign"`
	Webhook      WebhookConfig      `yaml:"webhook"`
	Callback     CallbackConfig     `yaml:"callback"`
	Audience     AudienceConfig     `yaml:"audience"`
	Freq         FreqConfig         `yaml:"freq"`
	Compliance   ComplianceConfig   `yaml:"compliance"`
	Preference   PreferenceConfig   `yaml:"preference"`
	ChannelQuota ChannelQuotaConfig `yaml:"channel_quota"`
}

// AuthConfig 运营台登录门禁（配置文件账号 + 签名 Session Cookie）
type AuthConfig struct {
	Enabled       bool       `yaml:"enabled"`
	SessionSecret string     `yaml:"session_secret"`
	CookieName    string     `yaml:"cookie_name"`
	TTLHours      int        `yaml:"ttl_hours"`
	CookieSecure  bool       `yaml:"cookie_secure"`
	Users         []AuthUser `yaml:"users"`
}

type AuthUser struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	// Role 绑定角色：admin | operator | viewer；空则 viewer
	Role string `yaml:"role"`
}

// LogConfig 全局日志
type LogConfig struct {
	// Level debug|info|warn|error，默认 info
	Level string `yaml:"level"`
	// Format text|json，默认 text；text 形如：2026-08-05 18:52:26 [INFO] msg key=value
	Format string `yaml:"format"`
}

type ServerConfig struct {
	Addr                 string   `yaml:"addr"`
	Mode                 string   `yaml:"mode"`
	AllowedOrigins       []string `yaml:"allowed_origins"`
	MaxBodyBytes         int64    `yaml:"max_body_bytes"`
	ReadHeaderTimeoutSec int      `yaml:"read_header_timeout_sec"`
	ReadTimeoutSec       int      `yaml:"read_timeout_sec"`
	WriteTimeoutSec      int      `yaml:"write_timeout_sec"`
	IdleTimeoutSec       int      `yaml:"idle_timeout_sec"`
}

type MySQLConfig struct {
	DSN     string `yaml:"dsn"`
	MaxIdle int    `yaml:"max_idle"`
	MaxOpen int    `yaml:"max_open"`
}

type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type MQConfig struct {
	// Driver 队列驱动：redis_stream（默认）| rocketmq | memory；可扩展自定义 Register
	Driver string `yaml:"driver"`

	// 兼容旧配置：仅配 stream/group 时作为 normal，并自动推导 high
	Stream string `yaml:"stream"`
	Group  string `yaml:"group"`

	High   MQQueueConfig `yaml:"high"`
	Normal MQQueueConfig `yaml:"normal"`

	// HighBizScenes 未显式传 priority 时，这些 biz_scene 自动走高优队列
	HighBizScenes []string `yaml:"high_biz_scenes"`

	RocketMQ RocketMQConfig `yaml:"rocketmq"`
	Memory   MemoryMQConfig `yaml:"memory"`

	// RedisStream 仅 driver=redis_stream 生效：PEL / DLQ / 容量治理
	RedisStream RedisStreamMQConfig `yaml:"redis_stream"`
}

// RedisStreamMQConfig Redis Stream 可靠性与容量选项
type RedisStreamMQConfig struct {
	// ClaimMinIdleMs PEL 空闲超过该毫秒才可被 XAUTOCLAIM（默认 30000）
	ClaimMinIdleMs int `yaml:"claim_min_idle_ms"`
	// ClaimBatch 每轮 XAUTOCLAIM 最多认领条数（默认 16）
	ClaimBatch int `yaml:"claim_batch"`
	// MaxDelivery 同一消息最大投递次数（含首次 XREADGROUP）；达到后写入 DLQ 并 ACK（默认 5）
	MaxDelivery int `yaml:"max_delivery"`
	// DLQSuffix 死信 Stream 后缀，完整名为 topic+suffix（默认 ":dlq"）
	DLQSuffix string `yaml:"dlq_suffix"`

	// MaxLen 主队列 XADD/XTRIM 上限；未配置默认 100000；显式 -1 表示不限制
	MaxLen int64 `yaml:"maxlen"`
	// DLQMaxLen 死信队列上限；0=跟随 MaxLen；-1=不限制；>0=独立上限
	DLQMaxLen int64 `yaml:"dlq_maxlen"`
	// MaxLenApprox 使用近似裁剪 MAXLEN ~（默认 true，性能更好）
	MaxLenApprox *bool `yaml:"maxlen_approx"`
	// TrimIntervalSec 消费侧定期 XTRIM 间隔秒；未配置默认 60；显式 -1 表示关闭定期裁剪（仍可依赖 XADD MAXLEN）
	TrimIntervalSec int `yaml:"trim_interval_sec"`
	// AckXDel ACK 成功后是否 XDEL 条目以释放内存（默认 true）
	AckXDel *bool `yaml:"ack_xdel"`
}

// MQQueueConfig 单优先级队列配置（驱动无关：topic/stream + consumer group）
type MQQueueConfig struct {
	Topic  string `yaml:"topic"`  // 通用名（RocketMQ Topic / Kafka Topic）
	Stream string `yaml:"stream"` // Redis Stream 名；未配 topic 时回退用 stream
	Group  string `yaml:"group"`  // Consumer Group
}

// TopicOrStream 解析队列名：优先 topic，其次 stream（兼容旧配置）
func (q MQQueueConfig) TopicOrStream() string {
	if q.Topic != "" {
		return q.Topic
	}
	return q.Stream
}

type RocketMQConfig struct {
	NameServers []string `yaml:"name_servers"`
	AccessKey   string   `yaml:"access_key"`
	SecretKey   string   `yaml:"secret_key"`
	Namespace   string   `yaml:"namespace"`
	Retry       int      `yaml:"retry"`
}

type MemoryMQConfig struct {
	BufferSize int `yaml:"buffer_size"`
}

type SchedulerConfig struct {
	BatchSize         int `yaml:"batch_size"`
	WorkerConcurrency int `yaml:"worker_concurrency"`
	// SplitConcurrency 同实例并行拆分活动数（默认 2）
	SplitConcurrency int `yaml:"split_concurrency"`
	PollIntervalMs   int `yaml:"poll_interval_ms"`
	ClaimTimeoutSec  int `yaml:"claim_timeout_sec"`
	// SplitLeaseSec 拆分租约超时；超时且无子任务的 running 主任务可被其它实例重拆（默认 90）
	SplitLeaseSec int `yaml:"split_lease_sec"`
}

type PusherConfig struct {
	WorkerConcurrency     int `yaml:"worker_concurrency"`      // 普通队列并发（营销）
	HighWorkerConcurrency int `yaml:"high_worker_concurrency"` // 高优队列并发（事务）
	RateLimitQPS          int `yaml:"rate_limit_qps"`
	// MaxRetry 默认额外重试次数（总尝试 = max_retry+1）；可被 channels.*.max_retry 覆盖
	MaxRetry int `yaml:"max_retry"`
	// RetryBackoff 默认退避曲线：exponential（默认）| linear | fixed
	RetryBackoff string `yaml:"retry_backoff"`
	// RetryBaseMs 退避基数（毫秒）；exponential/linear/fixed 均以此为底
	RetryBaseMs int `yaml:"retry_base_ms"`
	// RetryMaxMs 单次退避上限（毫秒）
	RetryMaxMs int `yaml:"retry_max_ms"`
	// TimeoutSec 默认单次 Send 超时（秒）；可被 channels.*.timeout_sec 覆盖
	TimeoutSec int `yaml:"timeout_sec"`
	// DedupTTLSec 用户+活动+渠道去重 Redis 标记 TTL，默认 7 天
	DedupTTLSec int `yaml:"dedup_ttl_sec"`
	// Channels 各渠道发送器：mode=stub|http；可覆盖重试/退避/超时
	Channels map[string]ChannelSenderConfig `yaml:"channels"`
}

type ChannelSenderConfig struct {
	Mode       string `yaml:"mode"` // stub（默认）| http
	URL        string `yaml:"url"`
	TimeoutSec int    `yaml:"timeout_sec"`
	// MaxRetry 覆盖全局 max_retry；nil=沿用全局
	MaxRetry *int `yaml:"max_retry"`
	// RetryBackoff 覆盖全局退避曲线
	RetryBackoff string `yaml:"retry_backoff"`
	RetryBaseMs  int    `yaml:"retry_base_ms"`
	RetryMaxMs   int    `yaml:"retry_max_ms"`
}

type CampaignConfig struct {
	DefaultChannel string `yaml:"default_channel"`
}

type WebhookConfig struct {
	Enabled       bool     `yaml:"enabled"`
	DefaultURL    string   `yaml:"default_url"`
	TimeoutSec    int      `yaml:"timeout_sec"`
	AllowedHosts  []string `yaml:"allowed_hosts"`
	AllowHTTP     bool     `yaml:"allow_http"`
	SigningSecret string   `yaml:"signing_secret"`
	MaxConcurrent int      `yaml:"max_concurrent"`
}

// CallbackConfig 渠道回执签名校验。
type CallbackConfig struct {
	SignatureRequired bool   `yaml:"signature_required"`
	SignatureSecret   string `yaml:"signature_secret"`
	MaxSkewSec        int    `yaml:"max_skew_sec"`
}

// AudienceConfig 人群 Provider
type AudienceConfig struct {
	// DemoEnabled 是否注册 DemoProvider（仅支持 DemoScenes，默认 true）
	DemoEnabled *bool `yaml:"demo_enabled"`
	// DemoScenes Demo 支持的 biz_scene；默认 demo,dev
	DemoScenes []string `yaml:"demo_scenes"`
	// HTTP 真实人群：按 biz_scene 路由到 HTTP 圈人服务
	HTTP AudienceHTTPConfig `yaml:"http"`
}

type AudienceHTTPConfig struct {
	Enabled    bool     `yaml:"enabled"`
	URL        string   `yaml:"url"`    // POST AudienceQuery → AudiencePage
	Scenes     []string `yaml:"scenes"` // 空=enabled 时承接所有非 demo 场景
	TimeoutSec int      `yaml:"timeout_sec"`
}

// FreqConfig 用户/渠道/场景频控与免打扰
type FreqConfig struct {
	Enabled bool `yaml:"enabled"`
	// 用户维度
	UserLimit     int `yaml:"user_limit"`
	UserWindowSec int `yaml:"user_window_sec"`
	// 用户+渠道
	UserChannelLimit     int `yaml:"user_channel_limit"`
	UserChannelWindowSec int `yaml:"user_channel_window_sec"`
	// 场景维度
	SceneLimit     int `yaml:"scene_limit"`
	SceneWindowSec int `yaml:"scene_window_sec"`
	// MarketingLimit 跨活动营销频次上限：同一用户在窗口内收到的全部营销消息总数。
	// 与 UserLimit 的区别是它只统计 normal 优先级，事务通知不占额度。
	MarketingLimit     int              `yaml:"marketing_limit"`
	MarketingWindowSec int              `yaml:"marketing_window_sec"`
	QuietHours         QuietHoursConfig `yaml:"quiet_hours"`
}

// PreferenceConfig 用户偏好中心
type PreferenceConfig struct {
	// Enabled 关闭后发送链路完全跳过偏好查询。
	// 用指针以区分「未配置」与「显式关闭」：存量配置文件没有这一节，
	// 若按 bool 零值处理会导致升级后偏好静默失效，已退订用户照发。
	Enabled *bool `yaml:"enabled"`
	// CacheTTLSec 发送链路偏好缓存 TTL；跨进程失效靠它兜底，pusher 建议 15~30s
	CacheTTLSec int `yaml:"cache_ttl_sec"`
	// CacheMaxEntries 缓存条目上限，防止长跑进程内存无界增长
	CacheMaxEntries int `yaml:"cache_max_entries"`
	// FailOpen 偏好库不可用时是否放行营销消息。默认 false：
	// 宁可漏发也不能给已退订用户发出去，那是合规事故。
	FailOpen bool `yaml:"fail_open"`
}

// IsEnabled 未配置时视为启用
func (c PreferenceConfig) IsEnabled() bool { return c.Enabled == nil || *c.Enabled }

// CacheTTL 偏好缓存 TTL
func (c PreferenceConfig) CacheTTL() time.Duration {
	if c.CacheTTLSec <= 0 {
		return 60 * time.Second
	}
	return time.Duration(c.CacheTTLSec) * time.Second
}

type QuietHoursConfig struct {
	Enabled bool     `yaml:"enabled"`
	Start   string   `yaml:"start"`  // "22:00"
	End     string   `yaml:"end"`    // "08:00"
	Scenes  []string `yaml:"scenes"` // 仅这些场景生效；空=全部
}

// ComplianceConfig 黑名单 / 退订
type ComplianceConfig struct {
	// BlacklistKey Redis SET，成员为 user_id
	BlacklistKey string `yaml:"blacklist_key"`
	// UnsubscribeKeyPrefix Redis SET 前缀，完整 key = prefix + channel
	UnsubscribeKeyPrefix string `yaml:"unsubscribe_key_prefix"`
}

// ChannelQuotaConfig 按渠道 × 优先级的发送配额与上游反压
type ChannelQuotaConfig struct {
	Enabled        bool                    `yaml:"enabled"`
	Distributed    bool                    `yaml:"distributed"`
	RedisKeyPrefix string                  `yaml:"redis_key_prefix"`
	WaitTimeoutMs  int                     `yaml:"wait_timeout_ms"`
	GlobalQPS      int                     `yaml:"global_qps"`
	Backpressure   QuotaBackpressureConfig `yaml:"backpressure"`
	Adaptive       QuotaAdaptiveConfig     `yaml:"adaptive"`
	// OverCapacityAction 拆分后发现超容量：warn | pause（仅 enforce 渠道）
	OverCapacityAction string                       `yaml:"over_capacity_action"`
	Channels           map[string]ChannelQuotaEntry `yaml:"channels"`
}

type QuotaBackpressureConfig struct {
	Enabled                  bool    `yaml:"enabled"`
	HighWatermark            float64 `yaml:"high_watermark"`
	LowWatermark             float64 `yaml:"low_watermark"`
	SlowdownFactor           float64 `yaml:"slowdown_factor"`
	DefaultPaceWhenThrottled int     `yaml:"default_pace_when_throttled"`
	SustainSec               int     `yaml:"sustain_sec"`
}

type QuotaAdaptiveConfig struct {
	Enabled            bool    `yaml:"enabled"`
	VendorThrottleRate float64 `yaml:"vendor_throttle_rate"` // 保留字段，当前遇 429 即 shrink
	ShrinkFactor       float64 `yaml:"shrink_factor"`
	RecoverIntervalSec int     `yaml:"recover_interval_sec"`
}

type ChannelQuotaEntry struct {
	QPS                 int     `yaml:"qps"`
	Burst               int     `yaml:"burst"`
	HighReserveRatio    float64 `yaml:"high_reserve_ratio"`
	Admission           string  `yaml:"admission"` // soft | enforce
	TargetFinishMinutes int     `yaml:"target_finish_minutes"`
}

func (c ChannelQuotaConfig) WaitTimeout() time.Duration {
	if c.WaitTimeoutMs <= 0 {
		return 2 * time.Second
	}
	return time.Duration(c.WaitTimeoutMs) * time.Millisecond
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyEnvOverrides()
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyEnvOverrides() {
	override := func(name string, dst *string) {
		if value, ok := os.LookupEnv(name); ok && value != "" {
			*dst = value
		}
	}
	override("STARLINK_SESSION_SECRET", &c.Auth.SessionSecret)
	override("STARLINK_MYSQL_DSN", &c.MySQL.DSN)
	override("STARLINK_REDIS_PASSWORD", &c.Redis.Password)
	override("STARLINK_CALLBACK_SECRET", &c.Callback.SignatureSecret)
	override("STARLINK_WEBHOOK_SIGNING_SECRET", &c.Webhook.SigningSecret)
	override("STARLINK_ROCKETMQ_ACCESS_KEY", &c.MQ.RocketMQ.AccessKey)
	override("STARLINK_ROCKETMQ_SECRET_KEY", &c.MQ.RocketMQ.SecretKey)
}

func (c *Config) applyDefaults() {
	if c.Server.Addr == "" {
		c.Server.Addr = ":8080"
	}
	if len(c.Server.AllowedOrigins) == 0 {
		c.Server.AllowedOrigins = []string{"http://localhost:3000", "http://localhost:5173"}
	}
	if c.Server.MaxBodyBytes <= 0 {
		c.Server.MaxBodyBytes = 2 << 20
	}
	if c.Server.ReadHeaderTimeoutSec <= 0 {
		c.Server.ReadHeaderTimeoutSec = 5
	}
	if c.Server.ReadTimeoutSec <= 0 {
		c.Server.ReadTimeoutSec = 15
	}
	if c.Server.WriteTimeoutSec <= 0 {
		c.Server.WriteTimeoutSec = 30
	}
	if c.Server.IdleTimeoutSec <= 0 {
		c.Server.IdleTimeoutSec = 60
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Log.Format == "" {
		c.Log.Format = "text"
	}
	// auth：未配置 cookie/ttl/secret 时补默认；users 空且 enabled 时不自动造账号（需 YAML 显式配置）
	if c.Auth.CookieName == "" {
		c.Auth.CookieName = "starlink_session"
	}
	if c.Auth.TTLHours <= 0 {
		c.Auth.TTLHours = 24
	}
	if c.Auth.SessionSecret == "" {
		c.Auth.SessionSecret = "change-me-in-production"
	}
	if c.Scheduler.BatchSize <= 0 {
		c.Scheduler.BatchSize = 200
	}
	if c.Scheduler.WorkerConcurrency <= 0 {
		c.Scheduler.WorkerConcurrency = 8
	}
	if c.Scheduler.SplitConcurrency <= 0 {
		c.Scheduler.SplitConcurrency = 2
	}
	if c.Scheduler.PollIntervalMs <= 0 {
		c.Scheduler.PollIntervalMs = 500
	}
	if c.Scheduler.ClaimTimeoutSec <= 0 {
		c.Scheduler.ClaimTimeoutSec = 60
	}
	if c.Scheduler.SplitLeaseSec <= 0 {
		c.Scheduler.SplitLeaseSec = 90
	}
	if c.Pusher.WorkerConcurrency <= 0 {
		c.Pusher.WorkerConcurrency = 16
	}
	if c.Pusher.HighWorkerConcurrency <= 0 {
		c.Pusher.HighWorkerConcurrency = 32
	}
	if c.Pusher.RateLimitQPS <= 0 {
		c.Pusher.RateLimitQPS = 500
	}
	if c.Pusher.MaxRetry <= 0 {
		c.Pusher.MaxRetry = 3
	}
	if c.Pusher.RetryBackoff == "" {
		c.Pusher.RetryBackoff = RetryBackoffExponential
	}
	if c.Pusher.RetryBaseMs <= 0 {
		c.Pusher.RetryBaseMs = 50
	}
	if c.Pusher.RetryMaxMs <= 0 {
		c.Pusher.RetryMaxMs = 5000
	}
	if c.Pusher.TimeoutSec <= 0 {
		c.Pusher.TimeoutSec = 10
	}
	if c.Pusher.DedupTTLSec <= 0 {
		c.Pusher.DedupTTLSec = 7 * 24 * 3600
	}

	// 优先级队列默认值；兼容旧 stream/group
	if c.MQ.Driver == "" {
		c.MQ.Driver = "redis_stream"
	}
	if c.MQ.Normal.TopicOrStream() == "" {
		if c.MQ.Stream != "" {
			c.MQ.Normal.Stream = c.MQ.Stream
		} else {
			c.MQ.Normal.Stream = "starlink:push:normal"
		}
	}
	if c.MQ.Normal.Group == "" {
		if c.MQ.Group != "" {
			c.MQ.Normal.Group = c.MQ.Group
		} else {
			c.MQ.Normal.Group = "pushers-normal"
		}
	}
	if c.MQ.High.TopicOrStream() == "" {
		c.MQ.High.Stream = "starlink:push:high"
	}
	if c.MQ.High.Group == "" {
		c.MQ.High.Group = "pushers-high"
	}
	// 回填顶层字段，便于日志/兼容读取
	c.MQ.Stream = c.MQ.Normal.TopicOrStream()
	c.MQ.Group = c.MQ.Normal.Group

	if len(c.MQ.HighBizScenes) == 0 {
		c.MQ.HighBizScenes = []string{"txn", "otp", "security", "payment", "transactional"}
	}
	if c.MQ.Memory.BufferSize <= 0 {
		c.MQ.Memory.BufferSize = 4096
	}
	if c.MQ.RocketMQ.Retry <= 0 {
		c.MQ.RocketMQ.Retry = 2
	}
	if c.MQ.RedisStream.ClaimMinIdleMs <= 0 {
		c.MQ.RedisStream.ClaimMinIdleMs = 30000
	}
	if c.MQ.RedisStream.ClaimBatch <= 0 {
		c.MQ.RedisStream.ClaimBatch = 16
	}
	if c.MQ.RedisStream.MaxDelivery <= 0 {
		c.MQ.RedisStream.MaxDelivery = 5
	}
	if c.MQ.RedisStream.DLQSuffix == "" {
		c.MQ.RedisStream.DLQSuffix = ":dlq"
	}
	// 容量：未配置（0）给默认上限；显式 -1 表示不限制（由 OptionsFromConfig 译为关闭）
	if c.MQ.RedisStream.MaxLen == 0 {
		c.MQ.RedisStream.MaxLen = 100000
	}
	if c.MQ.RedisStream.MaxLenApprox == nil {
		v := true
		c.MQ.RedisStream.MaxLenApprox = &v
	}
	// 定期 trim：未配置（0）默认 60s；显式 -1 表示关闭定期裁剪
	if c.MQ.RedisStream.TrimIntervalSec == 0 {
		c.MQ.RedisStream.TrimIntervalSec = 60
	}
	if c.MQ.RedisStream.AckXDel == nil {
		v := true
		c.MQ.RedisStream.AckXDel = &v
	}

	if c.Webhook.TimeoutSec <= 0 {
		c.Webhook.TimeoutSec = 5
	}
	if c.Webhook.MaxConcurrent <= 0 {
		c.Webhook.MaxConcurrent = 32
	}
	if c.Callback.MaxSkewSec <= 0 {
		c.Callback.MaxSkewSec = 300
	}
	if c.Campaign.DefaultChannel == "" {
		c.Campaign.DefaultChannel = "inbox"
	}
	if c.Audience.DemoEnabled == nil {
		v := true
		c.Audience.DemoEnabled = &v
	}
	if len(c.Audience.DemoScenes) == 0 {
		c.Audience.DemoScenes = []string{"demo", "dev"}
	}
	if c.Audience.HTTP.TimeoutSec <= 0 {
		c.Audience.HTTP.TimeoutSec = 10
	}
	if c.Freq.UserLimit <= 0 {
		c.Freq.UserLimit = 20
	}
	if c.Freq.UserWindowSec <= 0 {
		c.Freq.UserWindowSec = 86400
	}
	if c.Freq.UserChannelLimit <= 0 {
		c.Freq.UserChannelLimit = 10
	}
	if c.Freq.UserChannelWindowSec <= 0 {
		c.Freq.UserChannelWindowSec = 86400
	}
	if c.Freq.SceneLimit <= 0 {
		c.Freq.SceneLimit = 100000
	}
	if c.Freq.SceneWindowSec <= 0 {
		c.Freq.SceneWindowSec = 86400
	}
	// marketing_limit 默认 0 = 不启用跨活动上限，避免升级后突然开始拦截存量投放
	if c.Freq.MarketingWindowSec <= 0 {
		c.Freq.MarketingWindowSec = 86400
	}
	if c.Preference.CacheTTLSec <= 0 {
		c.Preference.CacheTTLSec = 60
	}
	if c.Preference.CacheMaxEntries <= 0 {
		c.Preference.CacheMaxEntries = 50000
	}
	if c.Compliance.BlacklistKey == "" {
		c.Compliance.BlacklistKey = "starlink:blacklist"
	}
	if c.Compliance.UnsubscribeKeyPrefix == "" {
		c.Compliance.UnsubscribeKeyPrefix = "starlink:unsub:"
	}

	// channel_quota 默认值
	if c.ChannelQuota.RedisKeyPrefix == "" {
		c.ChannelQuota.RedisKeyPrefix = "starlink:quota:"
	}
	if c.ChannelQuota.WaitTimeoutMs <= 0 {
		c.ChannelQuota.WaitTimeoutMs = 2000
	}
	if c.ChannelQuota.GlobalQPS <= 0 {
		c.ChannelQuota.GlobalQPS = c.Pusher.RateLimitQPS
	}
	if c.ChannelQuota.OverCapacityAction == "" {
		c.ChannelQuota.OverCapacityAction = "warn"
	}
	bp := &c.ChannelQuota.Backpressure
	if bp.HighWatermark <= 0 {
		bp.HighWatermark = 0.80
	}
	if bp.LowWatermark <= 0 {
		bp.LowWatermark = 0.50
	}
	if bp.SlowdownFactor <= 0 || bp.SlowdownFactor > 1 {
		bp.SlowdownFactor = 0.3
	}
	if bp.DefaultPaceWhenThrottled <= 0 {
		bp.DefaultPaceWhenThrottled = 50
	}
	if bp.SustainSec <= 0 {
		bp.SustainSec = 15
	}
	ad := &c.ChannelQuota.Adaptive
	if ad.ShrinkFactor <= 0 || ad.ShrinkFactor > 1 {
		ad.ShrinkFactor = 0.5
	}
	if ad.RecoverIntervalSec <= 0 {
		ad.RecoverIntervalSec = 60
	}
	if ad.VendorThrottleRate <= 0 {
		ad.VendorThrottleRate = 0.05
	}
	for name, e := range c.ChannelQuota.Channels {
		if e.QPS <= 0 {
			e.QPS = 100
		}
		if e.Burst <= 0 {
			e.Burst = e.QPS
		}
		if e.HighReserveRatio < 0 {
			e.HighReserveRatio = 0
		}
		if e.HighReserveRatio > 1 {
			e.HighReserveRatio = 1
		}
		if e.Admission == "" {
			e.Admission = "soft"
		}
		if e.TargetFinishMinutes <= 0 {
			e.TargetFinishMinutes = 60
		}
		c.ChannelQuota.Channels[name] = e
	}
}

func (c *Config) validate() error {
	mode := strings.ToLower(strings.TrimSpace(c.Server.Mode))
	if mode != "" && mode != "debug" && mode != "release" && mode != "test" {
		return fmt.Errorf("invalid server.mode %q", c.Server.Mode)
	}
	if c.Auth.Enabled && len(c.Auth.SessionSecret) < 32 {
		return fmt.Errorf("auth.session_secret must be at least 32 bytes when auth is enabled")
	}
	if c.MySQL.DSN == "" {
		return fmt.Errorf("mysql.dsn is required")
	}
	if c.Redis.Addr == "" {
		return fmt.Errorf("redis.addr is required")
	}
	if c.Callback.SignatureRequired && len(c.Callback.SignatureSecret) < 32 {
		return fmt.Errorf("callback.signature_secret must be at least 32 bytes when signatures are required")
	}
	return nil
}
