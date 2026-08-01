# Home Gateway

基于 Go、Gin、Vue 3、Vite 和 TypeScript 的单仓库项目。

## 目录结构

```text
cmd/server/       Go 服务入口
internal/database/ SQLx 连接与数据库迁移
internal/cloudflare/ Cloudflare v4 HTTP 客户端
internal/credential/ API Token 加密
internal/dns/      DNS 管理服务与 API
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

`compose.yml` 会构建生产镜像，并启动 Home Gateway 与 PostgreSQL 18。首次运行先复制
环境变量模板，并替换数据库密码及凭据加密主密钥：

```powershell
Copy-Item .env.example .env
```

`CREDENTIAL_ENCRYPTION_KEY` 必须是 Base64 编码的 32 字节随机值。配置完成后启动：

```powershell
docker compose up -d --build
docker compose ps
```

Web 地址为 `http://localhost:8080`，PostgreSQL 默认映射到本机 `5432`，BT 使用
`42069/tcp` 和 `42069/udp`。数据库与下载数据分别保存在
`postgres-data`、`app-data` 命名卷中。查看日志和停止服务：

```powershell
docker compose logs -f app
docker compose down
```

`docker compose down` 会保留数据卷；只有明确需要清空全部数据库和下载数据时才使用
`docker compose down -v`。

## 测试

```powershell
docker build -f Dockerfile.dev --target test -t home-gateway:test .
```

该阶段使用 SQLite 执行全部 Go 测试及 Vue 生产构建。完整验证 SQLite、PostgreSQL
和 MySQL：

```powershell
docker compose -f compose.test.yml up --build `
  --abort-on-container-exit `
  --exit-code-from tests
docker compose -f compose.test.yml down -v
```

任一数据库迁移、约束测试或前端构建失败都会返回非零状态。

## 数据库

应用使用以下环境变量：

- `DB_DRIVER`：`sqlite`、`pgsql`/`postgres` 或 `mysql`，默认为 `sqlite`
- `DB_DSN`：数据库连接字符串；SQLite 默认值为 `/data/home-gateway.db`

PostgreSQL 示例：

```powershell
docker run --rm -p 8080:8080 `
  -e DB_DRIVER=pgsql `
  -e "DB_DSN=postgres://gateway:password@database:5432/gateway?sslmode=disable" `
  home-gateway:latest
```

MySQL 示例：

```powershell
docker run --rm -p 8080:8080 `
  -e DB_DRIVER=mysql `
  -e "DB_DSN=gateway:password@tcp(database:3306)/gateway?parseTime=true" `
  home-gateway:latest
```

服务启动时会自动执行当前数据库方言对应的嵌入式迁移。

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

会话令牌为随机值，数据库仅保存 SHA-256 哈希，浏览器通过 `HttpOnly`、
`SameSite=Lax` Cookie 持有令牌。会话默认有效期为 24 小时；连续失败登录会触发
短期限流。HTTPS 部署时应设置 `SESSION_SECURE=true`。

## Cloudflare DNS 管理

登录后可在 Web 界面管理 API Token、绑定域名以及维护 A、AAAA、CNAME、TXT、
MX、CAA 和 SRV 记录。记录查询默认读取本地缓存；创建、修改和删除会先写入
Cloudflare，再更新缓存。“同步”操作会拉取全部远程分页并在单个数据库事务中
更新缓存，Cloudflare 始终是权威数据源。

创建 API Token 时应只授予目标 Zone 的以下最小权限：

- `Zone / Zone / Read`
- `Zone / DNS / Edit`

Token 使用 AES-256-GCM 加密。运行服务前必须通过
`CREDENTIAL_ENCRYPTION_KEY` 提供 Base64 编码的 32 字节主密钥；没有配置时服务
仍可启动和登录，但凭据写入及 Cloudflare 操作会返回 503。PowerShell 生成示例：

```powershell
$bytes = New-Object byte[] 32
$rng = New-Object Security.Cryptography.RNGCryptoServiceProvider
$rng.GetBytes($bytes)
$key = [Convert]::ToBase64String($bytes)
$key
```

启动时传入密钥：

```powershell
docker run --rm -p 8080:8080 `
  -e "CREDENTIAL_ENCRYPTION_KEY=$key" `
  -v home-gateway-data:/data `
  home-gateway:latest run
```

密钥不会写入数据库。必须将该密钥与 `/data` 数据卷分别安全备份；丢失或替换
密钥后，已有 API Token 无法解密。不要将密钥提交到 Git 或写入镜像。

受登录保护的 API 位于 `/api/dns`：

- `/credentials`：列出、添加、更新和删除加密凭据
- `/zones`：列出、绑定和移除域名，`POST /zones/:id/sync` 手动同步
- `/zones/:id/records`：读取缓存以及远程记录增删改查

## BT 下载管理

服务内嵌 `anacrolix/torrent`，支持磁力链接和 `.torrent` 文件、任务列表与实时
进度、暂停/恢复、文件选择及优先级、删除任务以及可选删除下载数据。任务状态和
文件选择保存在数据库中，服务重启后会自动恢复并续传。Web 管理接口均要求登录。

默认读取 `/data/config.yaml`；文件不存在时下载目录为 `/data/downloads`，
TCP/UDP 监听端口为 `42069`。可复制 `config.example.yaml`：

```yaml
bt:
  enabled: true
  download_dir: /data/downloads
  listen_port: 42069
```

使用自定义配置文件：

```powershell
docker run --rm -p 8080:8080 `
  -p 42069:42069/tcp `
  -p 42069:42069/udp `
  -v "${PWD}/config.yaml:/data/config.yaml:ro" `
  -v home-gateway-data:/data `
  home-gateway:latest run --config /data/config.yaml
```

Web 添加任务时可指定下载根目录内的相对子目录，只影响该新任务，不修改 YAML
或已有任务。绝对路径和 `..` 目录穿越会被拒绝。删除任务时可选择保留文件或同时
删除；数据删除仅针对数据库中记录且位于配置根目录内的种子文件。

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
  -e "CREDENTIAL_ENCRYPTION_KEY=$key" `
  -v home-gateway-data:/data `
  home-gateway:latest run
```

访问 `http://localhost:8080`。生产镜像采用多阶段构建，以非 root 用户运行，
最终镜像仅包含静态 Go 二进制文件、Vue 构建产物及可写的 SQLite 数据目录。
