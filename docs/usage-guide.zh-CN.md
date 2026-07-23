# AgentsharkX 中文使用指南

本文档适用于 AgentsharkX `0.7.0 Phase 7 preview`，介绍预览环境启动、登录、
真实 Agent 接入、四个工作区、日常运维、本地开发、发布验证和常见故障处理。

除非特别说明，所有命令都在仓库根目录执行：

```bash
cd /path/to/AgentsharkX
```

## 1. 产品定位

AgentsharkX 是位于 [agentgateway](https://github.com/agentgateway/agentgateway)
和 [AgentGuard](https://github.com/WhitzardAgent/AgentGuard) 之上的统一管理控制台：

- agentgateway 负责模型、Provider、MCP、路由、策略和请求日志；
- AgentGuard 负责 Agent、会话、工具、技能、安全事件、规则和审批；
- AgentsharkX 负责认证隔离、能力检测、数据归一化、聚合展示、管理操作和诊断。

AgentsharkX 是管理平面，不在 Agent 数据平面中，因此：

- Agent 的模型或 MCP 流量应直接经过 agentgateway；
- AgentGuard 客户端应直接接入 Agent 运行时；
- AgentsharkX 只读取两个上游的管理 API；
- AgentsharkX 不代理 Agent 流量，不根据时间或名称推断 Agent/任务，也不实现新的规则
  引擎、数据库、回放系统或流量采集器。

## 2. 快速启动预览环境

### 2.1 环境要求

运行预览环境需要：

- GNU Make；
- Docker 和 Docker Compose v2；
- OpenSSL；
- Python 3.11+ 和 Git，用于运行首条真实事件示例。

只有在本地开发或验证源码时，才额外需要 Node.js 24、npm 和 Go 1.26.5。
未安装本地 Go 时，部分 Make 目标可以使用仓库固定的 Go 容器。

### 2.2 生成本地凭据

首次启动前执行：

```bash
make preview-bootstrap
```

该命令会：

- 创建被 Git 忽略的根目录 `.env`；
- 生成随机的 AgentsharkX 管理员令牌和 AgentGuard API Key；
- 将 `.env` 权限设置为 `0600`；
- 默认把所有发布端口绑定到 `127.0.0.1`；
- 在 `.env` 已存在时拒绝覆盖，也不会把生成的凭据输出到终端。

不要直接使用未修改的 `deploy/example.env` 启动服务。模板中的占位令牌会被 BFF
启动校验拒绝。

### 2.3 启动服务

```bash
make preview-up
```

首次启动需要拉取 agentgateway 镜像，并构建 AgentGuard 和 AgentsharkX，耗时可能
长于后续启动。

查看容器状态：

```bash
make preview-status
```

正常情况下，应看到以下服务处于运行或健康状态：

- `agentshark`；
- `agentgateway`；
- `agentguard`；
- `agentguard-console`。

### 2.4 登录控制台

打开 <http://localhost:8080>，然后从本地 `.env` 读取管理员令牌：

```bash
sed -n 's/^AGENTSHARK_ADMIN_TOKEN=//p' .env
```

将令牌粘贴到登录页面。不要把令牌复制到聊天记录、工单、截图或 CI 日志中。

登录时，服务端会把管理令牌交换为 `HttpOnly`、`SameSite=Strict` 的会话 Cookie。
令牌不会持久化到浏览器存储。页面重新加载后，前端会通过认证会话接口恢复新的内存
CSRF 值。

### 2.5 检查系统健康状态

登录后先打开 **System**。agentgateway 和 AgentGuard 两张来源卡片都应显示健康。

AgentsharkX 自身的进程存活接口为：

```bash
curl -fsS http://127.0.0.1:8080/healthz
```

预期返回：

```json
{ "status": "ok" }
```

`/healthz` 只表示 AgentsharkX 进程正在提供服务，不代表两个上游均健康。分别检查上游：

```bash
curl -fsS http://127.0.0.1:15021/healthz/ready

set -a
. ./.env
set +a
curl -fsS -H "X-Api-Key: $AGENTGUARD_ADMIN_TOKEN" \
  http://127.0.0.1:38080/v1/backend/health
```

也可以运行仓库提供的只读兼容性检查：

```bash
set -a
. ./.env
set +a
make upstream-smoke
```

## 3. 产生第一条真实 AgentGuard 事件

刚启动时，如果还没有 Agent 接入，Trust 和 Audit 没有业务数据属于正常现象。仓库
提供了一个不需要 LLM 或 Provider API Key 的最小示例。

创建一次性虚拟环境并安装固定版本的 AgentGuard：

```bash
python3 -m venv .venv-quickstart
.venv-quickstart/bin/pip install \
  'agentguard @ git+https://github.com/WhitzardAgent/AgentGuard.git@4b755fb4a4a2763b7e817b3d0220fe5c22187b59'
```

加载本地凭据并运行示例：

```bash
set -a
. ./.env
set +a
AGENTGUARD_SERVER_URL=http://127.0.0.1:38080 \
AGENTGUARD_API_KEY="$AGENTGUARD_ADMIN_TOKEN" \
  .venv-quickstart/bin/python examples/agentguard_minimal.py
```

通常在三秒内可以看到：

- **Audit → Security events** 出现带 AgentGuard 来源标签的工具事件；
- **Trust → Agents** 出现示例中显式声明的 Agent；
- Trust 中可以查看对应的工具或运行时资源。

示例不会把提示词、工具参数、会话密钥、API Key 或原始敏感响应返回给浏览器。

## 4. 工作区使用说明

### 4.1 Connect：连接和路由

Connect 汇总 agentgateway 中显式配置的：

- Providers 和 Models；
- MCP Servers；
- Listeners、Routes 和 Backends；
- 成本目录和来源能力状态。

建议操作顺序：

1. 打开 **Connect**，检查来源和能力状态；
2. 查看 Provider、Model、MCP 和 Route 列表；
3. 选择具体资源查看安全投影后的详情；
4. 需要编辑原始配置、CEL 或使用 Playground 时，通过深链接进入 agentgateway
   控制台。

agentgateway 管理控制台默认地址为 <http://127.0.0.1:15000/ui>。

页面右上角的 **Configure agentgateway** 会直接打开固定版本原生 Raw Configuration
编辑器。预览环境将明确的 `deploy/agentgateway/config.yaml` 以可写方式挂载，并把
agentgateway 容器的非 root UID/GID 对齐到该文件的所有者，因此可以通过原生编辑器
校验并保存；管理端口仍只绑定回环地址。Docker 的 `read-write` 挂载不会绕过 Unix
权限：镜像默认 UID `65532` 无法写入检出用户持有的 `0644` 文件。请始终使用
`make preview-up` 启动或重建服务。保存后返回 AgentsharkX，页面会在重新获得焦点时
刷新，也会按固定间隔同步。

可以执行下面的原生保存回归检查：

```bash
set -a
. ./.env
set +a
make gateway-config-write-smoke
```

该检查读取当前配置并通过原生 `POST /api/config` 原样写回；可能包含敏感信息的内容
只保存在权限为 `0600` 的临时文件中，不会输出到终端。检查脚本默认访问宿主机回环
端口，不会误用 `.env` 中仅供 Compose 网络解析的 `http://agentgateway:15000`。

默认预览配置没有业务监听器、Provider 凭据、真实业务路由和请求日志数据库，因此
Connect 或网关审计显示 `partial` 是预期行为。Provider API Key 应配置在
agentgateway 中，不能放入 AgentsharkX。

### 4.2 Trust：Agent 和运行时资源

Trust 展示 AgentGuard 明确上报的：

- Agents；
- Sessions；
- Tools；
- Skills；
- MCP Resources；
- Skill/MCP 检测任务和工具标签。

只有资源或会话中明确存在 `agent_id`、`owner_agent_id` 等身份字段时，AgentsharkX
才会构建 Agent 视图。系统不会从网关流量、时间接近性或显示名称推断 Agent。

典型流程：

1. 运行已经接入 AgentGuard 的 Agent；
2. 打开 **Trust → Agents**；
3. 选择 Agent，查看会话、工具、技能和 MCP；
4. 按需更新工具标签；
5. 发起 Skill 或 MCP 检测任务，并轮询真实任务状态。

检测进度不会被伪造为百分比。BFF 重启后，内存中的检测任务状态会丢失。

### 4.3 Protect：策略和防护

Protect 按来源保留两套不同的策略模型：

- agentgateway 的路由策略和内容 Guardrail 摘要；
- AgentGuard 的运行时规则、插件阶段和审批队列。

AgentsharkX 不会把两套模型翻译成虚构的统一策略 DSL。高级网关策略编辑继续由
agentgateway 控制台负责。

危险写操作需要：

- 操作备注；
- 明确二次确认；
- 当前会话的 CSRF Token；
- 服务端生成的 Request ID；
- 可审计的结果回执。

AgentGuard 写操作不会自动重试。如果发布、删除或审批操作超时，应先检查上游的
规则或工单状态，确认操作没有成功后再手动重试。

Protect 页面的 **Configure AgentGuard** 会打开配置的 AgentGuard 原生控制台。标签、
扫描、运行时规则和审批继续使用 AgentsharkX 已验证的写接口；其他未验证的配置不会
在 BFF 中伪造编辑能力。

### 4.4 Audit：流量和安全审计

Audit 独立汇总：

- agentgateway 请求日志和 Analytics；
- AgentGuard Traffic、Audit 和 Sessions；
- 可以通过完全一致标识确认的跨来源关联。

默认 agentgateway 配置没有请求日志数据库，因此缺少网关流量记录时，界面会显示
来源范围内的 `partial` 或错误，而不会伪装成“成功但没有数据”。如需网关历史记录，
应先在 agentgateway 中配置业务路由、Provider 和请求日志数据库。

AgentsharkX 只在两个来源提供完全相同且非空的 Trace ID 或 Session ID 时标记关联。
它不会按时间窗口猜测关联关系。

审计事件列表和 SSE 使用有界内存窗口，最多保留 1000 条事件，并支持恢复和去重。
BFF 重启后，事件窗口会清空。Audit 不请求网关日志的原始 Payload 或 Attributes，
事件详情仅返回白名单字段和脱敏投影。

### 4.5 System：状态和诊断

出现问题时优先查看 **System**。该页面会独立探测两个上游，并展示：

- 来源健康状态；
- 版本和能力状态；
- `supported`、`partial`、`link-out` 或 `unavailable` 状态；
- 针对当前来源的恢复建议。

诊断响应不会返回配置 URL、API Key、Authorization Header 或原始上游响应。
即使一个上游不可用，AgentsharkX 仍会启动，以便通过 System 展示恢复信息。

## 5. 接入真实 Agent

完整接入包含网关流量和运行时保护两条独立路径。

### 5.1 让模型流量经过 agentgateway

在 agentgateway 中显式创建业务监听器、Provider 和路由，然后把 Agent 使用的
OpenAI 兼容客户端或 MCP 客户端指向该业务监听器：

```text
Agent → agentgateway 业务监听器 → 路由/策略 → 模型或 MCP Provider
```

默认的 `15000` 是管理端口，不是预配置的模型业务端点。不要把管理 API 直接暴露到
公网；固定版本的 agentgateway 管理 API 没有经过验证的原生认证中间件。

### 5.2 将 AgentGuard 接入运行时

Agent 进程至少需要：

```bash
export AGENTGUARD_SERVER_URL=http://127.0.0.1:38080
export AGENTGUARD_API_KEY='<AgentGuard API Key>'
```

这里使用 AgentGuard API Key，也就是预览 `.env` 中
`AGENTGUARD_ADMIN_TOKEN` 对应的值，不能使用 `AGENTSHARK_ADMIN_TOKEN`。

固定 main 提交的 AgentGuard 提供的适配器包括：

- `attach_langchain`；
- `attach_langgraph`；
- `attach_autogen`；
- `attach_openai_agents`；
- `attach_llamaindex`。

接入时应保留 `agent_id`、`session_id`、`user_id` 等显式身份，并保留
`llm_before`、`llm_after`、`tool_before`、`tool_after` 等原始事件阶段。详细契约请参阅
[Agent integration](agent-integration.md) 和固定提交的 AgentGuard 文档。

### 5.3 验证接入结果

完成一次 Agent 运行后，依次确认：

1. **System** 中相关来源健康；
2. **Trust** 中出现 AgentGuard 明确上报的身份和资源；
3. **Audit → Security events** 中出现来源、阶段、动作和脱敏详情引用；
4. 已配置请求日志数据库时，**Audit → Traffic** 出现网关记录；
5. 只有共享标识完全一致时，跨来源事件才显示为已关联。

## 6. 日常运维

### 6.1 默认本地端点

| 服务                         | 地址                                       |
| ---------------------------- | ------------------------------------------ |
| AgentsharkX                  | <http://localhost:8080>                    |
| agentgateway 控制台/管理 API | <http://127.0.0.1:15000/ui>                |
| agentgateway Metrics         | <http://127.0.0.1:15020/metrics>           |
| agentgateway Readiness       | <http://127.0.0.1:15021/healthz/ready>     |
| AgentGuard API               | <http://127.0.0.1:38080/v1/backend/health> |
| AgentGuard 控制台            | <http://127.0.0.1:38008/>                  |

所有发布端口默认仅绑定回环地址。改变任何 `*_BIND` 配置之前，应先评估认证、TLS 和
网络边界。

### 6.2 查看日志

查看 AgentsharkX：

```bash
docker compose --env-file deploy/versions.env --env-file .env \
  -f deploy/compose.yaml logs -f agentshark
```

查看 agentgateway：

```bash
docker compose --env-file deploy/versions.env --env-file .env \
  -f deploy/compose.yaml logs -f agentgateway
```

查看 AgentGuard：

```bash
docker compose --env-file deploy/versions.env --env-file .env \
  -f deploy/compose.yaml logs -f agentguard
```

### 6.3 重启或停止

只重启 AgentsharkX：

```bash
docker compose --env-file deploy/versions.env --env-file .env \
  -f deploy/compose.yaml restart agentshark
```

停止整个预览环境：

```bash
make preview-down
```

停止后，`.env` 和 `.venv-quickstart` 仍保留在本地。事件窗口、检测任务和规则检查
令牌是内存状态，会随 BFF 停止而消失。

## 7. 本地开发

### 7.1 前端 Mock 模式

Mock 模式适合只开发或审查界面，不需要真实 BFF 和上游服务：

```bash
npm ci --prefix apps/web
npm --prefix apps/web run dev
```

打开 <http://127.0.0.1:5173>。顶部演示选择器可以切换正常、空数据、加载、部分失败
和完全失败状态。Mock 仅作为 UI 证据，不能证明真实上游能力可用。

### 7.2 本地真实 BFF

先启动固定版本上游，然后在一个终端运行 Go BFF：

```bash
export AGENTSHARK_LISTEN_ADDR=127.0.0.1:8080
export AGENTSHARK_ENVIRONMENT=local
export AGENTSHARK_ADMIN_TOKEN='replace-with-at-least-16-characters'
export AGENTSHARK_COOKIE_SECURE=false
export AGENTGATEWAY_BASE_URL=http://127.0.0.1:15000
export AGENTGATEWAY_CONSOLE_URL=http://127.0.0.1:15000/ui
export AGENTGUARD_BASE_URL=http://127.0.0.1:38080
export AGENTGUARD_ADMIN_TOKEN='replace-with-the-agentguard-api-key'
export AGENTGUARD_VERSION=main-4b755fb
export AGENTSHARK_SCAN_TIMEOUT=90s
export AGENTSHARK_POLL_INTERVAL=2s

cd apps/server
go run ./cmd/agentshark
```

另开一个终端，在仓库根目录启动连接真实 BFF 的前端：

```bash
VITE_ENABLE_MOCKS=false npm --prefix apps/web run dev
```

本地明文 HTTP 仅允许在显式 `local`/`development` 环境、回环监听器和
`AGENTSHARK_COOKIE_SECURE=false` 的组合下使用。生产部署必须保持
`AGENTSHARK_COOKIE_SECURE=true`，并在 BFF 前终止 HTTPS。

## 8. 验证和发布门禁

首次验证前安装前端依赖：

```bash
npm ci --prefix apps/web
```

运行基础验证：

```bash
make verify
```

基础验证包括 Go 格式、单元测试和 Race 检查，前端生成文件、格式、类型、单元测试和
构建，以及 Secret Boundary、仓库结构、OpenAPI 和 Compose 检查。

其他主要目标：

```bash
make release-e2e      # 运行真实 BFF 的完整浏览器流程
make container-build  # 构建生产容器
make security-scan    # 运行安全扫描
make sbom             # 生成 SPDX SBOM 和许可证资料
make release-gate     # 运行完整发布门禁
```

`make release-gate` 覆盖基础验证、Secret 扫描、SBOM、安全扫描、容器构建和完整真实
E2E。提交前还应运行：

```bash
git diff --check
```

## 9. 常见故障

### 9.1 登录返回 401

可能原因是管理员令牌错误、浏览器会话缺失或已过期。重新读取当前 `.env` 中的
`AGENTSHARK_ADMIN_TOKEN`，清除 `localhost:8080` 的旧 Cookie 后重新登录。

### 9.2 写操作返回 403 `CSRF_REQUIRED`

重新加载页面一次，确认 `GET /api/v1/auth/session` 返回 `204` 并包含
`X-CSRF-Token`。危险写操作不要连续重试。

### 9.3 agentgateway 不健康

1. 确认容器内的 `AGENTGATEWAY_BASE_URL` 指向管理监听器，而不是业务监听器；
2. 检查 <http://127.0.0.1:15021/healthz/ready> 是否返回 `ready`；
3. 查看 `agentgateway` 容器日志；
4. 加载 `.env` 后运行 `make upstream-smoke`。

### 9.4 AgentGuard 不健康

1. 确认 `AGENTGUARD_BASE_URL` 指向 API 的 `38080` 端口；
2. 确认 `AGENTGUARD_ADMIN_TOKEN` 与 AgentGuard 服务使用的
   `AGENTGUARD_API_KEY` 一致；
3. 查看 `agentguard` 容器日志；
4. 使用 `X-Api-Key` 请求 `/v1/backend/health`。未认证请求返回 `401` 属于预期行为。

执行 `make preview-status`，AgentGuard 镜像应为
`agentsharkx/agentguard:main-4b755fb`。该镜像固定到官方仓库 main 提交
`4b755fb4a4a2763b7e817b3d0220fe5c22187b59`，构建并启动独立 server/frontend，
与官方 `./scripts/start.sh` 的源代码构建方式一致，但不会使用不可复现的 `latest`
标签。该提交已经包含服务端 Thought-Aligner；它在官方配置中是可选插件，默认不会
自动启用，应通过 AgentGuard 自己的插件配置管理。

### 9.5 Raw Configuration 保存失败

如果原生编辑器保存时报 `Permission denied`：

1. 使用 `make preview-up` 重建 agentgateway，使 Compose wrapper 重新读取配置文件
   所有者 UID/GID；
2. 不要把配置文件修改为全局可写；
3. 加载 `.env` 后运行 `make gateway-config-write-smoke`；
4. 检查 `make preview-status` 和 agentgateway 日志。

### 9.6 Audit 没有网关流量

默认网关配置没有业务监听器和请求日志数据库。先在 agentgateway 中配置真实路由、
Provider 凭据和日志存储，再产生实际流量。不要把“没有配置日志数据库”理解为“成功
返回空记录”。

### 9.7 Trust 没有 Agent

确认 AgentGuard 已接入 Agent 运行时，使用了正确的服务 URL 和 API Key，并且至少
执行过一次运行时操作。同时确认接入代码显式提供 `agent_id` 和 `session_id`。

### 9.8 启动配置被拒绝

BFF 会拒绝：

- 空值、占位值或短于 16 个字符的 AgentsharkX/AgentGuard 令牌；
- 在非本地环境关闭认证；
- 在非本地或非回环环境关闭 Secure Cookie；
- `AGENTSHARK_REDACT_PAYLOADS=false`。

使用 `make preview-bootstrap` 创建安全的本地 `.env`，不要绕过校验。

## 10. 安全和数据边界

- `.env` 不得提交到 Git；
- AgentsharkX 管理令牌与 AgentGuard API Key 不能混用；
- Provider API Key 只保存在 agentgateway 或对应服务端 Secret Store；
- 生产环境必须使用 HTTPS 和 Secure Cookie；
- agentgateway 管理端口和 AgentGuard 管理 API 应位于私有管理网络；
- AgentsharkX 不请求或展示原始网关 Payload、工具参数、运行时结果和上游密钥；
- 高风险写操作超时后，先确认上游状态再手动重试；
- AgentsharkX、AgentGuard 和 agentgateway 是独立进程，升级时需要分别核对版本和
  兼容性。

## 11. 推荐操作顺序

日常启动：

```bash
make preview-up
make preview-status
```

随后：

1. 登录 <http://localhost:8080>；
2. 在 **System** 检查两个上游；
3. 启动真实 Agent 或运行最小示例；
4. 在 **Trust** 检查 Agent、Session、Tool、Skill 和 MCP；
5. 在 **Audit** 检查网关流量和 AgentGuard 安全事件；
6. 在 **Protect** 检查策略、规则、插件和审批；
7. 出现异常时先看 **System**，再查看对应服务日志。

开发提交前：

```bash
make verify
git diff --check
```

发布前：

```bash
make release-gate
```

## 12. 延伸阅读

- [10 分钟预览快速开始](quickstart.md)
- [Agent 接入说明](agent-integration.md)
- [架构说明](architecture.md)
- [能力矩阵](capability-matrix.md)
- [上游兼容性记录](upstream-compatibility.md)
- [故障排查](troubleshooting.md)
- [发布证据](release/README.md)
