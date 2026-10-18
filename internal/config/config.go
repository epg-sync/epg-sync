package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/pkg/logger"
	"gopkg.in/yaml.v2"
)

// 配置文件目录，相对工作目录；Docker 中为 /app/config
const configDir = "config"

var (
	once     sync.Once
	instance *AppConfig
)

// Env 是运行环境，由环境变量 APP_ENV 指定
type Env string

const (
	EnvDev  Env = "dev"
	EnvProd Env = "prod"
)

// CurrentEnv 读取 APP_ENV，未设置时视为 prod
func CurrentEnv() (Env, error) {
	switch v := strings.ToLower(os.Getenv("APP_ENV")); v {
	case "", "prod", "production":
		return EnvProd, nil
	case "dev", "development":
		return EnvDev, nil
	default:
		return "", fmt.Errorf("unknown APP_ENV %q, expected dev or prod", v)
	}
}

// AppConfig 中每一项只有一个来源：
//   - 程序行为（yaml 标签）只来自配置文件
//   - 部署信息和机密（yaml:"-"）只来自环境变量，见 applyEnv
//
// 默认值统一定义在 defaults.go。
type AppConfig struct {
	Server    ServerConfig            `yaml:"server"`
	Database  DatabaseConfig          `yaml:"database"`
	Logger    logger.Config           `yaml:"logger"`
	Providers model.ProviderOverrides `yaml:"providers"`

	Env Env `yaml:"-"`
	// 实际读取到的配置文件，按加载顺序
	LoadedFiles []string `yaml:"-"`
}

type ServerConfig struct {
	Port           int    `yaml:"port"`
	Mode           string `yaml:"mode"`
	Timeout        int    `yaml:"timeout"`
	JWTExpireHours int    `yaml:"jwt_expire_hours"`

	// 环境变量 JWT_SECRET；为空时首次启动自动生成并保存到数据库
	JWTSecret string `yaml:"-"`
	// 环境变量 ADMIN_PASSWORD；只在首次启动创建管理员时使用，为空时随机生成
	AdminPassword string `yaml:"-"`
}

type DatabaseConfig struct {
	// 连接信息只来自环境变量，变量名与 Postgres 官方镜像相同，可以和 docker compose 共用一份 .env
	Host     string `yaml:"-"` // POSTGRES_HOST
	Port     int    `yaml:"-"` // POSTGRES_PORT
	User     string `yaml:"-"` // POSTGRES_USER
	Password string `yaml:"-"` // POSTGRES_PASSWORD
	Name     string `yaml:"-"` // POSTGRES_DB
	SSLMode  string `yaml:"-"` // POSTGRES_SSLMODE

	MaxOpenConns int  `yaml:"max_open_conns"`
	Debug        bool `yaml:"debug"`

	// 由上面的连接信息拼出的连接串
	URL string `yaml:"-"`
}

// Target 返回不含密码的连接目标，用于日志
func (d *DatabaseConfig) Target() string {
	return fmt.Sprintf("%s@%s/%s", d.User, net.JoinHostPort(d.Host, strconv.Itoa(d.Port)), d.Name)
}

// buildURL 用连接信息拼出连接串，密码中的特殊字符会被正确转义
func (d *DatabaseConfig) buildURL() string {
	user := url.User(d.User)
	if d.Password != "" {
		user = url.UserPassword(d.User, d.Password)
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     user,
		Host:     net.JoinHostPort(d.Host, strconv.Itoa(d.Port)),
		Path:     "/" + d.Name,
		RawQuery: url.Values{"sslmode": {d.SSLMode}}.Encode(),
	}
	return u.String()
}

// LoadConfig 按以下顺序加载，后者覆盖前者：
//
//	默认值 < config/config.yaml < config/config.<APP_ENV>.yaml < 环境变量
//
// 两个配置文件都是可选的。
func LoadConfig() (*AppConfig, error) {
	return loadFrom(configDir)
}

func loadFrom(dir string) (*AppConfig, error) {
	env, err := CurrentEnv()
	if err != nil {
		return nil, err
	}

	config := Default(env)
	config.Env = env

	for _, name := range []string{"config.yaml", "config." + string(env) + ".yaml"} {
		path := filepath.Join(dir, name)
		loaded, err := config.mergeFile(path)
		if err != nil {
			return nil, err
		}
		if loaded {
			config.LoadedFiles = append(config.LoadedFiles, path)
		}
	}

	if err := config.applyEnv(); err != nil {
		return nil, err
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return config, nil
}

// mergeFile 把配置文件中出现的项覆盖到当前配置上，文件不存在时跳过
func (c *AppConfig) mergeFile(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	// 严格模式：写错的键或已改为环境变量的项（如 database.host、server.jwt_secret）会直接报错
	if err := yaml.UnmarshalStrict(data, c); err != nil {
		return false, fmt.Errorf("failed to parse config file %s: %w (database connection and jwt secret are set via environment variables, see INSTALL.md)", path, err)
	}
	return true, nil
}

// applyEnv 读取部署信息和机密，它们只能通过环境变量设置
func (c *AppConfig) applyEnv() error {
	for name, dst := range map[string]*string{
		"POSTGRES_HOST":     &c.Database.Host,
		"POSTGRES_USER":     &c.Database.User,
		"POSTGRES_PASSWORD": &c.Database.Password,
		"POSTGRES_DB":       &c.Database.Name,
		"POSTGRES_SSLMODE":  &c.Database.SSLMode,
		"JWT_SECRET":        &c.Server.JWTSecret,
		"ADMIN_PASSWORD":    &c.Server.AdminPassword,
	} {
		if v := os.Getenv(name); v != "" {
			*dst = v
		}
	}

	if v := os.Getenv("POSTGRES_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid POSTGRES_PORT %q: %w", v, err)
		}
		c.Database.Port = port
	}

	// 与 Postgres 官方镜像一致：未指定库名时使用用户名
	if c.Database.Name == "" {
		c.Database.Name = c.Database.User
	}
	c.Database.URL = c.Database.buildURL()
	return nil
}

func GetInstance() *AppConfig {
	once.Do(func() {
		var err error
		instance, err = LoadConfig()
		if err != nil {
			panic(fmt.Sprintf("failed to load config: %v", err))
		}
	})
	return instance
}

func MustLoad() *AppConfig {
	config, err := LoadConfig()
	if err != nil {
		panic(fmt.Sprintf("failed to load config: %v", err))
	}
	instance = config
	return config
}

func (c *AppConfig) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}
	if c.Database.Port < 1 || c.Database.Port > 65535 {
		return fmt.Errorf("invalid database port: %d", c.Database.Port)
	}
	return nil
}
