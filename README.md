# agent-container-hub

`agent-container-hub` 是一个基于宿主机 `docker` / `podman` CLI 的容器会话与环境管理服务。
它同时提供：

- 会话管理：创建、执行、停止、查询长生命周期 sandbox session
- 环境注册：通过 `configs/environments/*.yaml` 维护可被会话引用的命名环境模板
- 镜像构建：基于环境中保存的 Dockerfile 触发本地构建与 smoke check
- 内置管理站：同一个 Go 进程同时托管 API 和轻量 Web UI

默认部署方式是宿主机二进制运行，不依赖 Kubernetes，也不是 Docker-in-Docker。

## 核心能力

### 1. 环境驱动的会话创建

会话不能直接凭镜像裸创建，必须引用一个已注册且启用的 environment。
environment YAML 定义了：

- `image_repository` / `image_tag`
- `default_cwd`
- `default_env`
- `mounts`
- `resources`
- `build.dockerfile`

创建 session 时会对这些配置做快照；后续 environment 更新不会回写已有 session。

### 2. 长生命周期 session

每个 session 对应一个常驻容器和一个独立 workspace。
你可以多次对同一个 session 执行命令，也可以随时停止并删除它。

### 3. 平台托管镜像构建

environment 可以内嵌 Dockerfile。调用构建接口后，服务会：

- 在 `BUILD_ROOT` 下生成构建上下文
- 调用宿主机容器引擎执行 build
- 保存 build job 记录
- 若配置了 `smoke_command`，再起一个临时容器做 smoke check

构建失败不会丢失记录，失败的 `BuildJob` 会保留。

## 快速开始

### 前置要求

- Go 1.26
- 已安装 `docker` 或 `podman`
- 可写的本地目录用于数据库、workspace、build context

### 本地启动

```bash
cp .env.example .env
```

最少确认这些配置：

- `BIND_ADDR=127.0.0.1:11960`
- `STATE_DB_PATH=./data/agent-container-hub.db`
- `CONFIG_ROOT=./configs`
- `WORKSPACE_ROOT=./data/workspaces`
- `BUILD_ROOT=./data/builds`
- `ENGINE=` 留空自动探测，或显式指定 `docker` / `podman`

启动：

```bash
make run
```

或：

```bash
make build
./agent-container-hub
```

测试：

```bash
make test
```

## 配置

项目只使用环境变量配置。

主要配置项：

- `BIND_ADDR`
  - 默认值：`127.0.0.1:8080`
  - HTTP 监听地址
- `AUTH_TOKEN`
  - 默认值：空
  - 当监听地址不是本地回环地址时必填
- `STATE_DB_PATH`
  - 默认值：`./data/agent-container-hub.db`
  - 运行态元数据数据库路径，只保存 `sessions` 和 `build_jobs`
- `CONFIG_ROOT`
  - 默认值：`./configs`
  - environment / image YAML 配置根目录，实际环境文件位于 `configs/environments/*.yaml`
- `WORKSPACE_ROOT`
  - 默认值：`./data/workspaces`
  - session workspace 根目录
- `BUILD_ROOT`
  - 默认值：`./data/builds`
  - environment build context 与 smoke check 临时目录根路径
- `ENGINE`
  - 默认值：自动探测
  - 可选值：`docker`、`podman`
- `ALLOWED_MOUNT_ROOTS`
  - 默认值：`WORKSPACE_ROOT`
  - 允许额外挂载的宿主机路径白名单
- `DEFAULT_COMMAND_TIMEOUT`
  - 默认值：`30s`
  - execute 请求未显式提供 `timeout_ms` 时的默认超时

示例：

```dotenv
# HTTP listen address. Use 127.0.0.1 for local-only access.
BIND_ADDR=127.0.0.1:11960

# Optional when binding locally. Required when binding to non-local addresses.
AUTH_TOKEN=

# Runtime metadata database path for sessions and build jobs.
STATE_DB_PATH=./data/agent-container-hub.db

# Root directory for YAML environment/image configs.
CONFIG_ROOT=./configs

# Root directory for per-session workspaces mounted into containers at /workspace.
WORKSPACE_ROOT=./data/workspaces

# Root directory used for managed image builds and smoke-check temp files.
BUILD_ROOT=./data/builds

# Container engine: docker or podman. Leave empty for auto-detection.
ENGINE=

# Comma-separated whitelist of host paths allowed in mounts[].source.
ALLOWED_MOUNT_ROOTS=./data/workspaces,/tmp/agent-container-hub-mounts

# Default timeout used when execute requests omit timeout_ms.
DEFAULT_COMMAND_TIMEOUT=30s
```

## API 概览

### 会话接口

- `POST /api/sessions/create`
- `POST /api/sessions/{id}/execute`
- `POST /api/sessions/{id}/stop`
- `GET /api/sessions`
- `GET /api/sessions/{id}`

创建 session 示例：

```bash
curl -X POST http://127.0.0.1:11960/api/sessions/create \
  -H 'Content-Type: application/json' \
  -d '{
    "session_id": "demo-shell",
    "environment_name": "shell"
  }'
```

执行命令示例：

```bash
curl -X POST http://127.0.0.1:11960/api/sessions/demo-shell/execute \
  -H 'Content-Type: application/json' \
  -d '{
    "command": "/bin/sh",
    "args": ["-lc", "pwd && echo hello"],
    "timeout_ms": 30000
  }'
```

停止 session 示例：

```bash
curl -X POST http://127.0.0.1:11960/api/sessions/demo-shell/stop
```

### 环境接口

- `POST /api/environments`
- `PUT /api/environments/{name}`
- `GET /api/environments`
- `GET /api/environments/{name}`
- `POST /api/environments/{name}/build`

注册 environment 示例：

```bash
curl -X POST http://127.0.0.1:11960/api/environments \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "shell",
    "description": "basic shell environment",
    "image_repository": "busybox",
    "image_tag": "latest",
    "default_cwd": "/workspace",
    "enabled": true,
    "build": {
      "dockerfile": "FROM busybox:latest\nCMD [\"/bin/sh\"]\n"
    }
  }'
```

触发 build 示例：

```bash
curl -X POST http://127.0.0.1:11960/api/environments/shell/build
```

### Environment YAML

environment 主数据不再保存在 `agent-container-hub.db` 中，而是以 YAML 文件形式维护在：

```text
configs/environments/*.yaml
```

示例：

```yaml
name: shell
description: Basic shell environment managed from configs/.
image_repository: busybox
image_tag: latest
default_cwd: /workspace
enabled: true
build:
  dockerfile: |
    FROM busybox:latest
    CMD ["/bin/sh"]
```

说明：

- API 的 `POST/PUT /api/environments*` 会直接写入或覆盖对应 YAML 文件
- 手工修改 YAML 后无需重启，后续读取会直接生效
- `GET /api/environments` 遇到坏 YAML 会返回错误并带上文件名
- `GET /api/environments/{name}`、创建 session、触发 build 只读取目标文件，不受无关坏文件影响

### 管理站与认证

- `GET /`
- `GET /login`
- `POST /api/auth/login`
- `POST /api/auth/logout`

认证方式：

- API 支持 `Authorization: Bearer <token>`
- 管理站登录成功后使用 cookie `agent-container-hub_auth`
- 当 `AUTH_TOKEN` 为空时，不启用鉴权

## Web UI

服务内置轻量管理站，可用于：

- 浏览和编辑 environments
- 触发镜像构建
- 浏览现有 sessions
- 创建 session 并停止 session

它是嵌入式页面，不是独立前端工程。

## 部署建议

推荐直接部署为宿主机进程：

```bash
make build
./agent-container-hub
```

生产环境建议：

- 将 `STATE_DB_PATH`、`CONFIG_ROOT`、`WORKSPACE_ROOT`、`BUILD_ROOT` 指向持久化磁盘
- 使用 systemd、supervisor 或类似工具托管进程
- 对外监听时务必设置 `AUTH_TOKEN`
- 预先确认宿主机容器引擎权限、镜像仓库登录状态和 socket 可用性

容器镜像可通过以下命令构建：

```bash
make docker-build
```

但容器化运行不是默认方式；若在容器中运行本服务，需要额外挂载宿主机容器引擎 socket 并处理权限问题。

## 常见排查

- 服务启动失败
  - 检查 `docker` / `podman` 是否可执行
  - 检查 `AUTH_TOKEN` 是否满足非本地监听要求
  - 检查 `STATE_DB_PATH`、`WORKSPACE_ROOT`、`BUILD_ROOT` 的父目录是否可写
- session 创建失败
  - 检查 `environment_name` 是否存在且已启用
  - 检查 `configs/environments/<name>.yaml` 是否存在且格式合法
  - 检查 environment 中的 mount 是否位于 `ALLOWED_MOUNT_ROOTS` 白名单内
- build 失败
  - 检查 Dockerfile 是否有效
  - 检查宿主机容器引擎权限和 registry 登录状态
  - 若配置了 smoke check，检查 `smoke_command` 和 `smoke_args`
- 调用方无法工作
  - 检查是否已经切换到 `/api/sessions/*` 与 `/api/environments/*`
  - 检查调用方配置的 `agent.tools.agent-container-hub.base-url`

## 升级说明

当前版本是一次性切换版本，不保留旧接口和旧运行态兼容：

- 只支持 `/api/sessions/*` 与 `/api/environments/*`，不再支持旧 `/execute` 和 `/session/stop`
- 不会自动接管旧 `agentboxd` 容器
- 旧登录 cookie 会失效，需要重新登录管理站
- 旧 `agentbox.db` 里的 environment 配置不会自动迁移到 `configs/`
- 若仍需复用旧运行态数据，请显式设置 `STATE_DB_PATH` 指向旧文件；镜像/environment 配置请改为维护在 `configs/environments/`

更完整的目录职责和开发约束见 [CLAUDE.md](./CLAUDE.md)。
# agent-container-hub
