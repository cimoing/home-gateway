# Home Gateway

基于 Go、Gin、Vue 3、Vite 和 TypeScript 的单仓库项目。

## 目录结构

```text
cmd/server/       Go 服务入口
internal/database/ SQLx 连接与数据库迁移
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

## 生产镜像

```powershell
docker build -t home-gateway:latest .
docker run --rm -p 8080:8080 `
  -v home-gateway-data:/data `
  home-gateway:latest run
```

访问 `http://localhost:8080`。生产镜像采用多阶段构建，以非 root 用户运行，
最终镜像仅包含静态 Go 二进制文件、Vue 构建产物及可写的 SQLite 数据目录。
