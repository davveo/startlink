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
	Event      domain.ReceiptEvent `json:"event" binding:"required"`
	RawPayload string              `json:"raw_payload"`
}

func (s *Service) Handle(ctx context.Context, in ReceiptInput) error {
	rec, err := s.pushRepo.GetRecordByProviderID(ctx, in.ProviderID)
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

	if err := s.pushRepo.UpdateRecordStatus(ctx, rec.ID, status, "", errMsg); err != nil {
		return err
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
	if err := s.pushRepo.CreateReceipt(ctx, receipt); err != nil {
		return err
	}

	// 回执后按渠道口径校准主任务用户成功/失败数
	if s.tasks != nil {
		oc, err := s.pushRepo.CountUserOutcomes(ctx, rec.MainTaskID)
		if err == nil && oc.HasRecords {
			_ = s.tasks.SyncMainUserCounts(ctx, rec.MainTaskID, oc.SuccessUsers, oc.FailUsers)
		}
	}
	return nil
}
