package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/epg-sync/epgsync/internal/database"
	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/pkg/logger"
	"github.com/epg-sync/epgsync/pkg/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const settingJWTSecret = "jwt_secret"

func (app *App) initializeDatabase() error {
	ctx := context.Background()

	logger.Debug("Connecting to Postgres...")
	db, err := database.Open(ctx, app.cfg.Database)
	if err != nil {
		return err
	}
	app.db = db

	if err := database.Migrate(ctx, db); err != nil {
		return err
	}

	if err := seedDefaultData(db); err != nil {
		logger.Error("Failed to seed default data", logger.Err(err))
	}

	if app.cfg.Server.JWTSecret == "" {
		secret, err := loadOrCreateSetting(db, settingJWTSecret, func() (string, error) {
			logger.Info("No JWT secret configured, generating one and storing it in the database")
			return utils.GenerateRandomString(48)
		})
		if err != nil {
			return fmt.Errorf("load jwt secret: %w", err)
		}
		app.cfg.Server.JWTSecret = secret
	}

	logger.Debug("Database initialized successfully")

	return nil
}

func seedDefaultData(db *gorm.DB) error {
	var count int64
	db.Model(&model.User{}).Count(&count)

	if count == 0 {
		logger.Info("No users found, creating default admin user...")

		randomPassword, err := utils.GenerateRandomString(14)
		if err != nil {
			return err
		}
		hashedPassword, _ := utils.HashPassword(string(randomPassword))
		admin := model.User{
			Username: "admin",
			Password: hashedPassword,
			Role:     "admin",
			IsActive: 1,
		}
		if err := db.Create(&admin).Error; err != nil {
			return err
		}
		logger.Info(fmt.Sprintf("Default admin created. User: admin, Pass: %s", randomPassword))
	}
	return nil
}

// loadOrCreateSetting 读取 settings 表中的值，不存在时用 generate 生成并保存
func loadOrCreateSetting(db *gorm.DB, key string, generate func() (string, error)) (string, error) {
	var setting model.Setting
	err := db.Where("key = ?", key).First(&setting).Error
	if err == nil {
		return setting.Value, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}

	value, err := generate()
	if err != nil {
		return "", err
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&model.Setting{Key: key, Value: value}).Error; err != nil {
		return "", err
	}

	// 并发启动时可能被别的实例先写入，以库里的值为准
	if err := db.Where("key = ?", key).First(&setting).Error; err != nil {
		return "", err
	}
	return setting.Value, nil
}
