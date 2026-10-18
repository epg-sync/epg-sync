# 安装与部署指南

[English](./INSTALL.md) | 简体中文

## 前置要求

- **Docker** & **Docker Compose**

数据库使用 PostgreSQL，已包含在 `docker-compose.yml` 中，不需要单独安装。表结构和内置频道、时区数据会在程序首次启动时自动创建。

---

## 1. 使用 Docker Compose 部署 (推荐)

### 步骤 1：准备文件

把项目根目录下的 `docker-compose.yml` 和 `.env.example` 放到服务器的同一个目录，然后创建 `.env`：

```bash
cp .env.example .env
```

编辑 `.env`，把 `POSTGRES_PASSWORD` 改成你自己的密码。这是唯一必填的配置：Postgres 容器和程序都从这份 `.env` 读取，其余各项都有默认值（见下文「配置说明」）。

### 步骤 2：启动服务

```bash
docker compose up -d
```

会启动三个容器：

- `epg-sync`：程序本身，Web 管理面板和接口都在 3000 端口
- `epg-sync-db`：PostgreSQL 17，数据保存在 `./data/postgres`
- `epg-sync-watchtower`：发布新版本后自动更新 `epg-sync`，见下文

查看日志确认运行状态：

```bash
docker compose logs -f epg-sync
```

启动日志会打印实际生效的配置，例如 `env=prod config_files=[] database=postgres@db:5432/postgres`。首次启动时还会打印默认管理员密码（`Default admin created. User: admin, Pass: ...`）。

### 自动更新与回滚

- **自动更新**：`epg-sync-watchtower` 每 5 分钟检查一次镜像。发布新版本后，它会自动拉取新镜像并重建 `epg-sync` 容器，不需要登录服务器。
  - 它只更新 `epg-sync`。数据库不会自动更新，因为 Postgres 大版本升级需要手动迁移数据。
  - 它使用 scope `epg-sync`，不会动服务器上其它项目的容器。
- **修改 `.env` 后要执行 `docker compose up -d`**：watchtower 重建容器时沿用旧容器的环境变量，`docker restart` 也不会重新读取 `.env`。
- **回滚**：在 `.env` 里写 `IMAGE_TAG=0.1.0`（要回到的版本），再执行 `docker compose up -d`。固定版本的镜像不会再变，自动更新也就暂停了。删掉这一行再执行一次 `docker compose up -d`，即恢复跟随最新版本。
- **日志**：重建容器后，`docker logs` 只剩新容器的输出。需要跨版本的历史日志时，在 `./config/config.yaml` 里设置 `logger.output: both`，日志会写入 `./logs`。

## 2. 配置说明

### 配置从哪里来

配置分两类，每一项只能在一个地方设置：

| 类别 | 设置位置 | 包含 |
| --- | --- | --- |
| 部署信息和机密 | 环境变量（`.env`） | 运行环境、数据库连接、JWT 密钥 |
| 程序行为 | 配置文件 `config/config.yaml` | 端口、超时、日志、抓取源等 |

所有默认值都定义在 [`internal/config/defaults.go`](internal/config/defaults.go) 一处。加载顺序如下，后者覆盖前者：

```
默认值 < config/config.yaml < config/config.<APP_ENV>.yaml < 环境变量
```

- 两个配置文件都是可选的，不存在时使用默认值。
- 配置文件采用严格解析：写错的键、或写了本应用环境变量设置的项（如 `database.host`、`server.jwt_secret`），启动时会直接报错。
- 程序启动时会读取工作目录下的 `.env`（本地开发用），但不会覆盖已经存在的环境变量。

### 环境变量（`.env`）

完整列表也在 [`.env.example`](.env.example) 中，注释里的值就是默认值。

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `POSTGRES_PASSWORD` | 无，**必填** | 数据库密码 |
| `POSTGRES_USER` | `postgres` | 数据库用户名 |
| `POSTGRES_DB` | 与 `POSTGRES_USER` 相同 | 数据库名 |
| `POSTGRES_HOST` | `localhost` | 数据库主机。Docker 中由 compose 固定为 `db` |
| `POSTGRES_PORT` | `5432` | 数据库端口。本地开发时也是数据库映射到本机的端口 |
| `POSTGRES_SSLMODE` | `disable` | 连接外部数据库时可设为 `require` |
| `APP_ENV` | `prod` | `prod` 或 `dev`。`dev` 时额外读取 `config/config.dev.yaml`。Docker 中固定为 `prod` |
| `JWT_SECRET` | 空 | 登录令牌的签名密钥。为空时首次启动自动生成并保存到数据库，重启后登录状态不会失效 |
| `IMAGE_TAG` | `latest` | 仅 docker compose 使用：程序镜像版本。设为具体版本号即回滚，并暂停自动更新 |
| `DOCKER_API_VERSION` | `1.40` | 仅 docker compose 使用：watchtower 调用的 Docker API 版本，Docker Engine 较旧时可调低 |

数据库相关变量的名称和默认值与 [Postgres 官方镜像](https://hub.docker.com/_/postgres) 一致，所以 compose 中数据库容器和程序读取同一份 `.env`，两边的用户名、库名一定相同。

### 配置文件（`config/config.yaml`）

只需写要修改的项，可参考仓库中的 [`config/config.yaml`](config/config.yaml)。Docker 部署时放在宿主机的 `./config/config.yaml`。

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `server.port` | `5678` | 程序监听端口（Docker 中由 nginx 代理到 3000） |
| `server.mode` | `release`（`APP_ENV=dev` 时为 `development`） | 运行模式 |
| `server.timeout` | `90` | HTTP 读写超时，单位秒 |
| `server.jwt_expire_hours` | `24` | 登录有效期，单位小时 |
| `database.max_open_conns` | `10` | 数据库最大连接数 |
| `database.debug` | `false` | 是否打印 SQL |
| `logger.level` | `info` | `debug` / `info` / `warn` / `error` |
| `logger.format` | `console` | `console` / `json` |
| `logger.output` | `stdout` | `stdout` / `stderr` / `file` / `both` |
| `logger.filename` | `logs/epgsync.log` | `output` 为 `file` 或 `both` 时的日志文件。Docker 中对应宿主机 `./logs` |
| `logger.max_size` | `100` | 单个日志文件大小上限，单位 MB |
| `logger.max_backups` | `10` | 保留的旧日志文件数 |
| `logger.max_age` | `30` | 旧日志保留天数 |
| `logger.compress` | `false` | 是否压缩旧日志 |
| `logger.enable_caller` | `false` | 是否记录调用位置 |
| `logger.enable_stack_trace` | `true` | 是否记录堆栈 |
| `logger.stack_trace_level` | `error` | 从哪个级别开始记录堆栈 |
| `providers` | 空 | 抓取源覆盖项，见下文 |

### 渠道源配置

所有内置的抓取源都在代码里自带默认配置（地址、优先级、并发数等），默认全部启用。只有需要调整时，才以 provider id 为 key 写要覆盖的项，没写的项沿用默认值：

```yaml
providers:
  ysp:
    enabled: false # 禁用央视频源
  migu_tv:
    priority: 1 # 优先级，数值越小优先级越高
    rate_limit: 3 # 最大同时请求数
    timeout: 15s
```

可用的 provider id：`bfgd` `btzx` `cctv_cn` `daxiang` `fengshow` `hainan` `hebei` `homeplus` `iqilu` `jsp` `jstv` `kknews` `migu_tv` `shanxi` `suzhou` `sxrtv` `vl` `xiamen` `ysp`

## 3. 用 GUI 客户端查看数据库

数据库端口只对服务器本机开放（`127.0.0.1:5432`），不会暴露到公网。

- **在服务器本机**：用 DBeaver、TablePlus、DataGrip 等客户端直接连接 `127.0.0.1:5432`。
- **从你自己的电脑**：在客户端的连接设置里开启 SSH 隧道，填服务器的 SSH 信息，数据库主机仍然填 `127.0.0.1:5432`。

连接信息就是 `.env` 中的值：用户名 `POSTGRES_USER`（默认 `postgres`），数据库 `POSTGRES_DB`（默认与用户名相同），密码 `POSTGRES_PASSWORD`。

也可以直接用命令行（使用默认用户名时）：

```bash
docker compose exec db psql -U postgres
```

## 4. 从旧版本升级

旧版本使用 MySQL / SQLite 和 Redis，新版本只使用 PostgreSQL，数据不会自动迁移：

- **节目单**：新版本启动后会重新抓取。
- **频道映射**：启动时自动重新生成。
- **管理员账号**：会重新生成，密码见首次启动日志。
- **自定义频道**：内置频道会自动写入。如果你在旧版本里新增或修改过频道，可以用接口导出再导入，已存在的频道会被更新：

```bash
# 1. 从旧版本导出（旧服务还在运行时）
OLD_TOKEN=$(curl -s -X POST http://旧服务器:3000/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"旧密码"}' | jq -r .data.token)
curl -s -H "Authorization: Bearer $OLD_TOKEN" http://旧服务器:3000/admin/channels \
  | jq '{channels: .data}' > channels.json

# 2. 导入到新版本
NEW_TOKEN=$(curl -s -X POST http://新服务器:3000/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"新密码"}' | jq -r .data.token)
curl -s -X POST http://新服务器:3000/admin/channels/batch \
  -H "Authorization: Bearer $NEW_TOKEN" -H 'Content-Type: application/json' -d @channels.json
```

旧版本的 `config.yaml` 需要调整，否则启动时会报错：

- 删除 `cache`、`database`（`max_open_conns`、`debug` 除外）、`scheduler` 段，以及 `server.host`、`server.jwt_secret`。数据库连接和 JWT 密钥改用环境变量。
- `providers` 段改为按 id 覆盖的写法，旧的列表写法不再支持。
- 配置目录在容器中改为 `/app/config`，compose 已按此挂载。

## 5. 本地开发

### 后端 (Go)

1. 确保 Go 1.24+ 已安装。
2. 准备 `.env`：`cp .env.example .env`，填写 `POSTGRES_PASSWORD`，并取消注释 `APP_ENV=dev`。
3. 可选：创建 `config/config.dev.yaml`，只写和 `config/config.yaml` 不同的项（例如 `logger.level: debug`）。该文件已被 git 忽略。
4. 启动本地数据库：

```bash
docker compose up -d db
```

5. 运行服务：

```bash
go run ./cmd/server
```

程序会读取项目根目录的 `.env`，和本地数据库容器使用同一份配置，不需要重复填写密码。

### 前端 (Next.js)

1. 确保 Node.js 和包管理器 (npm/yarn/pnpm) 已安装。
2. 进入 web 目录，安装依赖，并启动服务器：

```bash
cd web
pnpm install
pnpm run dev
```

### 发布新版本

在干净、且与 `origin/main` 同步的 `main` 分支上执行：

```bash
scripts/release.sh            # 交互选择 patch / minor / major / 自定义版本号
scripts/release.sh patch      # 也可以直接指定：patch / minor / major / X.Y.Z
scripts/release.sh --dry-run  # 只校验并显示将要发布的版本和改动，不打 tag
```

脚本确认后会打一个 `X.Y.Z` 的 tag 并推送，之后全部自动完成：

1. GitHub Actions 运行测试（包括 Postgres 集成测试），失败则停止，不会推出镜像
2. 构建 amd64 / arm64 镜像并推送到 GHCR，标签为 `X.Y.Z`、`X.Y`、`latest` 和提交哈希
3. 自动创建 GitHub Release，附上自动生成的更新说明
4. 服务器上的 watchtower 在 5 分钟内拉取新镜像并重建容器

## 6. 访问管理面板

Web 管理面板运行在 3000 端口。打开浏览器，访问：

```
http://<服务器IP>:3000
```

登录用户名是 `admin`，初始密码会在启动日志中生成，请查看日志获取。首次登录后建议立即修改密码。

你可以在登录后管理节目频道、查看节目单、同步节目单等操作。

## 7. 获取节目单接口

EPG Sync 支持 XMLTV 格式和 DIYP 格式。你可以通过以下 URL 获取节目单：

XMLTV 格式：

```
http://<服务器IP>:端口/api/xmltv
```

DIYP 格式：

```
http://<服务器IP>:端口/api/diyp
```
