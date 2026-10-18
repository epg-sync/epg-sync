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

	if err := seedDefaultData(db, app.cfg.Server.AdminPassword); err != nil {
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

// seedDefaultData 在没有任何用户时创建管理员 admin。
// 密码取 ADMIN_PASSWORD，未设置时随机生成并打印到 stdout（docker logs 能看到）。
func seedDefaultData(db *gorm.DB, initialPassword string) error {
	var count int64
	if err := db.Model(&model.User{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	password := initialPassword
	if password == "" {
		var err error
		if password, err = utils.GenerateRandomString(14); err != nil {
			return err
		}
	}
	hashedPassword, err := utils.HashPassword(password)
	if err != nil {
		return err
	}
	admin := model.User{
		Username: adminUsername,
		Password: hashedPassword,
		Role:     "admin",
		IsActive: 1,
	}
	if err := db.Create(&admin).Error; err != nil {
		return err
	}

	if initialPassword != "" {
		logger.Info("Default admin created with password from ADMIN_PASSWORD", logger.String("username", adminUsername))
		return nil
	}
	printCredentials("Default admin created", adminUsername, password)
	logger.Info("Default admin created with a random password, see the startup output", logger.String("username", adminUsername))
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
