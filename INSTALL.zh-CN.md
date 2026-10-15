# 安装与部署指南

[English](./INSTALL.md) | 简体中文

## 前置要求

在开始之前，请确保你的服务器或本地环境已安装以下软件：

- **Docker** & **Docker Compose**
- **MySQL** (可选，如果配置为 sqlite 则不需要)
- **Redis** (可选，如果配置为内存缓存则不需要)

---

## 1. 配置文件

在启动服务前，需要正确配置 `config/config.yaml` 文件。

### 基础配置

找到项目根目录下的 `config/config.yaml`，根据你的实际环境修改以下核心项：

```yaml
server:
  host: 0.0.0.0
  port: 5678 # 服务监听端口
  mode: release # 生产环境建议设置为 release 开发环境可设置为 development
  jwt_secret:
    "" # 请修改为至少32位的随机字符串 用于 JWT 签名 可以用下面的命令生成：
    # openssl rand -hex 16
  jwt_expire_hours: 24 # JWT 过期时间，单位小时

# 缓存设置
cache:
  type: redis # 推荐使用 'redis'，测试可用 'memory'
  addr: 127.0.0.1:6379 # Redis 地址
  password: "" # Redis 密码
  db: 0

# 数据库设置
database:
  driver: mysql # 可选 'mysql' 或 'sqlite'
  host: 127.0.0.1 # 数据库 IP
  port: 3306
  user: root # 数据库用户名
  password: your_password # 数据库密码
  name: epg_hub # 数据库名 或者 /config/epg_hub.db (sqlite)
```

### 渠道源配置

所有内置的抓取源都在代码里自带默认配置（地址、优先级、并发数等），默认全部启用，`providers` 留空即可：

```yaml
providers: {}
```

只有需要调整时，才以 provider id 为 key 写要覆盖的项，没写的项沿用默认值：

```yaml
providers:
  ysp:
    enabled: false # 禁用央视频源
  homeplus:
    priority: 1 # 优先级，数值越小优先级越高
    rate_limit: 3 # 最大同时请求数
    timeout: 15s
```

可用的 provider id：`bfgd` `btzx` `cctv_cn` `daxiang` `fengshow` `hainan` `hebei` `homeplus` `iqilu` `jsp` `jstv` `kknews` `migu_tv` `shanxi` `suzhou` `sxrtv` `xiamen` `ysp`

> **升级提示**：旧版本的列表写法（`providers:` 下面是 `- name: ... id: ...`）已不再支持，启动时会报错。升级后请删掉整段列表，按上面的写法只保留需要覆盖的项。

## 2. 数据库初始化

在首次运行前，如果配置文件中选择了 MySQL 作为数据库驱动，则需要初始化数据库结构。选择 sqlite 则跳过此步骤。

1. 创建数据库：

项目提供了 SQL 初始化脚本。请登录你的 MySQL 数据库，创建一个名为 epg_sync (或你配置文件中指定的名称) 的数据库，并导入表结构。

```sql
CREATE DATABASE epg_sync DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

2. 导入数据： 使用项目目录下的 config/epg_sync.sql 文件进行导入。

```bash
mysql -u root -p epg_sync < config/epg_sync.sql
```

## 3. 使用 Docker 部署 (推荐)

### 步骤 1：检查 Docker Compose 文件

打开项目根目录下的 docker-compose.yml，确保卷挂载和端口映射正确。

注意端口映射： 配置文件 config.yaml 中默认服务端口为 5678。如果你的 docker-compose.yml 映射的是 3000:3000，请确保两者一致，或者修改映射关系。

建议修改 docker-compose.yml 如下以匹配默认配置：

```yaml
services:
  epg-sync:
    image: epg-sync:latest
    build: .
    container_name: epg-sync
    restart: always
    ports:
      - "5678:5678" # 左边是宿主机端口，右边是容器内端口(需与 config.yaml 一致)
    volumes:
      - ./config:/config # 挂载配置文件目录
      - ./logs:/logs # 挂载日志目录
      - /etc/timezone:/etc/timezone:ro
      - /etc/localtime:/etc/localtime:ro
```

### 步骤 2：启动服务

在项目根目录下执行：

```bash
docker-compose up -d --build
```

查看日志确认运行状态：

```bash
docker-compose logs -f
```

## 4. 手动编译运行

如果你想在本地进行开发或不使用 Docker：

### 后端 (Go)

1. 确保 Go 环境 (Go 1.20+) 已安装。

2. 进入项目根目录。

3. 运行服务：

```bash
go run cmd/server/main.go
```

### 前端 (Next.js)

1. 确保 Node.js 和包管理器 (npm/yarn/pnpm) 已安装。

2. 进入 web 目录,安装依赖,并启动服务器：

```bash
cd web
pnpm install
pnpm run dev
```

## 5. 访问管理面板

默认情况下，EPG Sync 的 Web 管理面板运行在 3000 端口。打开浏览器，访问：

```
http://<服务器IP>:3000
```

登录用户名是 `admin`，初始密码会在启动日志中生成 ，请查看日志获取。首次登录后建议立即修改密码。

你可以在登录后管理节目频道、查看节目单、同步节目单等操作。

## 6. 获取节目单接口

EPG Sync 支持 XMLTV 格式 和 DIYP 格式。你可以通过以下 URL 获取节目单：

XMLTV 格式：

```
http://<服务器IP>:<端口>/api/xmltv
```

DIYP 格式：

```
http://<服务器IP>:<端口>/api/diyp
```
