# Chameleon 🦎: Evolve Your Connections with a SOCKS5 SmartProxyChain

Chameleon is more than just a SOCKS5 server; it's a **SmartProxyChain** manager that empowers you with unparalleled control and flexibility over your network traffic. By maintaining a dynamic pool of upstream SOCKS5 proxies, Chameleon intelligently routes client connections based on defined user permissions and proxy characteristics (tags), ensuring optimal performance, security, and reliability.

## Why Chameleon? Evolve Your Proxying Strategy!

With Chameleon's SmartProxyChain, you can:

*   🚀 **Optimize Performance & Suitability:**
    Direct traffic through the fastest, most geographically appropriate, or task-specific upstream proxies. Assign tags like "high-speed-streaming", "usa-exit", or "data-scraping" and let users access the resources they need efficiently.
*   🛡️ **Enhance Access Control & Security:**
    Define precisely which authenticated SOCKS5 users can access which types of upstream proxies using a flexible tagging system. Implement granular access policies and ensure compliance.
*   💪 **Improve Reliability & Resilience:**
    Chameleon's automatic health checks continuously monitor your upstream proxy pool, deactivating unresponsive proxies and ensuring your traffic is only routed through healthy, active connections. Optional webhook notifications keep you informed of critical pool status changes.
*   📊 **Gain Deep Insight & Observability:**
    Monitor your entire proxy infrastructure with comprehensive Prometheus metrics. Track request volumes, success/failure rates, individual proxy performance, and active proxy counts to understand usage patterns and identify bottlenecks.
*   ⚙️ **Stay Agile & Adaptable:**
    Dynamically adapt to changing network conditions or user requirements. Reload your upstream proxy list, user credentials, and even core application settings on-the-fly via secure API endpoints or a SIGHUP signal, all without service interruption.

Chameleon provides a robust, observable, and highly configurable solution for building and managing a sophisticated SOCKS5 proxying infrastructure.

## Table of Contents

1.  [Key Features](#key-features)
2.  [Getting Started](#getting-started)
    *   [Prerequisites](#prerequisites)
    *   [Installation](#installation)
3.  [Configuration Deep Dive](#configuration-deep-dive)
    *   [Main Configuration (`config.yml`)](#main-configuration-configyml)
    *   [Upstream Proxies (`proxies.json` with Tags)](#upstream-proxies-proxiesjson-with-tags)
    *   [SOCKS5 Users (`users.json` with Allowed Tags)](#socks5-users-usersjson-with-allowed-tags)
4.  [Running Chameleon](#running-chameleon)
    *   [Directly](#directly)
    *   [Using Docker](#using-docker)
    *   [Configuration Testing](#configuration-testing-1)
5.  [Dynamic Management API](#dynamic-management-api)
6.  [Monitoring Your SmartProxyChain](#monitoring-your-smartproxychain)
    *   [Structured Logging](#structured-logging)
    *   [Prometheus Metrics](#prometheus-metrics-1)
7.  [OS Signals](#os-signals)
8.  [Contributing](#contributing)
9.  [License](#license)

## Key Features

*   **SOCKS5 Server:** Fully compliant SOCKS5 protocol implementation.
*   **SmartProxyChain Engine:**
    *   Manages a pool of upstream SOCKS5 proxies.
    *   Intelligent, tag-based routing of client traffic.
    *   Configurable default routing policies for users without specific tags.
*   **User Authentication:** Secure username/password authentication for SOCKS5 clients.
*   **Active Health Checks:** Continuous monitoring of upstream proxy availability and latency.
*   **Dynamic Configuration:** Hot-reloading of proxy lists, user credentials, and main settings.
*   **Comprehensive Monitoring:** Detailed Prometheus metrics and structured file-based logging.
*   **Webhook Alerts:** Optional notifications for critical changes in proxy pool health.
*   **Dockerized:** Ready for deployment with `Dockerfile` and `docker-compose.yml`.

## Getting Started

### Prerequisites

*   Go (version 1.21 or higher recommended - check `go.mod` for the exact version)
*   Docker & Docker Compose (for containerized deployment)
*   A list of upstream SOCKS5 proxies you wish to manage.

### Installation

1.  **Clone the repository:**
    ```bash
    git clone https://github.com/sequring/chameleon.git
    cd chameleon
    ```

2.  **Build from source:**
    ```bash
    go build -o chameleon_server ./main.go
    ```
    (The Go module system will use `github.com/sequring/chameleon` as the module path from `go.mod`)

## Configuration Deep Dive

Chameleon's flexibility stems from its layered configuration. It's recommended to copy the example configuration files (`config.example.yml`, `proxies.example.json`, `users.example.json`) and adapt them to your needs.

### 1. Main Configuration (`config.yml`)

This YAML file is the heart of Chameleon, controlling server ports, logging behavior, paths to other configuration files, and crucial default policies for the SmartProxyChain. Create your `config.yml` based on `config.example.yml`.


**Crucial `users` section in `config.yml`:**
```yaml
# ...
users:
  config_file_path: "users.json" # Path to your users file
  # Defines behavior if a user has no 'allowed_proxy_tags' specified:
  # "deny": Deny access.
  # "allow_default_tag_only": Allow access only to proxies with 'default_proxy_tag'.
  # "allow_all_active": Allow access to any active proxy.
  default_behavior_no_tags: "allow_default_tag_only"
  default_proxy_tag: "general" # Tag used if above is "allow_default_tag_only"
# ...
```
**Remember to set a strong and unique `admin_api_reload_token` in your `config.yml`!**

### 2. Upstream Proxies (`proxies.json` with Tags)

Define your pool of upstream SOCKS5 proxies in a JSON file (e.g., `proxies.json`, path configured in `config.yml`). Each proxy can be assigned multiple tags. See `proxies.example.json` for structure.

**Example entry in `proxies.json`:**
```json
  {
    "address": "1.2.3.4:1080", "username": "puser1", "password": "ppass1",
    "tags": ["usa", "fast-isp", "general-browsing"]
  }
```

### 3. SOCKS5 Users (`users.json` with Allowed Tags)

Manage your SOCKS5 client credentials and their access rights in a JSON file (e.g., `users.json`, path configured in `config.yml`). See `users.example.json` for structure.

**Example entry in `users.json`:**
```json
  {
    "username": "premium_user", "password": "verysecure", "allowed": true,
    "allowed_proxy_tags": ["fast-isp", "streaming-optimized"]
  }
```

## Running Chameleon

### Directly

1.  Ensure your configuration files (`config.yml`, `proxies.json`, `users.json`) are correctly set up.
2.  Execute: `./chameleon_server -config /path/to/your/config.yml` (or `./chameleon_server` if `config.yml` is in the current directory).

### Using Docker

1.  Ensure your configuration files are present in the project root (or adjust paths in `docker-compose.yml`).
2.  Create a `logs` directory: `mkdir logs`.
3.  Run: `docker-compose up --build -d`
4.  Stop: `docker-compose down`
5.  Logs: `docker-compose logs -f chameleon_proxy` (or your service name in `docker-compose.yml`)

### Configuration Testing

Validate your setup without starting services:
```bash
./chameleon_server -t -config /path/to/your/config.yml
```

## Dynamic Management API

The admin server (default: `:8081`, see `config.yml`) provides endpoints for dynamic management. **All reload endpoints require the `X-Reload-Token` header (set in `config.yml`).**

*   **`POST /reload-proxies`**: Reloads the proxies file.
*   **`POST /reload-users`**: Reloads the users file.

Example:
```bash
curl -X POST -H "X-Reload-Token: YOUR_CONFIGURED_TOKEN" http://localhost:8081/reload-proxies
```

## Monitoring Your SmartProxyChain

### Structured Logging

*   **`access.log`**: Detailed SOCKS5 request logs.
*   **`error.log`**: Application operational logs, errors (also mirrored to `stdout`).
    Log paths and rotation are configured in `config.yml`.

### Prometheus Metrics

Access comprehensive metrics at `/metrics` on the admin server for monitoring.

## OS Signals

*   **`SIGINT`**, **`SIGTERM`**: Graceful shutdown.
*   **`SIGHUP`**: Reloads all configuration files.

## Contributing

Contributions to Chameleon are highly welcome! Please feel free to fork the repository, make your changes, and submit a Pull Request. You can also open an Issue to report bugs or suggest features.

## License

Chameleon is licensed under the  MIT License. See the `LICENSE` file in the repository for more details.

