package app

import (
	"bytes"
	"testing"

	"github.com/epg-sync/epgsync/internal/database/dbtest"
	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/pkg/logger"
	"github.com/epg-sync/epgsync/pkg/utils"
)

func TestSeedAdminUsesConfiguredPassword(t *testing.T) {
	db := dbtest.New(t)

	var buf bytes.Buffer
	logger.SetDefault(logger.NewTestLogger(&buf))
	t.Cleanup(func() { logger.SetDefault(logger.NewNoOpLogger()) })

	if err := seedDefaultData(db, "from-env-123"); err != nil {
		t.Fatal(err)
	}

	var admin model.User
	if err := db.Where("username = ?", adminUsername).First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if !utils.CheckPassword(admin.Password, "from-env-123") {
		t.Error("admin password does not match ADMIN_PASSWORD")
	}
	if bytes.Contains(buf.Bytes(), []byte("from-env-123")) {
		t.Error("password leaked into logs")
	}

	// 已有用户时不再创建，也不会改密码
	if err := seedDefaultData(db, "other"); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&model.User{}).Count(&count)
	if count != 1 {
		t.Errorf("users = %d, want 1", count)
	}
}

// 随机密码只打印到 stdout，不能进入 logger（日志文件和管理后台的日志）
func TestSeedAdminRandomPasswordNotLogged(t *testing.T) {
	db := dbtest.New(t)

	var buf bytes.Buffer
	logger.SetDefault(logger.NewTestLogger(&buf))
	t.Cleanup(func() { logger.SetDefault(logger.NewNoOpLogger()) })

	if err := seedDefaultData(db, ""); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(buf.Bytes(), []byte("Password")) || bytes.Contains(buf.Bytes(), []byte("Pass:")) {
		t.Errorf("password leaked into logs: %s", buf.String())
	}
}
