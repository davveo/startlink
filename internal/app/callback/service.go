package callback

import (
	"context"
	"errors"
	"time"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"github.com/starlink/push/pkg/errcode"
	"gorm.io/gorm"
)

// Service 回执接收：送达 / 点击 / 失败
type Service struct {
	pushRepo port.PushRepository
	tasks    port.TaskRepository
}

func NewService(pushRepo port.PushRepository, tasks port.TaskRepository) *Service {
	return &Service{pushRepo: pushRepo, tasks: tasks}
}

type ReceiptInput struct {
	ProviderID string              `json:"provider_id" binding:"required"`
	Provider   string              `json:"provider"` // 供应商标识；建议与 channel 一并传入
	Channel    domain.ChannelType  `json:"channel"`  // 渠道；与 provider 共同消歧
	Event      domain.ReceiptEvent `json:"event" binding:"required"`
	RawPayload string              `json:"raw_payload"`
}

func (s *Service) Handle(ctx context.Context, in ReceiptInput) error {
	provider := in.Provider
	if provider == "" && in.Channel != "" {
		provider = string(in.Channel)
	}
	rec, err := s.pushRepo.GetRecordByProviderRef(ctx, provider, in.Channel, in.ProviderID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errcode.NotFound
		}
		return err
	}

	var status domain.PushStatus
	switch in.Event {
	case domain.ReceiptDelivered:
		status = domain.PushStatusDelivered
	case domain.ReceiptClicked:
		status = domain.PushStatusClicked
	case domain.ReceiptFailed:
		status = domain.PushStatusFailed
	default:
		return errcode.InvalidParam
	}

	errMsg := ""
	if in.Event == domain.ReceiptFailed && in.RawPayload != "" {
		errMsg = in.RawPayload
		if len(errMsg) > 512 {
			errMsg = errMsg[:512]
		}
	}

	receipt := &domain.PushReceipt{
		PushRecordID: rec.ID,
		MainTaskID:   rec.MainTaskID,
		SubTaskID:    rec.SubTaskID,
		UserID:       rec.UserID,
		Channel:      rec.Channel,
		Event:        in.Event,
		RawPayload:   in.RawPayload,
		CreatedAt:    time.Now(),
	}
	if err := s.pushRepo.ApplyReceipt(ctx, rec.ID, status, errMsg, receipt); err != nil {
		return err
	}

	// 回执事务后再异步校准主任务用户成功/失败数
	if s.tasks != nil {
		oc, err := s.pushRepo.CountUserOutcomes(ctx, rec.MainTaskID)
		if err == nil && oc.HasRecords {
			_ = s.tasks.SyncMainUserCounts(ctx, rec.MainTaskID, oc.SuccessUsers, oc.FailUsers)
		}
	}
	return nil
}
