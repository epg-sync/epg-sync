package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/epg-sync/epgsync/internal/config"
	"github.com/epg-sync/epgsync/internal/database"
	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/pkg/logger"
	"github.com/epg-sync/epgsync/pkg/utils"
	"gorm.io/gorm"
)

const adminUsername = "admin"

// ResetAdminPassword 重置管理员 admin 的密码，供命令行使用：
//
//	docker compose exec epg-sync /app/epg-sync reset-admin-password
//
// password 为空时随机生成；admin 不存在时创建
func ResetAdminPassword(cfg *config.AppConfig, password string) error {
	ctx := context.Background()
	db, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	if sqlDB, err := db.DB(); err == nil {
		defer sqlDB.Close()
	}
	if err := database.Migrate(ctx, db); err != nil {
		return err
	}

	if password == "" {
		if password, err = utils.GenerateRandomString(14); err != nil {
			return err
		}
	}
	hashed, err := utils.HashPassword(password)
	if err != nil {
		return err
	}

	var user model.User
	err = db.Where("username = ?", adminUsername).First(&user).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		user = model.User{Username: adminUsername, Password: hashed, Role: "admin", IsActive: 1}
		err = db.Create(&user).Error
	case err == nil:
		err = db.Model(&user).Updates(map[string]any{"password": hashed, "is_active": 1}).Error
	}
	if err != nil {
		return fmt.Errorf("reset admin password: %w", err)
	}

	logger.Info("Admin password reset from command line", logger.String("username", adminUsername))
	printCredentials("Admin password reset", adminUsername, password)
	return nil
}

// printCredentials 把账号密码直接打印到 stdout，不经过 logger：
// 明文密码不应该进入日志文件和数据库里的日志（管理后台可见），只在启动输出（docker logs）里出现一次。
func printCredentials(title, username, password string) {
	lines := []string{
		title,
		"",
		"  Username: " + username,
		"  Password: " + password,
		"",
		"Change it after logging in (账户设置 / Account settings).",
	}
	bar := strings.Repeat("=", 64)
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, bar)
	for _, l := range lines {
		fmt.Fprintln(os.Stdout, "  "+l)
	}
	fmt.Fprintln(os.Stdout, bar)
	fmt.Fprintln(os.Stdout)
}
