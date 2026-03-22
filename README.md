# agent-container-hub

`agent-container-hub` 是一个基于宿主机 `docker` / `podman` CLI 的容器会话与环境管理服务。
它同时提供：

- 会话管理：创建、执行、停止、查询长生命周期 sandbox session
- 环境注册：通过 `configs/environments/<name>/environment.yml` 维护可被会话引用的命名环境模板
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
- `agent_prompt`
- `mounts`
- `resources`
- `build.dockerfile`

创建 session 时会对这些配置做快照；后续 environment 更新不会回写已有 session。

### 2. 长生命周期 session

每个 session 对应一个常驻容器和一个独立 rootfs 目录。
你可以多次对同一个 session 执行命令，也可以随时停止它并在历史列表中继续查看快照与日志。

### 3. 平台托管镜像构建

environment 可以内嵌 Dockerfile。调用构建接口后，服务会：

- 在 `BUILD_ROOT` 下生成构建上下文
- 调用宿主机容器引擎执行 build
- 保存 build job 记录
- 若配置了 `smoke_command`，再起一个临时容器做 smoke check

构建失败不会丢失记录，失败的 `BuildJob` 会保留。

内置环境：

- `shell` -> `busybox:latest`
- `daily-office` -> `daily-office:latest`

## 快速开始

### 前置要求

- Go 1.26
- 已安装 `docker` 或 `podman`
- 可写的本地目录用于数据库、rootfs、build context

### 本地启动

```bash
cp .env.example .env
```

最少确认这些配置：

- `BIND_ADDR=127.0.0.1:11960`
- `STATE_DB_PATH=./data/agent-container-hub.db`
- `CONFIG_ROOT=./configs`
- `ROOTFS_ROOT=./data/rootfs`
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

### 版本化打包

正式版本号由根目录 `VERSION` 单一管理，格式固定为 `vX.Y.Z`。

构建当前架构的 Linux release bundle：

```bash
make release
```

或显式指定版本与架构：

```bash
make release VERSION=v0.1.0 ARCH=arm64
make release VERSION=v0.1.0 ARCH=amd64
```

产物输出到：

```text
dist/release/agent-container-hub-vX.Y.Z-linux-<arch>.tar.gz
```

bundle 解压后包含：

- `agent-container-hub` Linux 二进制
- `.env.example`
- `start.sh` / `stop.sh`
- `systemd/agent-container-hub.service`
- `configs/environments/` live config
- `data/rootfs/` 与 `data/builds/` 空目录

更完整的发布设计见 [docs/versioned-release-bundle.md](./docs/versioned-release-bundle.md)。

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
  - SQLite 运行态数据库路径，保存 `sessions`、`build_jobs`、`session_executions`
- `CONFIG_ROOT`
  - 默认值：`./configs`
  - environment / image YAML 配置根目录，实际环境文件位于 `configs/environments/<name>/environment.yml`
- `ROOTFS_ROOT`
  - 默认值：`./data/rootfs`
  - session rootfs 根目录
- `BUILD_ROOT`
  - 默认值：`./data/builds`
  - environment build context 与 smoke check 临时目录根路径
- `SESSION_MOUNT_TEMPLATE_ROOT`
  - 默认值：空
  - 可选的会话挂载模板目录；未配置时模板接口返回空模板
- `ENGINE`
  - 默认值：自动探测
  - 可选值：`docker`、`podman`
- `DEFAULT_COMMAND_TIMEOUT`
  - 默认值：`30s`
  - execute 请求未显式提供 `timeout_ms` 时的默认超时
- `DELETE_ROOTFS_ON_STOP`
  - 默认值：`true`
  - session 停止或被校正为 stopped 时是否删除 rootfs 目录
- `ENABLE_EXEC_LOG_PERSIST`
  - 默认值：`false`
  - 是否持久化 execute 日志到 SQLite
- `EXEC_LOG_MAX_OUTPUT_BYTES`
  - 默认值：`65536`
  - 持久化 `stdout/stderr` 时的单字段最大字节数，超出会截断并记录标记

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

# Root directory for per-session rootfs directories mounted into containers at /workspace.
ROOTFS_ROOT=./data/rootfs

# Root directory used for managed image builds and smoke-check temp files.
BUILD_ROOT=./data/builds

# Optional root directory that provides session mount templates for the UI/API.
SESSION_MOUNT_TEMPLATE_ROOT=

# Container engine: docker or podman. Leave empty for auto-detection.
ENGINE=

# Default timeout used when execute requests omit timeout_ms.
DEFAULT_COMMAND_TIMEOUT=30s

# Delete the session rootfs directory when a session stops.
DELETE_ROOTFS_ON_STOP=true

# Persist execute logs into SQLite.
ENABLE_EXEC_LOG_PERSIST=false

# Max bytes stored per stdout/stderr field in session_executions.
EXEC_LOG_MAX_OUTPUT_BYTES=65536
```

## API 概览

### 会话接口

- `POST /api/sessions/create`
- `POST /api/sessions/{id}/execute`
- `POST /api/sessions/{id}/stop`
- `GET /api/sessions`
- `GET /api/sessions/query`
- `GET /api/sessions/{id}`
- `GET /api/sessions/{id}/executions`

创建 session 示例：

```bash
curl -X POST http://127.0.0.1:11960/api/sessions/create \
  -H 'Content-Type: application/json' \
  -d '{
    "session_id": "demo-shell",
    "environment_name": "shell",
    "cwd": "/workspace"
  }'
```

响应示例：

```json
{
  "session_id": "demo-shell",
  "environment_name": "shell",
  "image": "busybox:latest",
  "cwd": "/workspace",
  "rootfs_path": "/absolute/path/to/data/rootfs/demo-shell",
  "mounts": [
    {
      "source": "/srv/agents/shared-tools",
      "destination": "/opt/shared-tools",
      "read_only": true
    },
    {
      "source": "/absolute/path/to/data/rootfs/demo-shell",
      "destination": "/workspace",
      "read_only": false
    }
  ],
  "created_at": "2026-03-17T12:38:34.900000Z",
  "status": "active",
  "duration_ms": 42
}
```

其中 `duration_ms` 是服务端处理 create 请求的总耗时毫秒数。
`mounts` 中既包含 environment YAML 里定义的挂载，也包含系统自动追加的 `/workspace` 挂载。调用方也可以显式传入 `destination=/workspace` 覆盖默认 rootfs 挂载；未覆盖时，`/root` 已经留给调用方自定义 bind mount 使用，常见场景会看到 4 到 5 个业务目录加上 `/workspace`。

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

响应示例：

```json
{
  "session_id": "demo-shell",
  "exit_code": 0,
  "stdout": "/workspace\nhello\n",
  "stderr": "",
  "timed_out": false,
  "duration_ms": 95,
  "started_at": "2026-03-17T12:38:34.954509Z",
  "finished_at": "2026-03-17T12:38:35.049296Z"
}
```

其中 `duration_ms` 是服务端根据 `finished_at - started_at` 计算出的总耗时毫秒数。

查询 session 示例：

```bash
curl "http://127.0.0.1:11960/api/sessions/query?status=history&page=1&page_size=20"
```

查看某个 session 的 execute 日志：

```bash
curl "http://127.0.0.1:11960/api/sessions/demo-shell/executions?page=1&page_size=20"
```

说明：

- `GET /api/sessions` 保持兼容，只返回当前激活中的 session
- `GET /api/sessions/query` 支持 `status=active|history|all`、`session_id`、`environment_name`、`page`、`page_size`
- 历史 session 会保留 `stopped_at`
- 只有在 `ENABLE_EXEC_LOG_PERSIST=true` 时，`/executions` 才会返回持久化日志

停止 session 示例：

```bash
curl -X POST http://127.0.0.1:11960/api/sessions/demo-shell/stop
```

响应示例：

```json
{
  "session_id": "demo-shell",
  "status": "stopped",
  "duration_ms": 18
}
```

其中 `duration_ms` 是服务端处理 stop 请求的总耗时毫秒数。

### 环境接口

- `POST /api/environments`
- `PUT /api/environments/{name}`
- `GET /api/environments`
- `GET /api/environments/{name}`
- `GET /api/environments/{name}/agent-prompt`
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

内置 `daily-office` 直接通过 environment YAML 中的内联 Dockerfile 构建镜像，运行时则依赖只读 `/skills` 挂载：

```text
/Users/linlay/Project/all-skills -> /skills
```

宿主机仍需要具备容器引擎权限，以及访问基础镜像、apt/pip/npm 源和 Himalaya 下载源的能力。

调用侧如果要给智能体注入环境专属提示词，可以先读取：

```bash
curl http://127.0.0.1:11960/api/environments/daily-office/agent-prompt
```

示例响应：

```json
{
  "environment_name": "daily-office",
  "has_prompt": true,
  "prompt": "You are running inside the `daily-office` environment.\n...",
  "updated_at": "2026-03-20T12:00:00Z"
}
```

推荐注入流程：

1. 先 `GET /api/environments/daily-office/agent-prompt`
2. 若 `has_prompt=true`，将 `prompt` 拼到智能体系统提示
3. 再创建或使用该 environment 对应的 session

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
agent_prompt: |
  You are running inside the shell environment.
  Prefer checking existing tools before installing new ones.
enabled: true
build:
  dockerfile: |
    FROM busybox:latest
    CMD ["/bin/sh"]
```

`daily-office` 这类需要运行时技能目录挂载的环境，可以声明默认挂载与环境变量：

```yaml
mounts:
  - source: /Users/linlay/Project/all-skills
    destination: /skills
    read_only: true
default_env:
  NODE_PATH: /opt/daily-office/node_modules
  PATH: /opt/daily-office/node_modules/.bin:/skills/scripts:/skills/docx/scripts:/skills/pptx/scripts:/skills/pdf/scripts:/skills/xlsx/scripts:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
build:
  dockerfile: |
    FROM python:3.12-slim-bookworm
    ...
```

说明：

- API 的 `POST/PUT /api/environments*` 会直接写入或覆盖对应 YAML 文件
- 手工修改 YAML 后无需重启，后续读取会直接生效
- `agent_prompt` 可由调用侧在创建 session 前读取并注入到智能体提示词中
- `daily-office` 默认会把宿主机 `/Users/linlay/Project/all-skills` 只读挂载到容器内 `/skills`
- `GET /api/environments` 遇到坏 YAML 会返回错误并带上文件名
- `GET /api/environments/{name}`、创建 session、触发 build 只读取目标文件，不受无关坏文件影响
- `GET /api/environments/{name}` 与管理站保存/选中环境后，会同时返回 YAML 预览文本

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

- `Session` 页签中按状态/名称/environment 查询 session，并分页浏览历史
- 创建 active session、停止 active session
- 查看 session 创建快照中的 mounts、labels、resources
- 查看某个 session 的 execute 日志
- `Environment` 页签中继续表单编辑 environment，并查看 YAML 预览
- 触发镜像构建

它是嵌入式页面，不是独立前端工程。

## 部署建议

推荐通过 release bundle 部署为宿主机进程：

```bash
make release VERSION=v0.1.0
tar -xzf dist/release/agent-container-hub-v0.1.0-linux-<arch>.tar.gz
cd agent-container-hub
cp .env.example .env
./start.sh
```

开发态也可以直接在源码目录运行：

```bash
make build
./agent-container-hub
```

生产环境建议：

- 将 `STATE_DB_PATH`、`CONFIG_ROOT`、`ROOTFS_ROOT`、`BUILD_ROOT` 指向持久化磁盘
- 优先使用 bundle 内 `systemd/agent-container-hub.service` 模板托管进程
- 对外监听时务必设置 `AUTH_TOKEN`
- 预先确认宿主机容器引擎权限、镜像仓库登录状态和 socket 可用性
- `configs/environments/` 是唯一真相来源；若升级 bundle 前本地改过环境配置，请先备份或合并

容器镜像可通过以下命令构建：

```bash
make docker-build
```

但容器化运行不是默认方式；若在容器中运行本服务，需要额外挂载宿主机容器引擎 socket 并处理权限问题。

### Release Bundle 目录结构

```text
agent-container-hub/
  agent-container-hub
  VERSION
  .env.example
  start.sh
  stop.sh
  README.txt
  systemd/
    agent-container-hub.service
  configs/
    environments/
      shell/
      daily-office/
  data/
    rootfs/
    builds/
```

其中：

- `start.sh` 默认前台运行，`./start.sh --daemon` 可切到后台
- `stop.sh` 只负责停止 `--daemon` 模式启动的本地进程
- systemd 模板里的 `/opt/agent-container-hub` 只是示例路径，部署时需要替换成真实安装目录
- 这个项目虽然是非容器化部署，但运行期仍依赖宿主机 `docker` 或 `podman`

## 常见排查

- 服务启动失败
  - 检查 `docker` / `podman` 是否可执行
  - 检查 `AUTH_TOKEN` 是否满足非本地监听要求
  - 检查 `STATE_DB_PATH`、`ROOTFS_ROOT`、`BUILD_ROOT` 的父目录是否可写
  - 若 `STATE_DB_PATH` 指向旧 bbolt 文件，服务会直接报错；请改成新的 SQLite 文件路径
- session 创建失败
  - 检查 `environment_name` 是否存在且已启用
  - 检查 `configs/environments/<name>.yaml` 是否存在且格式合法
  - 检查 mount 的 `destination` 是否重复；若显式使用保留路径 `/workspace`，会覆盖默认 rootfs 挂载
- execute 日志为空
  - 检查是否设置了 `ENABLE_EXEC_LOG_PERSIST=true`
  - 检查 `EXEC_LOG_MAX_OUTPUT_BYTES` 是否过小导致输出被截断
- build 失败
  - 检查 Dockerfile 是否有效
  - 检查宿主机容器引擎权限和 registry 登录状态
  - 若配置了 smoke check，检查 `smoke_command` 和 `smoke_args`
- 调用方无法工作
  - 检查是否已经切换到 `/api/sessions/*` 与 `/api/environments/*`
  - 检查调用方配置的 `agent.tools.container-hub.base-url`

## 升级说明

当前版本是一次性切换版本，不保留旧接口和旧运行态兼容：

- 只支持 `/api/sessions/*` 与 `/api/environments/*`，不再支持旧 `/execute` 和 `/session/stop`
- 不会自动接管旧 `agentboxd` 容器
- 旧登录 cookie 会失效，需要重新登录管理站
- 旧 `agentbox.db` 或 bbolt 运行态数据不会自动迁移到新的 SQLite 结构
- 升级后请为 `STATE_DB_PATH` 指向一个新的 SQLite 文件；environment 配置仍维护在 `configs/environments/`

更完整的目录职责和开发约束见 [CLAUDE.md](./CLAUDE.md)。
