# Installation & Deployment Guide

English | [简体中文](./INSTALL.zh-CN.md)

## Prerequisites

Before you begin, make sure the following software is installed on your server or local environment:

- **Docker** & **Docker Compose**
- **MySQL** (optional, not required if configured to use SQLite)
- **Redis** (optional, not required if configured to use in-memory caching)

---

## 1. Configuration File

Before starting the service, you need to properly configure the `config/config.yaml` file.

### Basic Configuration

Locate `config/config.yaml` in the project root and modify the following core items according to your actual environment:

```yaml
server:
  host: 0.0.0.0
  port: 5678 # Service listening port
  mode: release # Use 'release' for production; 'development' for development
  jwt_secret:
    "" # Set to a random string of at least 32 characters, used for JWT signing. Generate one with:
    # openssl rand -hex 16
  jwt_expire_hours: 24 # JWT expiration time, in hours

# Cache settings
cache:
  type: redis # 'redis' is recommended; use 'memory' for testing
  addr: 127.0.0.1:6379 # Redis address
  password: "" # Redis password
  db: 0

# Database settings
database:
  driver: mysql # 'mysql' or 'sqlite'
  host: 127.0.0.1 # Database IP
  port: 3306
  user: root # Database username
  password: your_password # Database password
  name: epg_hub # Database name, or /config/epg_hub.db (sqlite)
```

### Provider Source Configuration

Every built-in provider ships with its own defaults in code (base URL, priority, concurrency, and so on) and is enabled by default, so `providers` can be left empty:

```yaml
providers: {}
```

To change something, add an entry keyed by provider id with only the fields you want to override. Anything you leave out keeps its default:

```yaml
providers:
  ysp:
    enabled: false # disable the Yangshipin source
  homeplus:
    priority: 1 # lower values have higher priority
    rate_limit: 3 # maximum concurrent requests
    timeout: 15s
```

Available provider ids: `bfgd` `btzx` `cctv_cn` `daxiang` `fengshow` `hainan` `hebei` `homeplus` `iqilu` `jsp` `jstv` `kknews` `migu_tv` `shanxi` `suzhou` `sxrtv` `xiamen` `ysp`

> **Upgrade note**: the old list format (`- name: ... id: ...` entries under `providers:`) is no longer supported and fails at startup. After upgrading, delete the whole list and keep only the overrides you need, as shown above.

## 2. Database Initialization

Before the first run, if MySQL is selected as the database driver in the configuration file, you need to initialize the database schema. Skip this step if you chose SQLite.

1. Create the database:

The project provides a SQL initialization script. Log in to your MySQL database, create a database named epg_sync (or the name specified in your configuration file), and import the table schema.

```sql
CREATE DATABASE epg_sync DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

2. Import the data: use the config/epg_sync.sql file in the project directory.

```bash
mysql -u root -p epg_sync < config/epg_sync.sql
```

## 3. Deploy with Docker (Recommended)

### Step 1: Check the Docker Compose File

Open docker-compose.yml in the project root and make sure the volume mounts and port mappings are correct.

Note on port mapping: the default service port in config.yaml is 5678. If your docker-compose.yml maps 3000:3000, make sure both are consistent, or modify the mapping.

It is recommended to modify docker-compose.yml as follows to match the default configuration:

```yaml
services:
  epg-sync:
    image: epg-sync:latest
    build: .
    container_name: epg-sync
    restart: always
    ports:
      - "5678:5678" # Left is the host port, right is the container port (must match config.yaml)
    volumes:
      - ./config:/config # Mount the configuration directory
      - ./logs:/logs # Mount the logs directory
      - /etc/timezone:/etc/timezone:ro
      - /etc/localtime:/etc/localtime:ro
```

### Step 2: Start the Service

Run the following in the project root:

```bash
docker-compose up -d --build
```

Check the logs to confirm the running status:

```bash
docker-compose logs -f
```

## 4. Build and Run Manually

If you want to develop locally or do not want to use Docker:

### Backend (Go)

1. Make sure the Go environment (Go 1.20+) is installed.

2. Enter the project root directory.

3. Run the service:

```bash
go run cmd/server/main.go
```

### Frontend (Next.js)

1. Make sure Node.js and a package manager (npm/yarn/pnpm) are installed.

2. Enter the web directory, install dependencies, and start the server:

```bash
cd web
pnpm install
pnpm run dev
```

## 5. Access the Management Panel

By default, the EPG Sync web management panel runs on port 3000. Open your browser and visit:

```
http://<server-IP>:3000
```

The login username is `admin`, and the initial password is generated in the startup logs — check the logs to obtain it. It is recommended to change the password immediately after the first login.

After logging in, you can manage program channels, view the program guide, sync the program guide, and more.

## 6. Program Guide API Endpoints

EPG Sync supports both XMLTV and DIYP formats. You can obtain the program guide via the following URLs:

XMLTV format:

```
http://<server-IP>:<port>/api/xmltv
```

DIYP format:

```
http://<server-IP>:<port>/api/diyp
```
