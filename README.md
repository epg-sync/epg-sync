# EPG Sync - Electronic Program Guide Aggregation & Management System

English | [简体中文](./README.zh-CN.md)

**EPG Sync** is a high-performance EPG synchronization and management system built with Golang and Next.js. It automatically fetches and aggregates program guide data from multiple mainstream TV program sources, and provides a friendly web interface for channel and program management.

## Features

- Supports multiple program guide output formats, including XMLTV and DIYP.
- **Program Guide Aggregation**: Automatically fetches and merges EPG data from multiple sources to generate a unified program guide.
- **Multi-source Support**: Built-in support for multiple mainstream EPG sources, with priority configuration and automatic failover.
  - Yangshipin (YSP)
  - CCTV official website
  - Migu Video (Migu)
  - Phoenix TV
  - KKNews
  - IQilu
  - JSTV
  - Other local TV sources (Suzhou, Hainan, Shanxi, Xiamen, etc.)
- **Web Management Panel**: A modern dashboard built with Next.js 16 and React 19.
  - Channel management and customization
  - EPG source mapping configuration
  - Sync status monitoring
  - Program guide preview and query
- **Automated Sync**: Background scheduled tasks automatically update EPG data.
- **Fast Responses**: Provider responses and generated EPG files are cached in memory.
- **Out-of-the-box Deployment**: One `docker compose up -d` starts the app with PostgreSQL; tables and built-in channel data are created automatically.

## 🚀 Quick Start

Please refer to the [Installation Guide (INSTALL.md)](./INSTALL.md) for deployment and configuration.

## LICENSE

This project is licensed under the GNU GENERAL PUBLIC LICENSE. See the [LICENSE](./LICENSE) file for details.
