package dto

type CreateChannelRequest struct {
	ChannelID   string `json:"channel_id" binding:"required"`
	DisplayName string `json:"display_name" binding:"required"`
	Category    string `json:"category"`
	Area        string `json:"area"`
	LogoURL     string `json:"logo_url"`
	Regexp      string `json:"regexp"`
	Timezone    string `json:"timezone"`
}

type UpdateChannelRequest struct {
	ID       int64 `json:"id" binding:"required"`
	IsActive int   `json:"is_active"`
	CreateChannelRequest
}

type BatchCreateChannelRequest struct {
	Channels []CreateChannelRequest `json:"channels" binding:"required,dive,required"`
}

// CustomSourceRequest 用于创建和更新自定义数据源。省略的字段：创建时 mode 为 full、enabled 为 1、priority 为 100，
// 更新时保持不变。mode：full 全量覆盖、fill 增量补充；priority 只对增量源有意义，数字越小优先级越高
type CustomSourceRequest struct {
	Name     string  `json:"name" binding:"required"`
	URL      string  `json:"url" binding:"required"`
	Mode     *string `json:"mode" binding:"omitempty,oneof=full fill"`
	Enabled  *int    `json:"enabled" binding:"omitempty,oneof=0 1"`
	Priority *int    `json:"priority" binding:"omitempty,min=0,max=100000"`
}

type GetEPGRequest struct {
	ChannelID string `form:"channel_id" binding:"omitempty"`
	Date      string `form:"date" binding:"required,datetime=2006-01-02"`
	Timezone  string `form:"timezone" binding:"omitempty"`
	Page      int    `form:"page" binding:"omitempty,min=1"`
	PageSize  int    `form:"page_size" binding:"omitempty,min=1,max=100"`
}

type GetEPGByDateRangeRequest struct {
	ChannelID string `form:"channel_id" binding:"required"`
	StartDate string `form:"start_date" binding:"required,datetime=2006-01-02"`
	EndDate   string `form:"end_date" binding:"required,datetime=2006-01-02"`
}

type SyncEPGRequest struct {
	ChannelID string `form:"channel_id" binding:"required"`
	StartDate string `form:"start_date" binding:"required,datetime=2006-01-02"`
	EndDate   string `form:"end_date" binding:"required,datetime=2006-01-02"`
}

type DIYPProgramRequest struct {
	Ch   string `form:"ch" binding:"required"`
	Date string `form:"date" binding:"required,datetime=2006-01-02"`
}
