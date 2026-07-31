# Home Gateway

基于 Go、Gin、Vue 3、Vite 和 TypeScript 的单仓库项目。

## 目录结构

```text
cmd/server/       Go 服务入口
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
  home-gateway:dev
```

前端地址为 `http://localhost:5173`，API 健康检查地址为
`http://localhost:8080/api/health`。前端支持 Vite 热更新；修改 Go 代码后需重启容器。

## 测试

```powershell
docker build -f Dockerfile.dev --target test -t home-gateway:test .
```

该阶段会执行全部 Go 测试及 Vue 生产构建，任一失败都会中止镜像构建。

## 生产镜像

```powershell
docker build -t home-gateway:latest .
docker run --rm -p 8080:8080 home-gateway:latest
```

访问 `http://localhost:8080`。生产镜像采用多阶段构建，以非 root 用户运行，
最终镜像仅包含静态 Go 二进制文件和 Vue 构建产物。
