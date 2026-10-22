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
	ListByChannelAndDate(ctx context.Context, channel *model.Channel, date string) ([]*model.Program, error)
	ListByChannelIDAndDate(ctx context.Context, channelID string, date time.Time, page, pageSize int) ([]*model.Program, int64, error)
	ListByChannelIDAndTimeRange(ctx context.Context, channelID string, start, end time.Time) ([]*model.Program, error)
	ListAllByDateRange(ctx context.Context, start, end time.Time) ([]*model.Program, error)
	GetCurrentProgram(ctx context.Context, channelID string) (*model.Program, error)
	DeleteBefore(ctx context.Context, before time.Time) (int64, error)
	// ReplaceChannelDay 在一个事务中删除频道在 date 当天开始的全部节目（不分 provider），再写入 programs
	ReplaceChannelDay(ctx context.Context, channelID string, date time.Time, programs []*model.Program) error
	// ReplaceChannelRanges 在一个事务中删除频道与 ranges 中任一时间段有重叠的全部节目（不分 provider），
	// 以及 providerID 此前写入的、结束时间晚于 ownFrom 的节目（清理上次同步留下的旧数据），再写入 programs。
	// 时间段之外（包括时间段之间的空档）其它 provider 的节目原样保留
	ReplaceChannelRanges(ctx context.Context, channelID string, ranges []model.TimeRange, providerID string, ownFrom time.Time, programs []*model.Program) error
	DeleteByProviderID(ctx context.Context, providerID string) (int64, error)
	// DeleteByProviderOutsideChannels 删除 providerID 写入的、结束时间晚于 after、且频道不在 keep 中的节目
	DeleteByProviderOutsideChannels(ctx context.Context, providerID string, keep []string, after time.Time) (int64, error)
	Exists(ctx context.Context, channelID string, date time.Time) (bool, error)
}

type CustomSourceRepository interface {
	Repository

	Create(ctx context.Context, source *model.CustomSource) error
	List(ctx context.Context) ([]*model.CustomSource, error)
	GetByID(ctx context.Context, id int64) (*model.CustomSource, error)
	// Update 只更新 name / url / enabled / priority
	Update(ctx context.Context, source *model.CustomSource) error
	Delete(ctx context.Context, id int64) error
	SetRunning(ctx context.Context, id int64) error
	SetSyncResult(ctx context.Context, id int64, status, lastError string, programCount int) error
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
