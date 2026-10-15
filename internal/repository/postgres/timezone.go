package postgres

import (
	"context"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/pkg/logger"
	"gorm.io/gorm"
)

type timezoneRepo struct {
	*BaseRepository
}

func NewTimezoneRepository(db *gorm.DB) *timezoneRepo {
	return &timezoneRepo{BaseRepository: NewBaseRepository(db)}
}

func (r *timezoneRepo) GetAllTimezones(ctx context.Context) ([]string, error) {
	var timezones []string
	err := r.db.WithContext(ctx).Model(&model.Timezone{}).Pluck("tz_name", &timezones).Error
	if err != nil {
		logger.Error("Failed to get all timezones",
			logger.Err(err),
		)
		return nil, err
	}
	return timezones, nil
}

func (r *timezoneRepo) GetTimezoneByName(ctx context.Context, tzName string) (*model.Timezone, error) {
	var timezone model.Timezone
	err := r.db.WithContext(ctx).Where("tz_name = ?", tzName).First(&timezone).Error
	if err != nil {
		logger.Error("Failed to get timezone by name",
			logger.String("tz_name", tzName),
			logger.Err(err),
		)
		return nil, err
	}
	return &timezone, nil
}
