package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server       ServerConfig       `yaml:"server"`
	MySQL        MySQLConfig        `yaml:"mysql"`
	Redis        RedisConfig        `yaml:"redis"`
	MQ           MQConfig           `yaml:"mq"`
	Scheduler    SchedulerConfig    `yaml:"scheduler"`
	Pusher       PusherConfig       `yaml:"pusher"`
	Campaign     CampaignConfig     `yaml:"campaign"`
	Webhook      WebhookConfig      `yaml:"webhook"`
	Audience     AudienceConfig     `yaml:"audience"`
	Freq         FreqConfig         `yaml:"freq"`
	Compliance   ComplianceConfig   `yaml:"compliance"`
	ChannelQuota ChannelQuotaConfig `yaml:"channel_quota"`
}

type ServerConfig struct {
	Addr string `yaml:"addr"`
	Mode string `yaml:"mode"`
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
	MaxRetry              int `yaml:"max_retry"`
	// DedupTTLSec 用户+活动+渠道去重 Redis 标记 TTL，默认 7 天
	DedupTTLSec int `yaml:"dedup_ttl_sec"`
	// Channels 各渠道发送器：mode=stub|http；http 时 POST JSON SendRequest → SendResult
	Channels map[string]ChannelSenderConfig `yaml:"channels"`
}

type ChannelSenderConfig struct {
	Mode       string `yaml:"mode"` // stub（默认）| http
	URL        string `yaml:"url"`
	TimeoutSec int    `yaml:"timeout_sec"`
}

type CampaignConfig struct {
	DefaultChannel string `yaml:"default_channel"`
}

type WebhookConfig struct {
	Enabled    bool   `yaml:"enabled"`
	DefaultURL string `yaml:"default_url"`
	TimeoutSec int    `yaml:"timeout_sec"`
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
	SceneLimit     int              `yaml:"scene_limit"`
	SceneWindowSec int              `yaml:"scene_window_sec"`
	QuietHours     QuietHoursConfig `yaml:"quiet_hours"`
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
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults()
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Server.Addr == "" {
		c.Server.Addr = ":8080"
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
