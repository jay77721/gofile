# Docker 部署文件

本目录集中维护 gofile 的容器构建、服务编排和监控部署配置：

```text
docker/
├── Dockerfile
├── docker-compose.yml
└── deploy/
    ├── prometheus/
    └── grafana/
```

Docker 构建上下文仍然是项目根目录，因此 `.dockerignore` 保留在根目录。请在项目根目录执行：

```bash
docker build -f docker/Dockerfile -t gofile:latest .
docker compose -f docker/docker-compose.yml up -d
docker compose -f docker/docker-compose.yml ps
docker compose -f docker/docker-compose.yml logs -f app
```

Compose 默认使用容器内部服务地址，例如 `mysql:3306` 和 `minio:9000`。本地 `.env` 中的宿主机地址不会覆盖这些容器内默认值；如需自定义，可设置 `DOCKER_SERVER_ADDR` 或 `MINIO_INTERNAL_ENDPOINT`。
