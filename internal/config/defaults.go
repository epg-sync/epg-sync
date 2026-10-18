package config

import "github.com/epg-sync/epgsync/pkg/logger"

// Default 返回所有配置项的默认值，这是默认值唯一定义的地方。
// 配置文件只需写要修改的项，没写的项保持这里的值。
func Default(env Env) *AppConfig {
	mode := "release"
	if env == EnvDev {
		mode = "development"
	}

	return &AppConfig{
		Server: ServerConfig{
			Port:           5678,
			Mode:           mode,
			Timeout:        90,
			JWTExpireHours: 24,
		},
		Database: DatabaseConfig{
			// 连接信息的默认值与 Postgres 官方镜像一致；Name 为空时取 User，见 applyEnv
			Host:    "localhost",
			Port:    5432,
			User:    "postgres",
			SSLMode: "disable",

			MaxOpenConns: 10,
		},
		Logger: logger.Config{
			Level:            "info",
			Format:           "console",
			Output:           "stdout",
			Filename:         "logs/epgsync.log",
			MaxSize:          100,
			MaxBackups:       10,
			MaxAge:           30,
			EnableStackTrace: true,
			StackTraceLevel:  logger.ErrorLevel,
			RetentionDays:    30,
		},
	}
}
