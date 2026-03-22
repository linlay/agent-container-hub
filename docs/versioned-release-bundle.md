# 版本化非容器化 Release Bundle

## 1. 目标与边界

这个项目的 release bundle 目标，是产出一个带明确版本号、单目标架构、可直接解压运行的 Linux 宿主机部署包，方便上传到 GitHub Release、自建制品库或内网服务器，再由部署端直接解压后启动。

它解决的是“如何交付一个可运行版本”，不是“如何分发源码”：

- 交付物是最终 bundle，而不是源码压缩包。
- bundle 内包含预构建 Go 二进制和最小运行资产，部署端不需要源码构建环境。
- bundle 面向宿主机进程部署，不包含 compose 文件，也不打 Docker 镜像 tar。
- 每次构建只产出一个目标架构 bundle，不做多架构合包。

当前仓库的版本单一来源是根目录 `VERSION` 文件，正式版本格式固定为 `vX.Y.Z`。发布脚本会强校验这个格式。以当前项目版本 `v0.1.0` 为例，最终产物命名规则为：

- `agent-container-hub-v0.1.0-linux-arm64.tar.gz`
- `agent-container-hub-v0.1.0-linux-amd64.tar.gz`

## 2. 方案总览

延续参考项目的结构，这套方案仍然分成四层：

1. 版本层：根目录 `VERSION` 统一管理正式版本号。
2. 构建层：按目标架构构建 Linux Go 二进制。
3. 组装层：把二进制、配置模板、环境配置、启动脚本和 systemd 模板组装成标准 bundle 目录。
4. 交付层：把 bundle 目录压缩成最终 `tar.gz`，输出到固定产物目录。

在 `agent-container-hub` 中，对应位置为：

- 版本来源：`VERSION`
- 构建入口：`make release` / `scripts/release.sh`
- release 模板资产：`scripts/release-assets/`
- 最终产物目录：`dist/release/`

## 3. 本项目怎么打包

### 3.1 打包入口

正式发布入口：

```bash
make release
```

`Makefile` 会把 `VERSION` 和 `ARCH` 传给脚本：

```bash
VERSION=$(VERSION) ARCH=$(ARCH) bash scripts/release.sh
```

也可以直接执行：

```bash
bash scripts/release.sh
```

常见用法：

```bash
make release VERSION=v1.0.0
make release VERSION=v1.0.0 ARCH=arm64
make release VERSION=v1.0.0 ARCH=amd64
```

其中：

- `VERSION` 默认读取根目录 `VERSION`
- `ARCH` 未显式传入时，会按 `uname -m` 自动识别为 `amd64` 或 `arm64`
- 脚本内部固定用 `GOOS=linux GOARCH=<arch>` 交叉编译 Linux 二进制

### 3.2 打包输入

`scripts/release.sh` 的主要输入包括：

- 版本号：`VERSION` 文件或环境变量 `VERSION`
- 目标架构：环境变量 `ARCH` 或当前机器架构
- 构建入口：`./cmd/agent-container-hub`
- 配置模板：`.env.example`
- live config：`configs/environments/...`
- release 模板资产：
  - `scripts/release-assets/start.sh`
  - `scripts/release-assets/stop.sh`
  - `scripts/release-assets/README.txt`
  - `scripts/release-assets/systemd/agent-container-hub.service`

脚本会强校验版本格式：

- 只接受 `vX.Y.Z`
- 不符合时直接失败，不继续构建

### 3.3 构建过程

构建脚本会使用 Go 原生交叉编译输出一个 Linux 单文件二进制：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=$ARCH \
  go build \
  -ldflags "-X main.buildVersion=$VERSION" \
  -o ...
```

这里有两个关键点：

- `CGO_ENABLED=0` 让产物更适合做离线分发
- `-ldflags "-X main.buildVersion=..."` 会把 bundle 版本写入服务启动日志

### 3.4 组装过程

脚本会组装出一个标准目录 `agent-container-hub/`，其中包含：

- `agent-container-hub`
- `.env.example`
- `start.sh`
- `stop.sh`
- `README.txt`
- `systemd/agent-container-hub.service`
- `configs/environments/...`
- `data/rootfs/`
- `data/builds/`

这里的 `configs/environments/` 直接作为运行配置打包，不再区分初始化模板目录和运行目录。也就是说，bundle 解压后默认就以 `./configs/environments` 作为唯一真相来源。

### 3.5 最终输出

最终 bundle 输出到：

```bash
dist/release/agent-container-hub-vX.Y.Z-linux-<arch>.tar.gz
```

这是对外分发的正式交付物。

## 4. 打哪些包，产物在哪里

### 4.1 交付层产物

正式交付层产物只有一个：

- `dist/release/agent-container-hub-vX.Y.Z-linux-arm64.tar.gz`
- `dist/release/agent-container-hub-vX.Y.Z-linux-amd64.tar.gz`

注意：

- 每次构建只会产出其中一个架构包
- `dist/release/` 是固定输出目录
- `dist/` 已加入 `.gitignore`

### 4.2 bundle 解压后的运行时结构

```text
agent-container-hub/
  agent-container-hub
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

部署端运行后，还会生成：

- `.env`：由使用者从 `.env.example` 复制并填入真实配置
- `.runtime/agent-container-hub.pid`：`./start.sh --daemon` 时生成
- `.runtime/agent-container-hub.log`：daemon 模式输出日志
- `data/agent-container-hub.db`：默认 SQLite 运行态数据库

## 5. 部署端如何消费这些包

### 5.1 标准部署步骤

```bash
tar -xzf agent-container-hub-v1.0.0-linux-amd64.tar.gz
cd agent-container-hub
cp .env.example .env
./start.sh
```

如果希望后台运行：

```bash
./start.sh --daemon
```

停止后台进程：

```bash
./stop.sh
```

### 5.2 `start.sh` 做了什么

`start.sh` 是 bundle 内的标准启动入口，会依次执行：

1. 校验当前目录下存在 bundle 必备文件。
2. 校验 `.env` 是否存在。
3. 创建 `.runtime/`、`data/rootfs/`、`data/builds/`。
4. 加载 `.env` 到当前进程环境。
5. 若配置了 `ENGINE`，检查对应 CLI 是否存在；否则检查 `docker` 或 `podman`。
6. 默认前台启动二进制，或在 `--daemon` 模式下后台运行并写入 pid/log 文件。

这意味着部署端不需要源码，也不需要先执行 `go build`。

### 5.3 `stop.sh` 做了什么

`stop.sh` 只负责停止 `./start.sh --daemon` 启动的本地后台进程：

- 读取 `.runtime/agent-container-hub.pid`
- 发送 `SIGTERM`
- 最多等待 30 秒
- 退出后删除 pid 文件

它不会清理数据库、rootfs、build 目录，也不会停止由服务创建的业务容器。

### 5.4 systemd 模板怎么用

bundle 内提供：

```text
systemd/agent-container-hub.service
```

这是一个模板文件，默认示例路径使用：

```text
/opt/agent-container-hub
```

部署端需要先把其中的安装路径替换成真实路径，再复制到 `/etc/systemd/system/` 并启用。这个 unit 直接通过：

- `WorkingDirectory`
- `EnvironmentFile`
- `ExecStart`

来启动 bundle 内二进制，不依赖 `start.sh`。

## 6. 非容器化部署时要注意什么

虽然 bundle 本身是宿主机进程部署，但服务能力仍然依赖宿主机容器引擎：

- session create / execute / stop 依赖 `docker` 或 `podman`
- environment build 依赖宿主机容器引擎 build 权限
- 若需要拉取私有基础镜像，部署端仍需具备 registry 登录状态

所以“非容器化部署”只表示服务本身不是通过容器交付，并不表示运行期不需要容器引擎。

## 7. 升级约定

当前 bundle 的升级策略保持简单：

- 新版本 bundle 会覆盖程序文件和默认配置模板
- `configs/environments/` 被视为 live config，升级时应先人工确认是否保留本地修改
- `data/` 应放在持久化磁盘，避免升级时丢失 session 元数据和构建目录
- v1 不做自动配置 merge，也不做旧 bundle 到新 bundle 的迁移脚本

如果需要把本地配置与官方 bundle 分离，建议在部署时通过 `.env` 显式指定外部 `CONFIG_ROOT`、`STATE_DB_PATH`、`ROOTFS_ROOT`、`BUILD_ROOT`。
