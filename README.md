# Home Gateway

基于 Go、Gin、Vue 3、Vite 和 TypeScript 的单仓库项目。

## 目录结构

```text
cmd/server/       Go 服务入口
internal/config/  YAML 配置（BT / 存储 / DNS 连接）
internal/database/ SQLite（用户与 BT 状态）迁移
internal/cloudflare/ Cloudflare v4 HTTP 客户端
internal/dns/      DNS 管理（远程 + 内存缓存）
internal/storage/  配置驱动的存储后端
internal/model/    数据模型
internal/router/  Gin 路由
web/              Vue 前端
```

## 本地编译

需要本机已安装 Go、Node.js / npm，以及 `make`：

```powershell
make          # 构建前端、嵌入资源并编译服务端 -> bin/home-gateway
make server   # 仅编译 Go（使用当前已嵌入的前端）
make web      # 仅构建 Vue
make embed-web # 将 web/dist 复制到 internal/webui/dist 供 go:embed
make test     # Go 测试 + 前端构建
make run      # 编译并启动（前端已嵌入二进制）
make release  # 交叉编译 linux/amd64 + linux/arm64 发布包
make clean    # 清理 bin/、web/dist/ 与 dist/
make help     # 查看全部目标
```

前端构建产物通过 `go:embed` 打入二进制，发布包为单文件可执行程序（另附示例配置）。
本地交叉编译产物位于 `dist/home-gateway-linux-amd64.zip` 与
`dist/home-gateway-linux-arm64.zip`。

开发时仍可通过 `WEB_ROOT` 覆盖，从磁盘目录提供前端静态文件。

## GitHub Actions

推送 `v*` 标签（如 `v1.0.0`）时，Actions 会自动：

1. 运行 Go 测试并构建前端
2. 将前端嵌入后交叉编译 **linux/amd64（x64）** 与 **linux/arm64（树莓派 64 位）**
3. 打包为包含单二进制与示例配置的 `.zip` 产物
4. 创建 GitHub Release 并上传两个架构的压缩包

树莓派 4/5 及启用 64 位系统的树莓派 3 请使用 `home-gateway-linux-arm64.zip`。

## 开发

构建开发镜像：

```powershell
docker build -f Dockerfile.dev --target development -t home-gateway:dev .
```

启动前后端开发服务，并挂载本地源码：

```powershell
docker run --rm -it `
  -p 8080:8080 `
  -p 5173:5173 `
  -v "${PWD}:/workspace" `
  -v home-gateway-node-modules:/workspace/web/node_modules `
  -v "${PWD}/config:/config" `
  -v "${PWD}/data:/data" `
  home-gateway:dev
```

前端地址为 `http://localhost:5173`，API 健康检查地址为
`http://localhost:8080/api/health`。前端支持 Vite 热更新；修改 Go 代码后需重启容器。

## Docker Compose 本地运行

`compose.yml` 构建生产镜像并以嵌入式 SQLite 运行，默认启用 IPv6 网络。首次可复制
环境变量与配置模板，并按需设置 `config.yaml` 中 `${VAR}` 引用的密钥：

```powershell
Copy-Item .env.example .env
New-Item -ItemType Directory -Force config, data | Out-Null
Copy-Item config.example.yaml config/config.yaml
docker compose up -d --build
docker compose ps
```

Web 地址为 `http://localhost:8080`，默认嵌入式引擎（`anacrolix`）使用 `42069/tcp` 与 `42069/udp`。

容器通过 `DATA=/data` 设置数据根目录；配置与数据中的默认路径均为相对路径：

| 用途 | 路径 |
|------|------|
| 配置文件 | `/config/config.yaml`（主机 `./config`） |
| 数据根 `DATA` | `/data`（主机 `./data`） |
| SQLite | `$DATA/db/home-gateway.db` |
| 下载目录 | `$DATA/bt/downloads` |

可选：使用 Compose profile `transmission` 启动 `transmission-daemon`，并在
`config.yaml` 中设置 `bt.engine: transmission` 与
`bt.transmission.url: http://transmission:9091/transmission/rpc`。此时请去掉
`app` 服务上的 BT 端口映射，改由 `transmission` 服务发布 `BT_PORT`，且两边共用
同一 `DATA` 卷，使引擎本地下载路径一致。

查看日志和停止服务：

```powershell
docker compose logs -f app
docker compose down
```

主机 Docker 需启用 IPv6（`daemon.json` 中 `"ipv6": true`）后，compose 默认网络才会真正下发 IPv6 地址。

## 测试

```powershell
docker build -f Dockerfile.dev --target test -t home-gateway:test .
```

该阶段使用 SQLite 执行全部 Go 测试及 Vue 生产构建：

```powershell
docker compose -f compose.test.yml up --build `
  --abort-on-container-exit `
  --exit-code-from tests
docker compose -f compose.test.yml down -v
```

任一迁移、约束测试或前端构建失败都会返回非零状态。

## 数据库

应用仅支持嵌入式 SQLite，相关环境变量：

- `DATA`：数据根目录（默认 `.`；容器内为 `/data`），相对路径均基于此解析
- `DB_DRIVER`：仅允许 `sqlite`（默认）
- `DB_DSN`：SQLite 路径，默认相对路径 `db/home-gateway.db`（相对 `DATA`）

服务启动时会自动执行嵌入式 SQLite 迁移。用户密码与 BT 任务状态保存在该库中。

## 命令行

API 服务通过 `run` 子命令启动：

```powershell
docker run --rm -p 8080:8080 `
  -v "${PWD}/config:/config" `
  -v "${PWD}/data:/data" `
  home-gateway:latest run
```

交互式创建用户及修改密码：

```powershell
docker run --rm -it `
  -v "${PWD}/config:/config" `
  -v "${PWD}/data:/data" `
  home-gateway:latest user create admin

docker run --rm -it `
  -v "${PWD}/config:/config" `
  -v "${PWD}/data:/data" `
  home-gateway:latest user passwd admin
```

密码输入默认关闭终端回显并要求输入两次。自动化场景可从标准输入读取一次：

```powershell
"initial-password" | docker run --rm -i `
  -v "${PWD}/config:/config" `
  -v "${PWD}/data:/data" `
  home-gateway:latest user create admin --password-stdin
```

用户名长度为 1 至 64 个字符且不能包含空白或控制字符；密码长度为 8 至 72
字节。数据库只保存 bcrypt 哈希。

## 登录

登录页面由生产服务在 `http://localhost:8080` 提供。认证接口包括：

- `POST /api/auth/login`：校验用户名和密码并创建会话
- `GET /api/auth/session`：读取当前登录用户
- `POST /api/auth/logout`：撤销当前会话

会话令牌为随机值，保存在**进程内存**中（重启后需重新登录）；浏览器通过
`HttpOnly`、`SameSite=Lax` Cookie 持有令牌。会话默认有效期为 24 小时；连续失败
登录会触发短期限流。HTTPS 部署时应设置 `SESSION_SECURE=true`。

## 配置文件

默认读取 `/config/config.yaml`（见 `config.example.yaml`）：

- `bt.*`：下载引擎参数（部分可在 Web 设置页写回）
  - `bt.engine`：`anacrolix`（默认，进程内）或 `transmission`（RPC 连接 daemon）
  - `bt.transmission.url` / `username` / `password`：仅 `engine=transmission` 时需要
  - `bt.download_dir`：相对 `DATA` 的本地下载路径（默认 `bt/downloads`）
  - `bt.block`：屏蔽规则（客户端、Peer ID、端口、IP/CIDR）；Peers 列表右键可追加，支持热加载；`transmission` 引擎下客户端/Peer ID/端口规则不会在握手层强制生效
  - `POST /api/bt/block`：追加一条屏蔽规则并写回 YAML
- `storage.backends[]`：按**名称**定义 local / smb / s3；密钥用 `${ENV}`
  - Web「存储管理 → 同步」支持任意两个后端之间的双栏目录对比与复制（`POST /api/storage/sync/jobs`）
- `dns.cloudflare.token` / `zones`：Cloudflare 连接与托管域名列表

修改连接类配置后，可调用 `POST /api/system/reload-config`（需登录）热加载，无需
重启进程。存储后端不在 Web UI 中增删改。

## Cloudflare DNS 管理

Token 与 Zone 列表来自 YAML。Web 界面可查看域名、维护 A/AAAA/CNAME/TXT/MX/CAA/SRV
记录。列表优先使用进程内缓存；编辑会先写 Cloudflare 再增量更新缓存；“刷新”
会全量拉取远程记录。Cloudflare 始终是权威数据源。

API Token 建议最小权限：

- `Zone / Zone / Read`
- `Zone / DNS / Edit`

受登录保护的 API 位于 `/api/dns`：

- `GET /zones`：列出配置中的域名
- `POST /zones/:zoneName/sync`：手动全量刷新记录
- `/zones/:zoneName/records`：读取缓存以及远程记录增删改查

## BT 下载管理

默认使用进程内嵌的 `anacrolix/torrent`；也可通过 `bt.engine: transmission` 对接
本机或 Compose 中的 `transmission-daemon`（RPC）。两种引擎均支持磁力链接和
`.torrent` 文件、任务列表与实时进度、暂停/恢复、文件选择及优先级、删除任务以及
可选删除下载数据。任务状态和文件选择保存在数据库中，服务重启后会自动恢复并续传。
Web 管理接口均要求登录。

BT 仅下载到本机 `bt.download_dir`。跨存储复制请使用「存储管理 → 同步」双栏界面，
在任意两个已配置后端之间对比目录并复制文件/文件夹。

BT 任务状态与文件选择保存在 SQLite；下载内容在本地磁盘上。

使用自定义配置文件：

```powershell
docker run --rm -p 8080:8080 `
  -p 42069:42069/tcp `
  -p 42069:42069/udp `
  -v "${PWD}/config:/config" `
  -v "${PWD}/data:/data" `
  home-gateway:latest run
```

Web 添加任务时可指定相对子目录与存储后端名称。绝对路径和 `..` 目录穿越会被拒绝。

BT API 位于 `/api/bt`：

- `GET /settings`：读取当前只读配置和引擎状态
- `GET/POST /tasks`：查询任务，或通过 `/tasks/magnet`、`/tasks/torrent` 添加
- `/tasks/:id/pause|resume`：暂停和恢复
- `/tasks/:id/files`：查询及更新文件选择和优先级
- `DELETE /tasks/:id?deleteData=true`：删除任务并可选删除数据

配置位于 `/config`；数据库与下载内容位于 `DATA` 下的相对路径（`db/`、`bt/downloads/`），升级或迁移前应备份这两个目录。
如修改 `bt.listen_port`，Docker 的 TCP 和 UDP 映射必须同步修改。

## 生产镜像

### 本地源码构建

```powershell
docker build -t home-gateway:latest .
docker run --rm -p 8080:8080 `
  -p 42069:42069/tcp `
  -p 42069:42069/udp `
  -v "${PWD}/config:/config" `
  -v "${PWD}/data:/data" `
  home-gateway:latest run
```

访问 `http://localhost:8080`。镜像以非 root 用户运行，前端已嵌入二进制，
挂载点为 `/config` 与 `/data`。

### 从 GitHub Release 构建（`Dockerfile.gh`）

不在本地编译，而是下载已发布的 zip，校验哈希后打成运行镜像。适用于只想用
官方发布产物、或在构建环境不便安装 Go/Node 的场景。

需要传入：

| 变量 | 说明 |
|------|------|
| `VERSION` | Release 版本，可带或不带 `v` 前缀（如 `1.0.0` / `v1.0.0`） |
| `SHA256` | **当前目标架构**对应 zip 的 SHA-256（`home-gateway-linux-amd64.zip` 或 `arm64`） |
| `REPO` | 可选，默认 `cimoing/home-gateway` |

PowerShell 示例（amd64）：

```powershell
$VERSION = "v1.0.0"
$ASSET = "home-gateway-linux-amd64.zip"
$URL = "https://github.com/cimoing/home-gateway/releases/download/$VERSION/$ASSET"

# 下载后计算哈希，或从 Release 说明 / checksum 文件获取
Invoke-WebRequest $URL -OutFile $ASSET
$SHA256 = (Get-FileHash $ASSET -Algorithm SHA256).Hash.ToLower()

docker build `
  -f Dockerfile.gh `
  --build-arg VERSION=$VERSION `
  --build-arg SHA256=$SHA256 `
  -t "home-gateway:${VERSION}" `
  .
```

多架构（buildx，需分别为各架构提供对应 zip 的哈希）：

```powershell
docker buildx build `
  -f Dockerfile.gh `
  --platform linux/amd64 `
  --build-arg VERSION=v1.0.0 `
  --build-arg SHA256=<amd64-zip-sha256> `
  -t home-gateway:v1.0.0-amd64 `
  --load `
  .

docker buildx build `
  -f Dockerfile.gh `
  --platform linux/arm64 `
  --build-arg VERSION=v1.0.0 `
  --build-arg SHA256=<arm64-zip-sha256> `
  -t home-gateway:v1.0.0-arm64 `
  --load `
  .
```

树莓派等 arm64 设备请使用 `home-gateway-linux-arm64.zip` 的哈希，并加
`--platform linux/arm64`。哈希不匹配时构建会失败，避免安装被篡改的二进制。
