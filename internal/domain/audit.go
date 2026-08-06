package domain

import "time"

// AuditLog 用户写操作审计
type AuditLog struct {
	ID           uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Operator     string    `gorm:"size:64;index;not null" json:"operator"`
	Action       string    `gorm:"size:64;index;not null" json:"action"`
	ResourceType string    `gorm:"size:32;index" json:"resource_type,omitempty"`
	ResourceID   string    `gorm:"size:64;index" json:"resource_id,omitempty"`
	Method       string    `gorm:"size:16" json:"method"`
	Path         string    `gorm:"size:256" json:"path"`
	IP           string    `gorm:"size:64" json:"ip,omitempty"`
	Detail       string    `gorm:"type:text" json:"detail,omitempty"`
	Success      bool      `gorm:"not null;default:true;index" json:"success"`
	CreatedAt    time.Time `gorm:"index" json:"created_at"`
}

func (AuditLog) TableName() string { return "audit_logs" }

// ListAuditLogQuery 审计日志列表筛选
type ListAuditLogQuery struct {
	Operator  string `form:"operator"`
	Action    string `form:"action"`
	Success   *bool  `form:"success"`
	Since     string `form:"since"` // RFC3339
	Until     string `form:"until"`
	Page      int    `form:"page"`
	PageSize  int    `form:"page_size"`
}
