# Installation and Deployment Guide

English | [简体中文](./INSTALL.zh-CN.md)

## Prerequisites

- **Docker** & **Docker Compose**

The database is PostgreSQL and is already included in `docker-compose.yml`, so there is nothing else to install. Tables and the built-in channel and timezone data are created automatically on first start.

---

## 1. Deploy with Docker Compose (Recommended)

### Step 1: Prepare the files

Put `docker-compose.yml` and `.env.example` from the project root into the same directory on your server, then create `.env`:

```bash
cp .env.example .env
```

Edit `.env` and set `POSTGRES_PASSWORD` to your own password. It is the only required setting: both the Postgres container and the application read this `.env`, and everything else has a default (see "Configuration" below).

### Step 2: Start the services

```bash
docker compose up -d
```

This starts two containers:

- `epg-sync`: the application; the web admin panel and the APIs are on port 3000
- `epg-sync-db`: PostgreSQL 17, with data stored in `./data/postgres`

Check the logs to confirm it is running:

```bash
docker compose logs -f epg-sync
```

The startup log prints the effective configuration, for example `env=prod config_files=[] database=postgres@db:5432/postgres`. On first start it also prints the default admin password (`Default admin created. User: admin, Pass: ...`).

## 2. Configuration

### Where settings come from

Settings fall into two groups, and each setting can be set in exactly one place:

| Group | Set in | Covers |
| --- | --- | --- |
| Deployment and secrets | Environment variables (`.env`) | Runtime environment, database connection, JWT secret |
| Application behavior | Config file `config/config.yaml` | Port, timeouts, logging, providers, etc. |

All defaults are defined in one place, [`internal/config/defaults.go`](internal/config/defaults.go). Settings are loaded in this order, later ones overriding earlier ones:

```
defaults < config/config.yaml < config/config.<APP_ENV>.yaml < environment variables
```

- Both config files are optional; missing files fall back to the defaults.
- Config files are parsed strictly: a misspelled key, or a setting that belongs in an environment variable (such as `database.host` or `server.jwt_secret`), fails at startup.
- On startup the application reads `.env` from the working directory (for local development), without overriding variables that are already set.

### Environment variables (`.env`)

The full list is also in [`.env.example`](.env.example); the values in its comments are the defaults.

| Variable | Default | Description |
| --- | --- | --- |
| `POSTGRES_PASSWORD` | none, **required** | Database password |
| `POSTGRES_USER` | `postgres` | Database user |
| `POSTGRES_DB` | same as `POSTGRES_USER` | Database name |
| `POSTGRES_HOST` | `localhost` | Database host. Set to `db` by compose in Docker |
| `POSTGRES_PORT` | `5432` | Database port. In local development, also the port the database is published on |
| `POSTGRES_SSLMODE` | `disable` | Set to `require` for an external database |
| `APP_ENV` | `prod` | `prod` or `dev`. `dev` also reads `config/config.dev.yaml`. Always `prod` in Docker |
| `JWT_SECRET` | empty | Login token signing secret. If empty, one is generated on first start and stored in the database, so logins stay valid across restarts |
| `EPG_IMAGE` | `ghcr.io/epg-sync/epg-sync:latest` | Used by docker compose only: the application image |

The database variables use the same names and defaults as the [official Postgres image](https://hub.docker.com/_/postgres), so in compose the database container and the application read the same `.env` and always agree on the user and database name.

### Config file (`config/config.yaml`)

Only write the settings you want to change; see [`config/config.yaml`](config/config.yaml) in the repository for an example. With Docker, put it at `./config/config.yaml` on the host.

| Key | Default | Description |
| --- | --- | --- |
| `server.port` | `5678` | Port the application listens on (proxied to 3000 by nginx in Docker) |
| `server.mode` | `release` (`development` when `APP_ENV=dev`) | Run mode |
| `server.timeout` | `90` | HTTP read/write timeout, in seconds |
| `server.jwt_expire_hours` | `24` | Login lifetime, in hours |
| `database.max_open_conns` | `10` | Maximum open database connections |
| `database.debug` | `false` | Log SQL statements |
| `logger.level` | `info` | `debug` / `info` / `warn` / `error` |
| `logger.format` | `console` | `console` / `json` |
| `logger.output` | `stdout` | `stdout` / `stderr` / `file` / `both` |
| `logger.filename` | `logs/epgsync.log` | Log file used when `output` is `file` or `both`. Maps to `./logs` on the host in Docker |
| `logger.max_size` | `100` | Maximum size of one log file, in MB |
| `logger.max_backups` | `10` | Number of old log files to keep |
| `logger.max_age` | `30` | Days to keep old log files |
| `logger.compress` | `false` | Compress old log files |
| `logger.enable_caller` | `false` | Log the calling location |
| `logger.enable_stack_trace` | `true` | Log stack traces |
| `logger.stack_trace_level` | `error` | Lowest level that gets a stack trace |
| `providers` | empty | Provider overrides, see below |

### Provider Source Configuration

Every built-in provider ships with its own defaults in code (base URL, priority, concurrency, and so on) and is enabled by default. To change something, add an entry keyed by provider id with only the fields you want to override. Anything you leave out keeps its default:

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

## 3. Browse the Database with a GUI Client

The database port is only open on the server itself (`127.0.0.1:5432`) and is not exposed to the internet.

- **On the server**: connect DBeaver, TablePlus, DataGrip or a similar client to `127.0.0.1:5432`.
- **From your own computer**: enable an SSH tunnel in the client's connection settings with your server's SSH details, and keep the database host as `127.0.0.1:5432`.

The connection details are the values in `.env`: user `POSTGRES_USER` (default `postgres`), database `POSTGRES_DB` (defaults to the user name), and password `POSTGRES_PASSWORD`.

You can also use the command line (with the default user):

```bash
docker compose exec db psql -U postgres
```

## 4. Upgrading from an Older Version

Older versions used MySQL / SQLite and Redis. This version only uses PostgreSQL, and data is not migrated automatically:

- **Programs**: fetched again after the new version starts.
- **Channel mappings**: regenerated automatically on startup.
- **Admin account**: recreated; the password is in the first startup log.
- **Custom channels**: the built-in channels are created automatically. If you added or edited channels in the old version, export them through the API and import them into the new one. Existing channels are updated:

```bash
# 1. Export from the old version (while it is still running)
OLD_TOKEN=$(curl -s -X POST http://old-server:3000/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"old-password"}' | jq -r .data.token)
curl -s -H "Authorization: Bearer $OLD_TOKEN" http://old-server:3000/admin/channels \
  | jq '{channels: .data}' > channels.json

# 2. Import into the new version
NEW_TOKEN=$(curl -s -X POST http://new-server:3000/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"new-password"}' | jq -r .data.token)
curl -s -X POST http://new-server:3000/admin/channels/batch \
  -H "Authorization: Bearer $NEW_TOKEN" -H 'Content-Type: application/json' -d @channels.json
```

An old `config.yaml` needs changes, or startup fails:

- Remove the `cache`, `database` (except `max_open_conns` and `debug`) and `scheduler` sections, plus `server.host` and `server.jwt_secret`. The database connection and JWT secret now come from environment variables.
- The `providers` section now uses per-id overrides; the old list format is no longer supported.
- Inside the container the config directory is now `/app/config`; compose already mounts it there.

## 5. Local Development

### Backend (Go)

1. Make sure Go 1.24+ is installed.
2. Prepare `.env`: `cp .env.example .env`, set `POSTGRES_PASSWORD`, and uncomment `APP_ENV=dev`.
3. Optional: create `config/config.dev.yaml` with only the settings that differ from `config/config.yaml` (for example `logger.level: debug`). This file is ignored by git.
4. Start the local database:

```bash
docker compose up -d db
```

5. Run the server:

```bash
go run ./cmd/server
```

The server reads the `.env` in the project root, the same file the database container uses, so the password is never written twice.

### Frontend (Next.js)

1. Make sure Node.js and a package manager (npm/yarn/pnpm) are installed.
2. Go to the web directory, install dependencies, and start the dev server:

```bash
cd web
pnpm install
pnpm run dev
```

## 6. Access the Admin Panel

The web admin panel runs on port 3000. Open in your browser:

```
http://<server-ip>:3000
```

The username is `admin`, and the initial password is generated in the startup log. Change it after your first login.

After logging in you can manage channels, view program schedules, and sync EPG data.

## 7. EPG Endpoints

EPG Sync supports the XMLTV and DIYP formats:

XMLTV:

```
http://<server-ip>:<port>/api/xmltv
```

DIYP:

```
http://<server-ip>:<port>/api/diyp
```
