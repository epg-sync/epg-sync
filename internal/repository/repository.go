package repository

import (
	"context"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
)

type Repository interface {
	Ping(ctx context.Context) error
	Close() error
}

type ChannelRepository interface {
	Repository

	Create(ctx context.Context, channel *model.Channel) error
	CreateBatch(ctx context.Context, channels []*model.Channel) error
	GetAllChannels(ctx context.Context) ([]*model.Channel, error)
	GetByID(ctx context.Context, id string) (*model.Channel, error)
	GetByChannelName(ctx context.Context, channelName string) (*model.Channel, error)
	// MatchByRegexp 返回 regexp 能匹配 channelName 的全部频道（按 id 排序），以及各自匹配到的那段文字
	MatchByRegexp(ctx context.Context, channelName string) ([]*model.ChannelRegexpMatch, error)
	Update(ctx context.Context, channel *model.Channel) error
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) (int64, error)
}

type ProgramRepository interface {
	Repository

	Create(ctx context.Context, program *model.Program) error
	CreateBatch(ctx context.Context, programs []*model.Program) error
	// CreateBatchReplacingFill 写入内置源抓到的节目，并在同一个事务里删除增量源在这些频道上、
	// 与新节目时间范围重叠的节目。增量源只补内置源没有的内容，内置源一旦有数据就换掉它们
	CreateBatchReplacingFill(ctx context.Context, programs []*model.Program) error
	ListByChannelAndDate(ctx context.Context, channel *model.Channel, date string) ([]*model.Program, error)
	ListByChannelIDAndDate(ctx context.Context, channelID string, date time.Time, page, pageSize int) ([]*model.Program, int64, error)
	ListByChannelIDAndTimeRange(ctx context.Context, channelID string, start, end time.Time) ([]*model.Program, error)
	ListAllByDateRange(ctx context.Context, start, end time.Time) ([]*model.Program, error)
	GetCurrentProgram(ctx context.Context, channelID string) (*model.Program, error)
	DeleteBefore(ctx context.Context, before time.Time) (int64, error)
	// ReplaceChannelDay 在一个事务中删除频道在 date 当天开始的全部节目（不分 provider），再写入 programs
	ReplaceChannelDay(ctx context.Context, channelID string, date time.Time, programs []*model.Program) error
	// ReplaceChannelDays 在一个事务中：删除 providerID 此前在该频道写入的、结束时间晚于 ownFrom 的节目
	// （清理上次同步留下的旧数据）；删除开始时间落在 days 任一天内的全部节目（不分 provider）；再写入 programs。
	// days 之外其它 provider 的节目原样保留。days 为空时只做清理
	ReplaceChannelDays(ctx context.Context, channelID string, days []model.TimeRange, providerID string, ownFrom time.Time, programs []*model.Program) error
	// ListProviderDays 列出频道在 [from, to) 内各 provider 有节目的日期，日期按 timezone 计算，按开始时间归属
	ListProviderDays(ctx context.Context, channelID, timezone string, from, to time.Time) ([]*model.ProviderDay, error)
	DeleteByProviderID(ctx context.Context, providerID string) (int64, error)
	// DeleteByProviderOutsideChannels 删除 providerID 写入的、结束时间晚于 after、且频道不在 keep 中的节目
	DeleteByProviderOutsideChannels(ctx context.Context, providerID string, keep []string, after time.Time) (int64, error)
	// Exists 判断频道在 date 当天是否已有节目。增量源（custom-fill:）的节目不算，内置源会照常抓取并换掉它们
	Exists(ctx context.Context, channelID string, date time.Time) (bool, error)
}

type CustomSourceRepository interface {
	Repository

	Create(ctx context.Context, source *model.CustomSource) error
	List(ctx context.Context) ([]*model.CustomSource, error)
	GetByID(ctx context.Context, id int64) (*model.CustomSource, error)
	// Update 只更新 name / url / mode / enabled / priority
	Update(ctx context.Context, source *model.CustomSource) error
	Delete(ctx context.Context, id int64) error
	SetRunning(ctx context.Context, id int64) error
	SetSyncResult(ctx context.Context, id int64, status, lastError, message string, programCount int) error
	// ClearSyncResult 清空同步状态和节目数（停用后节目已被清除），保留上次同步时间
	ClearSyncResult(ctx context.Context, id int64) error
	// ResetRunning 把进程中途退出留下的 running 状态改为 failed
	ResetRunning(ctx context.Context) error
}

type ChannelMappingsRepository interface {
	Repository
	Create(ctx context.Context, mapping *model.ChannelMapping) error
	CreateBatch(ctx context.Context, mappings []*model.ChannelMapping) error
	GetByProviderChannelID(ctx context.Context, providerChannelID, providerID string) (*model.ChannelMapping, error)
	GetByCanonicalID(ctx context.Context, canonicalChannelID string) ([]*model.ChannelMapping, error)
	ListByCanonicalID(ctx context.Context, canonicalChannelID string) ([]*model.ChannelMapping, error)
	ListAllChannelMappings(ctx context.Context) ([]*model.ChannelMapping, error)
	ListByProviderID(ctx context.Context, providerID string) ([]*model.ChannelMapping, error)
	DeleteByProviderID(ctx context.Context, providerID string) error
}

type TimezoneRepository interface {
	Repository
	GetAllTimezones(ctx context.Context) ([]string, error)
	GetTimezoneByName(ctx context.Context, tzName string) (*model.Timezone, error)
}

type UserRepository interface {
	Repository
	Create(ctx context.Context, user *model.User) error
	GetByID(ctx context.Context, id int64) (*model.User, error)
	GetByUsername(ctx context.Context, username string) (*model.User, error)
	Update(ctx context.Context, user *model.User) error
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context, offset, limit int) ([]*model.User, int64, error)
}

type LogRepository interface {
	CreateBatch(ctx context.Context, logs []*model.Log) error
	List(ctx context.Context, filter model.LogFilter) ([]*model.Log, error)
	DeleteBefore(ctx context.Context, before time.Time) (int64, error)
}
