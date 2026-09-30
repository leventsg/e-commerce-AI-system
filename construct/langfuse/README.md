# 本地 Langfuse

独立 Compose 项目 `ecommerce-langfuse`，包含 Web、Worker、PostgreSQL、ClickHouse、Redis 和 MinIO。仅 UI `127.0.0.1:3001` 和浏览器预签名对象下载端口 `127.0.0.1:9090` 对宿主机开放；其他服务只使用独立容器网络。数据保存在该 Compose 项目的命名卷中。

## 启动

要求 Python 3.10+、支持 `up --wait` 的 Docker Compose，以及运行中的 Docker Linux 容器引擎。建议为 Docker 分配至少 4 CPU、8 GB RAM。

在仓库根目录执行：

```powershell
python construct/langfuse/manage.py uct/langfuse/manage.py start
python construct/langfuse/manage.py statusinit
python constr
```

`init` 使用系统安全随机源创建 `.env`，不输出密码。已有文件时拒绝覆盖，防止凭证与持久化数据不一致。示例文件仅说明配置项，不能直接用于启动。Linux 文件权限为 `0600`；Windows 使用工作区用户 ACL，请勿把目录共享给其他用户。

`start` 先初始化开发项目，再短暂重启 Web，使用官方 headless initialization 创建评测项目，最后恢复常规配置并用两组 API Key 校验项目。启动可重复执行，资源按稳定 ID 复用。首次拉取镜像与数据库迁移可能较慢。失败后修复原因并重新运行，无需删除卷。

打开 <http://localhost:3001>，登录邮箱和密码见本地 `.env` 中的 `LANGFUSE_ADMIN_EMAIL`、`LANGFUSE_ADMIN_PASSWORD`。

| 用途 | 项目 ID | `.env` 中的 Key |
|---|---|---|
| 正式 Agent 开发观测 | `ecommerce-dev` | `LANGFUSE_DEV_PUBLIC_KEY` / `LANGFUSE_DEV_SECRET_KEY` |
| 合成数据模拟评测 | `ecommerce-eval` | `LANGFUSE_EVAL_PUBLIC_KEY` / `LANGFUSE_EVAL_SECRET_KEY` |

两个项目有不同 Key。Go Agent 使用开发项目；评测 Go 入口和 Python Langfuse SDK 使用评测项目，同一次实验必须使用同一项目。宿主机接入地址为 `http://localhost:3001`。Agent 若运行在其他容器中，需要使用宿主机可达地址并同步配置网络；不要直接把数据库端口暴露出去。

## 运行与验证

```powershell
python construct/langfuse/manage.py check
docker compose --env-file construct/langfuse/.env -f construct/langfuse/compose.yaml logs --tail 100 langfuse-web langfuse-worker
python -m unittest discover -s tests/langfuse_deploy -v
python construct/langfuse/manage.py stop
```

`check` 校验配置与两个项目的 API 鉴权。`stop` 保留数据卷。不要把完整 `docker compose config` 输出发到日志或提交到仓库，它会展开密码；需要语法验证时使用 `config --quiet`。

完整验收还需要真实模型实验：确认 Langfuse 中 Supervisor、子 Agent、模型、工具及确认恢复能关联；检查对应任务评分和证据；断开 Langfuse 后聊天仍应可用。初始化脚本测试和 Compose 语法校验不能代替该验收。

## 版本与来源

Web/Worker 固定 `4.46.0`，基础服务也固定具体 tag。未使用 `latest` 或浮动主版本。升级时先备份卷，在独立 Compose 项目验证迁移、OTLP 摄取、实验与评分，再同时更新 Web/Worker。

- [Langfuse v4.46.0 官方部署文件](https://github.com/langfuse/langfuse/blob/v4.46.0/docker-compose.yml)
- [Docker Compose 部署](https://langfuse.com/self-hosting/deployment/docker-compose)
- [Headless initialization](https://langfuse.com/self-hosting/administration/headless-initialization)

当前环境验证记录（2026-09-29）：镜像已拉取，四个基础服务健康，Web 和 Worker 健康端点均实际返回 200。已修复容器 HOSTNAME 导致回环健康检查连接失败的问题。Docker 引擎随后在双项目初始化期间停止，`dockerDesktopLinuxEngine` 管道消失；双项目 API 校验和 UI 验收仍未完成。镜像固定不代表完整部署已经验收。

已通过部署脚本测试（5 项）及 Compose `config --quiet`。Docker 稳定运行后可重新执行 `python construct/langfuse/manage.py start`；该操作保留已有数据卷。
