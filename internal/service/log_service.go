package service

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/repository"
	"github.com/epg-sync/epgsync/pkg/logger"
)

const (
	logBatchSize     = 200
	logFlushInterval = time.Second
	logCleanupEvery  = time.Hour

	defaultLogPageSize = 100
	maxLogPageSize     = 500
)

// LogService 把程序日志异步写入数据库，供管理后台查看。
// 日志先进入 logger.Hub 的缓冲区，这里攒批写入，不阻塞打日志的一方。
type LogService struct {
	repo      repository.LogRepository
	hub       *logger.Hub
	retention time.Duration

	stop chan struct{}
	done sync.WaitGroup
}

func NewLogService(repo repository.LogRepository, hub *logger.Hub, retentionDays int) *LogService {
	return &LogService{
		repo:      repo,
		hub:       hub,
		retention: time.Duration(retentionDays) * 24 * time.Hour,
		stop:      make(chan struct{}),
	}
}

// Start 开始消费日志。启动前打的日志已在 Hub 的缓冲区里，会一并写入
func (s *LogService) Start() {
	s.done.Add(1)
	go s.run()
}

// Stop 写完缓冲区里剩下的日志后返回，需在关闭数据库之前调用
func (s *LogService) Stop() {
	close(s.stop)
	s.done.Wait()
}

func (s *LogService) run() {
	defer s.done.Done()

	flush := time.NewTicker(logFlushInterval)
	defer flush.Stop()
	cleanup := time.NewTicker(logCleanupEvery)
	defer cleanup.Stop()

	s.cleanup()

	batch := make([]*model.Log, 0, logBatchSize)
	var reportedDropped int64

	write := func() {
		if dropped := s.hub.Dropped(); dropped > reportedDropped {
			// 走 logger 会再进 Hub；这条提示直接放进本批
			batch = append(batch, &model.Log{
				Time:    time.Now(),
				Level:   int8(logger.WarnLevel),
				Logger:  "logs",
				Message: "Log buffer full, some entries were not saved to the database (still in stdout)",
				Fields:  map[string]any{"dropped": dropped - reportedDropped},
			})
			reportedDropped = dropped
		}
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.repo.CreateBatch(ctx, batch); err != nil {
			// 不能用 logger，否则这条错误又会进入 Hub
			fmt.Fprintf(os.Stderr, "failed to save %d log entries to database: %v\n", len(batch), err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case r := <-s.hub.Records():
			batch = append(batch, toLogModel(r))
			if len(batch) >= logBatchSize {
				write()
			}
		case <-flush.C:
			write()
		case <-cleanup.C:
			s.cleanup()
		case <-s.stop:
			for {
				select {
				case r := <-s.hub.Records():
					batch = append(batch, toLogModel(r))
					if len(batch) >= logBatchSize {
						write()
					}
				default:
					write()
					return
				}
			}
		}
	}
}

func (s *LogService) cleanup() {
	if s.retention <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	deleted, err := s.repo.DeleteBefore(ctx, time.Now().Add(-s.retention))
	if err != nil {
		logger.Error("Failed to delete old logs", logger.Err(err))
		return
	}
	if deleted > 0 {
		logger.Info("Deleted old logs", logger.Int64("deleted", deleted))
	}
}

func (s *LogService) List(ctx context.Context, filter model.LogFilter) ([]*model.Log, error) {
	switch {
	case filter.Limit <= 0:
		filter.Limit = defaultLogPageSize
	case filter.Limit > maxLogPageSize:
		filter.Limit = maxLogPageSize
	}
	return s.repo.List(ctx, filter)
}

func toLogModel(r logger.Record) *model.Log {
	return &model.Log{
		Time:    r.Time,
		Level:   int8(r.Level),
		Logger:  r.Logger,
		Caller:  r.Caller,
		Message: r.Message,
		Fields:  r.Fields,
		Stack:   r.Stack,
	}
}
