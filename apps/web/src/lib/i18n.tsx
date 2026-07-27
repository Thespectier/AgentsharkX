import {
  Children,
  createContext,
  type ReactNode,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";

export type Locale = "en" | "zh-CN";

const localeStorageKey = "agentshark.locale";

const zhCN: Record<string, string> = {
  "CONTROL PLANE": "控制平面",
  Environment: "环境",
  Connecting: "连接中",
  "Environment health": "环境健康状态",
  Workspaces: "工作区",
  Home: "首页",
  Connect: "连接",
  Overview: "概览",
  Traffic: "流量",
  Setup: "设置",
  Trust: "信任",
  Agents: "智能体",
  Resources: "资源",
  Scans: "扫描",
  Protect: "防护",
  Policies: "策略",
  Guardrails: "护栏",
  "Runtime rules": "运行时规则",
  Plugins: "插件",
  Approvals: "审批",
  Audit: "审计",
  Analytics: "分析",
  "Security events": "安全事件",
  Sessions: "会话",
  System: "系统",
  Documentation: "文档",
  Collapse: "折叠",
  "Expand sidebar": "展开侧边栏",
  "Collapse sidebar": "折叠侧边栏",
  "Open navigation": "打开导航",
  "Close navigation": "关闭导航",
  "Primary navigation": "主导航",
  "Search or jump to commands": "搜索或跳转到命令",
  "Search or jump to…": "搜索或跳转到…",
  "MOCK DATA": "模拟数据",
  "LIVE BFF": "实时 BFF",
  "Live mock": "实时模拟",
  "No data": "无数据",
  Loading: "加载中",
  "Partial failure": "部分失败",
  "Total failure": "完全失败",
  "Demo state": "演示状态",
  "Upstream health": "上游健康状态",
  Sources: "数据源",
  "Last 60m": "最近 60 分钟",
  "Data window: last 60 minutes": "数据窗口：最近 60 分钟",
  "Switch to Chinese": "切换到中文",
  "Switch to English": "切换到英文",
  "Open system settings": "打开系统设置",
  "{count} pending approvals": "{count} 项待审批",
  sections: "子页面",
  "scroll region": "滚动区域",
  Previous: "上一页",
  Next: "下一页",
  total: "总计",
  Status: "状态",
  Provider: "提供商",
  Kind: "类型",
  "Explicit references": "显式引用",
  Model: "模型",
  "Provider / routing": "提供商 / 路由",
  "MCP server": "MCP 服务器",
  Transport: "传输",
  Scope: "范围",
  Route: "路由",
  Protocol: "协议",
  Listener: "监听器",
  Backends: "后端",
  Framework: "框架",
  "Last active": "最后活跃",
  Resource: "资源",
  Type: "类型",
  "Labels / detector": "标签 / 检测器",
  Action: "操作",
  Policy: "策略",
  Phase: "阶段",
  Manage: "管理",
  Agent: "智能体",
  "Raw reference": "原始引用",
  "Upstream ID": "上游 ID",
  "Trust level": "信任级别",
  "Original ID": "原始 ID",
  "Raw detail reference": "原始详情引用",
  Verified: "已验证",
  None: "无",
  records: "条记录",

  "Home / Runtime posture": "首页 / 运行态势",
  "Home / First run": "首页 / 首次运行",
  "Home / Phase 2 foundation": "首页 / 第二阶段基础",
  "Control plane unavailable": "控制平面不可用",
  "Bring your control plane online": "让控制平面上线",
  "Management planes connected": "管理平面已连接",
  "Source-scoped health, gateway traffic, runtime decisions, and actions requiring human attention.":
    "按来源展示健康状态、网关流量、运行时决策和需要人工处理的操作。",
  "Connect both management planes and send one request before AgentsharkX renders operational charts.":
    "连接两个管理平面并发送一次请求后，AgentsharkX 才会展示运行图表。",
  "The secure BFF is connected and preserving each upstream's independent health and capability state.":
    "安全 BFF 已连接，并独立保留每个上游的健康和能力状态。",
  "Three-step setup": "三步设置",
  "From zero to the first verified event": "从零开始获取首个已验证事件",
  "No empty charts and no invented traffic. The console activates each surface only after its source responds.":
    "不展示空图表，也不虚构流量；只有数据源响应后才会启用对应界面。",
  "Open setup": "打开设置",
  "Credentials stay in the BFF": "凭据仅保留在 BFF",
  "Secure health stream": "安全健康流",
  "Health events are normalized without collecting prompts, authorization headers, or raw upstream payloads.":
    "健康事件会被标准化，但不会收集提示词、授权头或原始上游载荷。",
  "SSE connected": "SSE 已连接",
  "Connecting to SSE": "正在连接 SSE",
  "Heartbeat and source health changes only": "仅包含心跳和数据源健康变化",
  "No fabricated operations": "不虚构操作",
  "Traffic, decisions, approvals, and audit arrays remain empty until their verified integration phases.":
    "流量、决策、审批和审计数据在完成验证接入前保持为空。",
  "BFF boundary active": "BFF 边界已启用",
  "Use System to inspect live capability probes": "在系统页面查看实时能力探测",
  "View security events": "查看安全事件",
  "Review approvals": "处理审批",
  "Probe timed out · cached view": "探测超时 · 显示缓存视图",
  Fetched: "获取于",
  Requests: "请求数",
  Denied: "拒绝数",
  "Active sessions": "活跃会话",
  "Pending approvals": "待审批项",
  "Last 60 minutes": "最近 60 分钟",
  "31 explicit AgentGuard agents": "31 个显式 AgentGuard 智能体",
  "0.20% of evaluated actions": "占已评估操作的 0.20%",
  "Awaiting human review": "等待人工审核",
  "Across configured providers": "覆盖已配置提供商",
  "Gateway requests": "网关请求",
  "Runtime findings": "运行时发现",
  "Connect agentgateway": "连接 agentgateway",
  "Connect AgentGuard": "连接 AgentGuard",
  "Send a verification request": "发送验证请求",
  "Traffic & decisions": "流量与决策",
  "Exact rolling 60 minutes in 5-minute Beijing-time buckets; requests and explicit denies use independent axes.":
    "按北京时间展示精确滚动 60 分钟的 5 分钟桶；请求数和明确拒绝使用独立坐标轴。",
  "Security queue": "安全队列",
  "Latest deny, human-check, and audit findings.": "最新拒绝、人工检查和审计发现。",
  "Open queue": "打开队列",
  "No recent security findings": "近期没有安全发现",
  "Source-scoped health, live gateway traffic, runtime decisions, and actions requiring human attention.":
    "按来源展示健康状态、实时网关流量、运行时决策和需要人工处理的操作。",
  "Unified presentation only. No task or time-window correlation is implied.":
    "仅提供统一展示，不代表任务关联或时间窗口关联。",
  "This view is driven by the labelled Phase 1 fixture.":
    "此视图由明确标注的第一阶段夹具数据驱动。",
  "This view is driven by authenticated Phase 2 BFF responses.":
    "此视图由经过身份验证的第二阶段 BFF 响应驱动。",
  "No security events in this range.": "此时间范围内没有安全事件。",
  "Mock SSE connected": "模拟 SSE 已连接",
  "Live SSE connected": "实时 SSE 已连接",
  "Dynamic elements on this page are driven by clearly labelled MSW REST and SSE fixtures.":
    "此页面的动态元素由明确标注的 MSW REST 与 SSE 夹具数据驱动。",
  "Dynamic elements on this page are driven by authenticated BFF REST and SSE responses.":
    "此页面的动态元素由经过身份验证的 BFF REST 与 SSE 响应驱动。",
  "Live control plane": "实时控制平面",
  "Agent traffic & decisions": "智能体流量与决策",
  "Live agent traffic topology": "实时智能体流量拓扑",
  "Gateway traffic": "网关流量",
  "Guard decisions": "防护决策",
  "Deny rate": "拒绝率",
  "Last 60 minutes · direct DENY and denied approvals": "最近 60 分钟 · 直接拒绝与已拒绝审批",
  "Resumable SSE · no inferred correlation": "可恢复 SSE · 不推断关联",
  requests: "请求数",
  latency: "延迟",
  security: "安全事件",
  "{mode} trend chart for the last 60 minutes in {count} five-minute Beijing-time buckets":
    "最近 60 分钟的{mode}趋势图，共 {count} 个北京时间 5 分钟桶",

  "Connect / agentgateway": "连接 / agentgateway",
  "Gateway data unavailable": "网关数据不可用",
  "Connect agents to every destination": "将智能体连接到每个目标",
  "Verified agentgateway configuration and traffic surfaces. Advanced editing stays in the native console.":
    "展示已验证的 agentgateway 配置和流量界面；高级编辑仍在原生控制台中完成。",
  "Connection status": "连接状态",
  "Runtime and configuration were checked independently by the BFF.":
    "BFF 已分别检查运行状态和配置。",
  "Analytics summary": "分析摘要",
  "Last 60 minutes, derived only from the verified request-log analytics contract.":
    "最近 60 分钟，仅来自已验证的请求日志分析契约。",
  "Total tokens": "Token 总数",
  "Estimated cost": "预估成本",
  Buckets: "时间桶",
  Unavailable: "不可用",
  "Version unavailable": "版本不可用",
  Providers: "提供商",
  "Providers explicitly present in agentgateway configuration.":
    "agentgateway 配置中明确存在的提供商。",
  Models: "模型",
  "Direct and virtual models remain explicitly distinguished.": "直接模型和虚拟模型保持明确区分。",
  "MCP federation": "MCP 联邦",
  "Gateway MCP targets remain distinct from AgentGuard resources.":
    "网关 MCP 目标与 AgentGuard 资源保持区分。",
  "Listeners & routes": "监听器与路由",
  "HTTP and TCP routes from explicit configuration fields.":
    "来自显式配置字段的 HTTP 和 TCP 路由。",
  "Filter explicit resources": "筛选显式资源",
  "No matching resources": "没有匹配的资源",
  "No explicit upstream resources match this query.": "没有显式上游资源匹配此查询。",
  "Management verification": "管理面验证",
  "Live BFF checks against /api/runtime and /api/config.":
    "BFF 对 /api/runtime 和 /api/config 执行实时检查。",
  "Connection verified": "连接已验证",
  "Configuration unreadable": "配置不可读取",
  "Native console tools": "原生控制台工具",
  "Advanced editors stay in the pinned agentgateway console.":
    "高级编辑器保留在固定版本的 agentgateway 控制台中。",
  "Open in agentgateway": "在 agentgateway 中打开",
  "Use upstream-native tools for advanced editing and testing.":
    "使用上游原生工具进行高级编辑和测试。",
  "Configure agentgateway": "配置 agentgateway",
  "Run check": "运行检查",
  "Raw Config": "原始配置",
  "Resource detail": "资源详情",
  "No validated console URL is configured.": "未配置经过验证的控制台 URL。",
  "Analytics is unavailable.": "分析功能不可用。",
  "Manage verified LLM providers and direct models here. Advanced routing and policy editing stay in the native console.":
    "在此管理已验证的 LLM 提供商和直连模型；高级路由和策略编辑仍在原生控制台中完成。",
  "Loading LLM configuration...": "正在加载 LLM 配置...",
  Credential: "凭据",
  Actions: "操作",
  Visibility: "可见性",
  Routing: "路由",
  Targets: "目标",
  "Add provider": "添加提供商",
  "Edit provider": "编辑提供商",
  "Delete provider": "删除提供商",
  "Provider is referenced by a model": "该提供商正被模型引用",
  "Shared providers and credential references from verified agentgateway fields.":
    "管理来自已验证 agentgateway 字段的共享提供商和凭据引用。",
  "Reusable provider credentials, cloud authentication, and connection settings.":
    "管理可复用的提供商凭据、云身份验证和连接设置。",
  "Filter providers": "筛选提供商",
  "{count} shown": "显示 {count} 项",
  "LLM provider settings": "LLM 提供商设置",
  "No provider matches this filter.": "没有提供商符合当前筛选条件。",
  "No providers found": "未找到提供商",
  "Incoming model": "沿用传入模型",
  "Strip prefix": "移除前缀",
  "Custom CEL": "自定义 CEL",
  "Add model": "添加模型",
  "Edit model": "编辑模型",
  "Delete model": "删除模型",
  "Model is referenced by a virtual model": "该模型正被虚拟模型引用",
  "Direct model aliases and provider bindings. Provider binding is fixed after creation.":
    "管理直连模型别名和提供商绑定；提供商绑定在创建后不可更改。",
  "Direct model aliases, provider bindings, and outgoing model transformations.":
    "管理直连模型别名、提供商绑定和出站模型转换。",
  "Direct models": "直连模型",
  "Filter models": "筛选模型",
  "Direct LLM model settings": "直连 LLM 模型设置",
  "No direct model matches this filter.": "没有直连模型符合当前筛选条件。",
  "No models found": "未找到模型",
  "Virtual model": "虚拟模型",
  "Not provided": "未提供",
  "Virtual routing, failover, and policy fields remain read-only here.":
    "虚拟路由、故障转移和策略字段在此保持只读。",
  "Virtual models": "虚拟模型",
  "Virtual LLM models": "虚拟 LLM 模型",
  "agentgateway has no virtual models configured.": "agentgateway 尚未配置虚拟模型。",
  "No virtual models": "没有虚拟模型",
  Edit: "编辑",
  Configured: "已配置",
  "Ambient / none": "运行环境 / 无",
  "Verified provider and direct-model settings are written while unowned agentgateway fields are preserved.":
    "写入已验证的提供商和直连模型设置，同时保留不归当前界面管理的 agentgateway 字段。",
  "Provider name": "提供商名称",
  "Provider type": "提供商类型",
  "Default model": "默认模型",
  "Base URL": "基础 URL",
  "Optional override": "可选覆盖值",
  "Model name": "模型名称",
  "Provider mode": "提供商模式",
  "Shared provider": "共享提供商",
  "Target model": "目标模型",
  "Optional upstream model override": "可选的上游模型覆盖值",
  "Save provider": "保存提供商",
  "Save model": "保存模型",
  "Credentials are inherited from the shared provider reference.": "凭据继承自共享提供商引用。",
  "AWS region": "AWS 区域",
  "Vertex project": "Vertex 项目",
  "Vertex region": "Vertex 区域",
  "Azure resource name": "Azure 资源名称",
  "Azure resource type": "Azure 资源类型",
  "Azure API version": "Azure API 版本",
  "Azure project name": "Azure 项目名称",
  "Authentication source": "身份验证来源",
  "Authentication is configured upstream. Its value is not exposed.":
    "身份验证已在上游配置，其值不会被暴露。",
  "Authentication is configured upstream as {kind}. Its value is not exposed.":
    "上游已配置为{kind}，具体值不会被暴露。",
  "Use ambient cloud identity here; structured cloud credentials remain native-console only.":
    "此处使用运行环境中的云身份；结构化云凭据仍仅能在原生控制台中管理。",
  "No credential value is exposed by agentgateway.": "agentgateway 不会暴露任何凭据值。",
  "Credential mode": "凭据模式",
  "Environment variable": "环境变量",
  "Credential file": "凭据文件",
  "Provider API key": "提供商 API Key",
  "API key": "API Key",
  "AWS access key ID": "AWS Access Key ID",
  "AWS secret access key": "AWS Secret Access Key",
  "AWS session token": "AWS 会话令牌",
  "Google credential file": "Google 凭据文件",
  "Azure managed identity client ID": "Azure 托管身份客户端 ID",
  "Optional user-assigned client ID": "可选的用户分配身份客户端 ID",
  Optional: "可选",
  "Show secret": "显示密钥",
  "Hide secret": "隐藏密钥",
  "Tokenize requests before forwarding": "转发前对请求进行分词",
  "Outgoing model": "出站模型",
  "Explicit outgoing model": "明确指定出站模型",
  "Model CEL expression": "模型 CEL 表达式",
  "Custom API formats": "自定义 API 格式",
  "Select at least one format. Paths are optional overrides and must start with /.":
    "请至少选择一种格式；路径为可选覆盖值，且必须以 / 开头。",
  "Default path": "默认路径",
  preserve: "保留现有凭据",
  ambient: "运行环境",
  environment: "环境变量引用",
  literal: "明文 API Key",
  file: "文件引用",
  "aws-static": "AWS 静态凭据",
  "gcp-file": "GCP 凭据文件",
  "azure-managed-identity": "Azure 托管身份",
  other: "其他凭据",
  reference: "共享提供商",
  builtin: "内置提供商",
  custom: "自定义",
  incoming: "沿用传入模型",
  explicit: "明确指定",
  strip: "移除前缀",
  public: "公开",
  internal: "内部",
  "The configuration revision is unavailable. Refresh and retry.":
    "配置修订信息不可用，请刷新后重试。",
  "This removes the item from agentgateway configuration after a fresh revision check.":
    "重新检查配置修订后，将从 agentgateway 配置中删除此项。",
  "Delete {name}": "删除 {name}",
  "References are checked again by the server before agentgateway is updated.":
    "更新 agentgateway 前，服务器会再次检查引用关系。",
  "I confirm that {name} should be deleted.": "我确认应删除 {name}。",
  request: "请求",
  "LLM provider created": "LLM 提供商已创建",
  "LLM provider updated": "LLM 提供商已更新",
  "LLM provider deleted": "LLM 提供商已删除",
  "LLM model created": "LLM 模型已创建",
  "LLM model updated": "LLM 模型已更新",
  "LLM model deleted": "LLM 模型已删除",
  "Provider created in agentgateway.": "已在 agentgateway 中创建提供商。",
  "Provider updated in agentgateway.": "已在 agentgateway 中更新提供商。",
  "Provider deleted from agentgateway.": "已从 agentgateway 中删除提供商。",
  "Model created in agentgateway.": "已在 agentgateway 中创建模型。",
  "Model updated in agentgateway.": "已在 agentgateway 中更新模型。",
  "Model deleted from agentgateway.": "已从 agentgateway 中删除模型。",

  "Trust / AgentGuard context": "信任 / AgentGuard 上下文",
  "Know what every agent can reach": "了解每个智能体可访问的资源",
  "Inspect only identities and resources reported explicitly by AgentGuard. Missing identity facts remain unknown.":
    "仅检查 AgentGuard 明确上报的身份和资源；缺失的身份事实保持未知。",
  "Identity boundary": "身份边界",
  "No trust level is inferred when AgentGuard omits it.":
    "AgentGuard 未提供信任级别时不会进行推断。",
  "Observed agents": "已观测智能体",
  "Select an agent to inspect its explicit sessions and resources.":
    "选择智能体以查看其明确上报的会话和资源。",
  "Runtime resources": "运行时资源",
  "Tools, Skills, and MCP resources share a view but retain their upstream type, owner, IDs, and raw references.":
    "工具、技能和 MCP 资源共用一个视图，但保留各自的上游类型、所有者、ID 和原始引用。",
  "Filter explicit Trust data": "筛选显式信任数据",
  "All resource types": "全部资源类型",
  Tools: "工具",
  Skills: "技能",
  "No resources reported": "未上报资源",
  "No explicit AgentGuard resources match this query.": "没有显式 AgentGuard 资源匹配此查询。",
  "Edit labels": "编辑标签",
  "Edit labels for {name}": "编辑 {name} 的标签",
  "Scan {name}": "扫描 {name}",
  "Save labels": "保存标签",
  "Saving labels…": "正在保存标签…",
  "Tool labels": "工具标签",
  Boundary: "边界",
  Sensitivity: "敏感度",
  Integrity: "完整性",
  "Tags (comma separated)": "标签（逗号分隔）",
  "No scans available": "暂无扫描",
  "Trigger a Skill or MCP scan from Resources. The BFF retains only a bounded in-memory job history.":
    "请从资源页面触发 Skill 或 MCP 扫描；BFF 仅保留有界的内存任务历史。",
  "Detection running": "检测进行中",
  "Detection failed": "检测失败",
  "Detection succeeded": "检测成功",
  "Retry scan": "重试扫描",
  Scan: "扫描",
  "Resource type": "资源类型",
  "Filter Trust resources": "筛选信任资源",
  "Loading explicit workspace facts…": "正在加载显式工作区信息…",
  "No explicit sessions.": "没有显式会话。",
  "No explicit resources.": "没有显式资源。",
  "Unknown principal": "未知主体",
  "Not scanned": "未扫描",
  "Read-only": "只读",
  "Server-reported state · no synthetic percentage": "服务器上报状态 · 不虚构百分比",
  "AgentGuard did not complete the scan.": "AgentGuard 未完成扫描。",
  "Retrying…": "正在重试…",
  "Creating scan job…": "正在创建扫描任务…",
  "AgentGuard detection is still running. It will continue on the server if you leave this view.":
    "AgentGuard 检测仍在运行；离开此视图后任务仍会在服务器端继续。",
  "Every row originates from an explicit AgentGuard agent_id. This is contextual metadata, not remote attestation or cryptographic identity.":
    "每一行都来自明确的 AgentGuard agent_id；这是上下文元数据，并非远程证明或密码学身份。",

  "Protect / Policies & intervention": "防护 / 策略与干预",
  "Protect / Controls": "防护 / 控制",
  "Enforce every critical boundary": "守护每个关键边界",
  "Source, scope, phase, and action stay explicit; gateway and runtime policy models are never merged into a synthetic DSL.":
    "来源、范围、阶段和动作均保持明确；网关策略与运行时策略绝不会合并为虚构 DSL。",
  "Gateway controls": "网关控制",
  "Runtime controls": "运行时控制",
  "Content guardrails": "内容护栏",
  "Advanced configuration": "高级配置",
  "A successful AgentGuard syntax check creates a short-lived, source-bound, one-use publish token.":
    "AgentGuard 语法检查成功后会生成短期、源码绑定且仅可使用一次的发布令牌。",
  "New rule": "新建规则",
  "Publish runtime rule": "发布运行时规则",
  "Check exactly one rule, add an operator note, then explicitly confirm publication. Rule source is never written to audit logs.":
    "请检查且仅检查一条规则，填写操作备注并明确确认发布；规则源码不会写入审计日志。",
  "Explicit AgentGuard agent": "明确的 AgentGuard 智能体",
  "Rule source": "规则源码",
  "Check syntax": "检查语法",
  "Checked and publishable": "检查通过，可以发布",
  "Not publishable": "不可发布",
  "Check required before publish": "发布前必须检查",
  "Operator note": "操作备注",
  "I confirm this checked rule should be published to the selected agent.":
    "我确认将已检查规则发布到所选智能体。",
  Cancel: "取消",
  "Publish checked rule": "发布已检查规则",
  "Delete runtime rule": "删除运行时规则",
  "Deletion note": "删除备注",
  "I confirm this runtime rule should be deleted.": "我确认删除此运行时规则。",
  "Delete rule": "删除规则",
  "Configure AgentGuard": "配置 AgentGuard",
  "Approval queue is clear": "审批队列为空",
  "Pending review": "待处理",
  "Operator decisions stay explicit": "操作员决策保持明确",
  "Review approval": "审核审批",
  Deny: "拒绝",
  Approve: "批准",
  "Retry deny": "重试拒绝",
  "Retry approve": "重试批准",
  Delete: "删除",
  "Use the native policy editor": "使用原生策略编辑器",
  "Raw config, CEL, credentials, and vendor-specific bodies are intentionally not duplicated.":
    "原始配置、CEL、凭据和供应商专用内容不会在此重复保存。",
  "Open agentgateway": "打开 agentgateway",
  "Deletion is limited to a currently reported user-managed AgentGuard rule.":
    "仅允许删除当前已上报且由用户管理的 AgentGuard 规则。",
  "Source, runtime phase, rule matches, and risk remain visible.":
    "数据源、运行阶段、规则匹配和风险始终可见。",
  "Duplicate clicks are disabled while a decision is pending.": "决策处理期间会禁用重复点击。",
  "Receipts include the BFF request ID for audit lookup.":
    "操作回执包含用于审计查询的 BFF 请求 ID。",

  "Audit / Traffic & security": "审计 / 流量与安全",
  "Audit / Evidence": "审计 / 证据",
  "See every verified signal": "查看每个已验证信号",
  "Analyze gateway traffic and runtime security evidence without inventing task-level correlation.":
    "分析网关流量和运行时安全证据，不虚构任务级关联。",
  Filter: "筛选",
  "Search events": "搜索事件",
  "Summary, agent, model, or resource": "摘要、智能体、模型或资源",
  Source: "来源",
  Severity: "严重程度",
  "All sources": "全部来源",
  "All severities": "全部严重程度",
  "Traffic trend": "流量趋势",
  "Latency trend": "延迟趋势",
  "Last 60 minutes; verified request volume and explicit denies use independent axes.":
    "最近 60 分钟；已验证请求量和明确拒绝使用独立坐标轴。",
  "Nearest-rank P95 from the bounded redacted request-log sample; tooltips show sample size and gaps mean no samples.":
    "基于有界脱敏请求日志样本计算 nearest-rank P95；提示框显示样本量，缺口表示没有样本。",
  "Unified activity": "统一活动",
  "Runtime sessions": "运行时会话",
  "No sessions found": "未找到会话",
  Timestamp: "时间戳",
  "Event type": "事件类型",
  Summary: "摘要",
  "Agent / target": "智能体 / 目标",
  Decision: "决策",
  Correlation: "关联",
  Session: "会话",
  Principal: "主体",
  Events: "事件",
  Denies: "拒绝",
  "Last seen": "最后出现",
  "Not reported": "未上报",
  "Event detail": "事件详情",
  "Event not found": "未找到事件",
  "Gateway request evidence": "网关请求证据",
  "Guard evidence": "防护证据",
  "Sensitive content boundary": "敏感内容边界",
  "agentgateway source log": "agentgateway 源日志",
  "Prompt / completion": "提示词 / 补全内容",
  "Retained upstream and available in the source log": "上游已留存，可在源日志中查看",
  "Not retained upstream for this event": "此事件未在上游留存相关内容",
  "Open exact source log": "打开精确匹配的源日志",
  "Started at": "开始时间",
  "Completed at": "完成时间",
  Duration: "持续时间",
  "HTTP status": "HTTP 状态",
  Operation: "操作类型",
  "Request model": "请求模型",
  "Response model": "响应模型",
  "Input tokens": "输入 Token",
  "Output tokens": "输出 Token",
  "Trace ID": "Trace ID",
  "Span ID": "Span ID",
  "Error present": "存在错误",
  "Guard event ID": "防护事件 ID",
  "Guard event type": "防护事件类型",
  Tool: "工具",
  "Framework source": "框架来源",
  "MCP tool": "MCP 工具",
  "Risk score": "风险分数",
  "Matched rules": "命中规则",
  "Rule version": "规则版本",
  "Resolved at": "处理时间",
  Prompt: "提示词",
  Payload: "载荷",
  Authorization: "授权信息",
  "Tool arguments": "工具参数",
  "Not collected by AgentsharkX": "AgentsharkX 不采集",
  "Retained upstream; content not retrieved": "上游已留存；AgentsharkX 未读取内容",
  "Credential values are never collected": "凭据值永不采集",
  Yes: "是",
  No: "否",
  "AgentsharkX keeps its BFF projection redacted. Prompt or completion content retained upstream is available through the exact agentgateway source log and is not copied into this API.":
    "AgentsharkX 的 BFF 投影保持脱敏；上游留存的提示词或补全内容可通过精确匹配的 agentgateway 源日志查看，不会复制到此 API。",
  "No request samples": "没有请求样本",
  "P95 latency": "P95 延迟",
  "5-minute bucket": "5 分钟桶",
  samples: "个样本",
  "Reset filters": "重置筛选",
  "No gateway traffic or AgentGuard security records exist in this time range.":
    "此时间范围内没有网关流量或 AgentGuard 安全记录。",
  "No audit data yet": "暂无审计数据",
  "Select a record for redacted detail. Source IDs remain intact.":
    "选择记录以查看脱敏详情；数据源 ID 保持不变。",
  "No records from this source are present in the selected time range.":
    "所选时间范围内没有来自该数据源的记录。",
  "AgentGuard sessions only; counts use exact session-ID matches and do not imply a task DAG.":
    "仅显示 AgentGuard 会话；计数基于精确会话 ID 匹配，不代表任务 DAG。",
  "AgentGuard has not reported any runtime sessions.": "AgentGuard 尚未上报运行时会话。",
  "Redacted raw JSON": "脱敏原始 JSON",

  "System / Diagnostics": "系统 / 诊断",
  "Sources, versions & capabilities": "数据源、版本与能力",
  "Diagnostics support the four product workspaces; System is not a fifth capability layer.":
    "诊断功能服务于四个产品工作区；系统页面不是第五个能力层。",
  "Standalone management plane": "独立运行的管理平面",
  "Runtime security control plane": "运行时安全控制平面",
  "No latency sample": "无延迟样本",
  "Live management probe": "实时管理探测",
  Checked: "检查于",
  "Troubleshooting guide": "故障排查指南",
  "Capability registry": "能力注册表",
  "The UI hides, disables, or links out according to this registry. Version numbers alone never enable a feature.":
    "界面根据此注册表隐藏、禁用或跳转；仅凭版本号不会启用功能。",
  "Probe based": "基于探测",

  "Partial data": "部分数据",
  "Loading console data": "正在加载控制台数据",
  "This view could not be loaded": "无法加载此视图",
  Retry: "重试",
  Unknown: "未知",
  healthy: "健康",
  degraded: "降级",
  down: "不可用",
  connecting: "连接中",
  supported: "支持",
  partial: "部分支持",
  unavailable: "不可用",
  "read-only": "只读",
  published: "已发布",
  configured: "已配置",
  pending: "待处理",
  critical: "严重",
  high: "高",
  medium: "中",
  low: "低",
  info: "信息",
  steady: "稳定",
  "Close dialog": "关闭弹窗",
  "Close drawer": "关闭抽屉",
  "Close detail drawer": "关闭详情抽屉",
  "Return home": "返回首页",
  "Workspace not found": "未找到工作区",
  "This route is not part of the Connect, Trust, Protect, Audit, or System information architecture.":
    "此路由不属于连接、信任、防护、审计或系统信息架构。",

  "Open Home": "打开首页",
  "Open Connect": "打开连接",
  "Open Trust": "打开信任",
  "Open Protect": "打开防护",
  "Open Audit": "打开审计",
  "Open System": "打开系统",
  "Runtime posture": "运行态势",
  "Gateway resources": "网关资源",
  "Agents and resources": "智能体与资源",
  "Policies and approvals": "策略与审批",
  "Traffic and security events": "流量与安全事件",
  "Capabilities and sources": "能力与数据源",
  "Show pending approvals": "显示待审批项",
  "need review": "需要审核",
  "No matching command": "没有匹配的命令",
  Navigate: "导航",
  Close: "关闭",
  "Mock console": "模拟控制台",
  "Close command palette": "关闭命令面板",
  "Command palette": "命令面板",
  "Search commands": "搜索命令",
  "Jump to a workspace or action…": "跳转到工作区或操作…",

  "AgentsharkX / Admin session": "AgentsharkX / 管理员会话",
  "Unlock the control plane": "解锁控制平面",
  "The token is exchanged once for a strict browser session. It is never stored in local storage or exposed to upstream services.":
    "令牌仅用于换取严格的浏览器会话，绝不会存入本地存储或暴露给上游服务。",
  "Administrator token": "管理员令牌",
  "Creating session…": "正在创建会话…",
  "Continue securely": "安全继续",
  "Checking the control-plane session…": "正在检查控制平面会话…",
};

type Variables = Record<string, string | number>;

function interpolate(message: string, variables?: Variables): string {
  if (!variables) return message;
  return Object.entries(variables).reduce(
    (result, [key, value]) => result.replaceAll(`{${key}}`, String(value)),
    message,
  );
}

export function translate(message: string, locale: Locale, variables?: Variables): string {
  const translated = locale === "zh-CN" ? (zhCN[message] ?? message) : message;
  return interpolate(translated, variables);
}

type I18nValue = {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  toggleLocale: () => void;
  t: (message: string, variables?: Variables) => string;
};

const I18nContext = createContext<I18nValue>({
  locale: "en",
  setLocale: () => undefined,
  toggleLocale: () => undefined,
  t: (message, variables) => interpolate(message, variables),
});

function initialLocale(): Locale {
  const stored = localStorage.getItem(localeStorageKey);
  return stored === "zh-CN" ? "zh-CN" : "en";
}

export function I18nProvider({ children }: { children: ReactNode }) {
  const [locale, setLocale] = useState<Locale>(initialLocale);
  useEffect(() => {
    localStorage.setItem(localeStorageKey, locale);
    document.documentElement.lang = locale;
  }, [locale]);
  const value = useMemo<I18nValue>(
    () => ({
      locale,
      setLocale,
      toggleLocale: () => setLocale((current) => (current === "en" ? "zh-CN" : "en")),
      t: (message, variables) => translate(message, locale, variables),
    }),
    [locale],
  );
  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nValue {
  return useContext(I18nContext);
}

export function localizeChildren(children: ReactNode, t: I18nValue["t"]): ReactNode {
  return Children.map(children, (child) => {
    if (typeof child !== "string") return child;
    const leading = child.match(/^\s*/)?.[0] ?? "";
    const trailing = child.match(/\s*$/)?.[0] ?? "";
    const content = child.trim();
    return content ? `${leading}${t(content)}${trailing}` : child;
  });
}

function beijingHour(date: Date): number {
  const hour = new Intl.DateTimeFormat("en-US", {
    hour: "2-digit",
    hourCycle: "h23",
    timeZone: "Asia/Shanghai",
  })
    .formatToParts(date)
    .find((part) => part.type === "hour")?.value;
  return Number(hour ?? 0);
}

export function getBeijingGreeting(date: Date, locale: Locale): string {
  const hour = beijingHour(date);
  if (locale === "zh-CN") {
    if (hour < 5) return "夜深了，您的智能体仍在掌控之中。";
    if (hour < 12) return "早上好。您的智能体均在掌控之中。";
    if (hour < 18) return "下午好。您的智能体均在掌控之中。";
    return "晚上好。您的智能体均在掌控之中。";
  }
  if (hour < 5) return "It's late. Your agents are still in control.";
  if (hour < 12) return "Good morning. Your agents are in control.";
  if (hour < 18) return "Good afternoon. Your agents are in control.";
  return "Good evening. Your agents are in control.";
}

export function useBeijingGreeting(): string {
  const { locale } = useI18n();
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), 60_000);
    return () => window.clearInterval(timer);
  }, []);
  return getBeijingGreeting(now, locale);
}
