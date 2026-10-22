package model

import (
	"strconv"
	"time"
)

const (
	CustomSourceStatusRunning = "running"
	CustomSourceStatusSuccess = "success"
	CustomSourceStatusFailed  = "failed"
)

const (
	// CustomSourceModeFull 全量覆盖：文件里某频道某天有节目，这一天就以文件为准（覆盖内置源）。
	// 多个全量源冲突时先添加的（id 小的）优先
	CustomSourceModeFull = "full"
	// CustomSourceModeFill 增量补充：只补内置源和全量源当天都没有节目的「频道+天」，增量源之间按 priority
	CustomSourceModeFill = "fill"
)

const (
	// CustomSourceProviderPrefix 是全量源写入节目时 provider_id 的前缀，完整形式为 "custom:<id>"
	CustomSourceProviderPrefix = "custom:"
	// CustomSourceFillProviderPrefix 是增量源写入节目时 provider_id 的前缀，完整形式为 "custom-fill:<id>"。
	// 内置源判断「当天是否已有节目」时不计这类节目，抓到数据后会把它们换掉
	CustomSourceFillProviderPrefix = "custom-fill:"
)

// CustomSource 是用户配置的 XMLTV 文件源（.xml / .xml.gz）。
// Priority 只对增量源有意义，数字越小优先级越高
type CustomSource struct {
	ID           int64      `json:"id" gorm:"column:id;primaryKey;autoIncrement;not null"`
	Name         string     `json:"name" gorm:"column:name"`
	URL          string     `json:"url" gorm:"column:url"`
	Mode         string     `json:"mode" gorm:"column:mode"`
	Enabled      int        `json:"enabled" gorm:"column:enabled"`
	Priority     int        `json:"priority" gorm:"column:priority"`
	LastSyncAt   *time.Time `json:"last_sync_at" gorm:"column:last_sync_at"`
	LastStatus   string     `json:"last_status" gorm:"column:last_status"`
	LastError    string     `json:"last_error" gorm:"column:last_error"`
	LastMessage  string     `json:"last_message" gorm:"column:last_message"`
	ProgramCount int        `json:"program_count" gorm:"column:program_count"`
	CreatedAt    time.Time  `json:"created_at" gorm:"column:created_at"`
	UpdatedAt    time.Time  `json:"updated_at" gorm:"column:updated_at"`
}

func (s *CustomSource) IsFill() bool {
	return s.Mode == CustomSourceModeFill
}

// ProviderID 返回该源写入的节目所用的 provider_id，全量源和增量源的前缀不同
func (s *CustomSource) ProviderID() string {
	prefix := CustomSourceProviderPrefix
	if s.IsFill() {
		prefix = CustomSourceFillProviderPrefix
	}
	return prefix + strconv.FormatInt(s.ID, 10)
}

// ProviderDay 表示某个 provider 在某个频道的某一天（频道时区，格式 2006-01-02）有节目
type ProviderDay struct {
	ProviderID string `gorm:"column:provider_id"`
	Day        string `gorm:"column:day"`
}
