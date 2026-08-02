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
  -v home-gateway-data:/data `
  home-gateway:dev
```

前端地址为 `http://localhost:5173`，API 健康检查地址为
`http://localhost:8080/api/health`。前端支持 Vite 热更新；修改 Go 代码后需重启容器。

## Docker Compose 本地运行

`compose.yml` 构建生产镜像并以嵌入式 SQLite 运行。首次可复制环境变量模板，并按需
设置 `config.yaml` 中 `${VAR}` 引用的密钥（如 `CF_API_TOKEN`、`SMB_PASSWORD`）：

```powershell
Copy-Item .env.example .env
Copy-Item config.example.yaml config.yaml
docker compose up -d --build
docker compose ps
```

Web 地址为 `http://localhost:8080`，BT 使用 `42069/tcp` 与 `42069/udp`。SQLite、
配置与下载数据保存在 `app-data` 命名卷中。查看日志和停止服务：

```powershell
docker compose logs -f app
docker compose down
```

`docker compose down` 会保留数据卷；只有明确需要清空全部数据时才使用
`docker compose down -v`。

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

- `DB_DRIVER`：仅允许 `sqlite`（默认）
- `DB_DSN`：SQLite 文件路径，默认 `/data/home-gateway.db`

服务启动时会自动执行嵌入式 SQLite 迁移。用户密码与 BT 任务状态保存在该库中。

## 命令行

API 服务通过 `run` 子命令启动：

```powershell
docker run --rm -p 8080:8080 `
  -v home-gateway-data:/data `
  home-gateway:latest run
```

交互式创建用户及修改密码：

```powershell
docker run --rm -it `
  -v home-gateway-data:/data `
  home-gateway:latest user create admin

docker run --rm -it `
  -v home-gateway-data:/data `
  home-gateway:latest user passwd admin
```

密码输入默认关闭终端回显并要求输入两次。自动化场景可从标准输入读取一次：

```powershell
"initial-password" | docker run --rm -i `
  -v home-gateway-data:/data `
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

默认读取 `/data/config.yaml`（见 `config.example.yaml`）：

- `bt.*`：下载引擎参数（部分可在 Web 设置页写回）
  - `bt.storage_backend`：可选，默认存储后端名称；留空则使用本地文件系统
  - `bt.download_dir`：未选后端时为本地路径；选中后端时为该后端上的相对目录
  - `bt.block`：屏蔽规则（客户端、Peer ID、端口、IP/CIDR）；Peers 列表右键可追加，支持热加载
  - `POST /api/bt/block`：追加一条屏蔽规则并写回 YAML
- `storage.backends[]`：按**名称**定义 local / smb / s3；密钥用 `${ENV}`
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

服务内嵌 `anacrolix/torrent`，支持磁力链接和 `.torrent` 文件、任务列表与实时
进度、暂停/恢复、文件选择及优先级、删除任务以及可选删除下载数据。任务状态和
文件选择保存在数据库中，服务重启后会自动恢复并续传。Web 管理接口均要求登录。

BT 任务状态、文件选择与同步进度保存在 SQLite；下载内容在磁盘上。添加任务时可
选择配置中的存储后端名称；远程后端先写入本地 staging，再按 `complete` /
`per_file` 策略同步。

使用自定义配置文件：

```powershell
docker run --rm -p 8080:8080 `
  -p 42069:42069/tcp `
  -p 42069:42069/udp `
  -v "${PWD}/config.yaml:/data/config.yaml:ro" `
  -v home-gateway-data:/data `
  home-gateway:latest run --config /data/config.yaml
```

Web 添加任务时可指定相对子目录与存储后端名称。绝对路径和 `..` 目录穿越会被拒绝。

BT API 位于 `/api/bt`：

- `GET /settings`：读取当前只读配置和引擎状态
- `GET/POST /tasks`：查询任务，或通过 `/tasks/magnet`、`/tasks/torrent` 添加
- `/tasks/:id/pause|resume`：暂停和恢复
- `/tasks/:id/files`：查询及更新文件选择和优先级
- `DELETE /tasks/:id?deleteData=true`：删除任务并可选删除数据

数据库、任务元数据和默认下载文件均位于 `/data`，升级或迁移前应备份该数据卷。
如修改 `bt.listen_port`，Docker 的 TCP 和 UDP 映射必须同步修改。

## 生产镜像

```powershell
docker build -t home-gateway:latest .
docker run --rm -p 8080:8080 `
  -p 42069:42069/tcp `
  -p 42069:42069/udp `
  -v home-gateway-data:/data `
  home-gateway:latest run
```

访问 `http://localhost:8080`。生产镜像采用多阶段构建，以非 root 用户运行，
最终镜像仅包含静态 Go 二进制文件、Vue 构建产物及可写的 SQLite 数据目录。
