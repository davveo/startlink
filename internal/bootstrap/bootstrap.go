package bootstrap

import (
	"fmt"
	"log/slog"

	"github.com/starlink/push/internal/adapter/audience"
	"github.com/starlink/push/internal/adapter/channel"
	"github.com/starlink/push/internal/adapter/mq"
	redisx "github.com/starlink/push/internal/adapter/redis"
	"github.com/starlink/push/internal/adapter/repo"
	"github.com/starlink/push/internal/adapter/webhook"
	"github.com/starlink/push/internal/config"
	"github.com/starlink/push/internal/port"
	"gorm.io/gorm"
)

// Infra 公共基础设施装配
type Infra struct {
	Cfg       *config.Config
	DB        *gorm.DB
	Redis     *redisx.Client
	MQ        port.PriorityBroker // 可插拔：redis_stream / rocketmq / memory / 自定义
	Tasks     *repo.TaskRepo
	Push      *repo.PushRepo
	AggCache  *redisx.Aggregator
	Limiter   port.ChannelLimiter
	Channels  *channel.Registry
	Audience  *audience.Registry
	Webhook   *webhook.Client
	Templates *repo.TemplateRepo
}

func NewInfra(cfgPath string) (*Infra, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}

	db, err := repo.NewDB(cfg.MySQL.DSN, cfg.MySQL.MaxIdle, cfg.MySQL.MaxOpen)
	if err != nil {
		return nil, err
	}
	if err := repo.AutoMigrate(db); err != nil {
		return nil, fmt.Errorf("auto migrate: %w", err)
	}

	rdb := redisx.New(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)

	// RocketMQ：若编译启用 -tags rocketmq，InitApacheRocketTransport 会注入 Transport
	if cfg.MQ.Driver == "rocketmq" {
		mq.TryInitRocketTransport(cfg.MQ.RocketMQ)
	}

	broker, err := mq.Open(mq.Deps{
		Cfg:   cfg.MQ,
		Redis: rdb.RDB(),
	})
	if err != nil {
		return nil, fmt.Errorf("init mq: %w", err)
	}
	slog.Info("mq ready",
		"driver", broker.Driver(),
		"high", cfg.MQ.High.TopicOrStream(),
		"normal", cfg.MQ.Normal.TopicOrStream(),
		"drivers", mq.Drivers(),
	)

	chReg := channel.NewRegistry()
	channel.RegisterDefaults(chReg)
	channel.RegisterFromConfig(chReg, cfg.Pusher.Channels)

	audReg := audience.NewRegistry()
	// 真实 HTTP Provider 优先注册，避免被 Demo 截胡
	if cfg.Audience.HTTP.Enabled && cfg.Audience.HTTP.URL != "" {
		audReg.Register(audience.NewHTTPProvider(
			cfg.Audience.HTTP.URL,
			cfg.Audience.HTTP.Scenes,
			cfg.Audience.HTTP.TimeoutSec,
		))
		slog.Info("audience http provider registered", "url", cfg.Audience.HTTP.URL, "scenes", cfg.Audience.HTTP.Scenes)
	}
	if cfg.Audience.DemoEnabled == nil || *cfg.Audience.DemoEnabled {
		audReg.Register(audience.NewDemoProvider(cfg.Audience.DemoScenes))
		slog.Info("audience demo provider registered", "scenes", cfg.Audience.DemoScenes)
	}
	audReg.RegisterFilter(audience.NewComplianceFilter())
	audReg.RegisterFilter(audience.NewBlacklistFilter(rdb.RDB(), cfg.Compliance.BlacklistKey))
	audReg.RegisterFilter(audience.NewUnsubscribeFilter(rdb.RDB(), cfg.Compliance.UnsubscribeKeyPrefix))
	audReg.RegisterFilter(audience.NewABSampleFilter())

	wh := webhook.New(cfg.Webhook.DefaultURL, cfg.Webhook.TimeoutSec, cfg.Webhook.Enabled)

	limiter := redisx.NewChannelQuotaLimiter(rdb.RDB(), cfg.ChannelQuota, cfg.Pusher.RateLimitQPS)
	if cfg.ChannelQuota.Enabled {
		slog.Info("channel quota enabled",
			"distributed", cfg.ChannelQuota.Distributed,
			"global_qps", cfg.ChannelQuota.GlobalQPS,
			"channels", len(cfg.ChannelQuota.Channels),
		)
	}

	return &Infra{
		Cfg:       cfg,
		DB:        db,
		Redis:     rdb,
		MQ:        broker,
		Tasks:     repo.NewTaskRepo(db),
		Push:      repo.NewPushRepo(db),
		AggCache:  redisx.NewAggregator(rdb),
		Limiter:   limiter,
		Channels:  chReg,
		Audience:  audReg,
		Webhook:   wh,
		Templates: repo.NewTemplateRepo(db),
	}, nil
}
