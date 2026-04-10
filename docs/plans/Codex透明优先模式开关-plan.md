# Codex 透明优先模式开关实施计划

## 1. 计划目标

在后端 `cliproxy` 与前端 `Cli-Proxy-API-Management-Center` 中新增一个全局 `Codex transparent-first mode` 开关，且第一版仅作用于 `Codex Responses WebSocket`。

## 2. 实施范围

### 2.1 后端仓库

- 工作区：[cliproxy](/C:/Users/11252/Desktop/cliproxy)

### 2.2 前端仓库

- 工作区：`C:\Users\11252\Desktop\Cli-Proxy-API-Management-Center`

## 3. 方案总览

整体方案分为两部分：

1. 后端增加配置项、管理 API 接口和 websocket 行为分支。
2. 前端增加全局开关展示、保存与提示文案。

第一版不重构现有兼容恢复模式，但需要在 `Codex Responses WebSocket` 相关的三层链路同时加约束：

- websocket handler
- 通用流式重试 / 选路层
- Codex websocket executor

## 4. 后端实施计划

### 4.1 配置结构扩展

- 在 [config.go](/C:/Users/11252/Desktop/cliproxy/internal/config/config.go) 中新增 `Codex` 顶层配置段，例如：
  - `type CodexConfig struct { TransparentWebsocketMode bool }`
  - `Config.Codex CodexConfig`
- 在 [config.example.yaml](/C:/Users/11252/Desktop/cliproxy/config.example.yaml) 中增加注释和默认值说明。

### 4.2 管理 API 配置读写

- 在管理 handler 中增加该字段的读取和更新能力。
- 方案优先级：
  - 优先复用现有 `GetConfig / PutConfigYAML / persist` 机制。
  - 若前端需要更简洁的布尔开关接口，则新增专用 `GET/PUT/PATCH` 管理端点。
- 路由注册位置参考现有布尔配置端点，例如 [server.go](/C:/Users/11252/Desktop/cliproxy/internal/api/server.go#L556) 的 `/ws-auth`。

### 4.3 Codex websocket 行为分支

- 主要落点：
  - [openai_responses_websocket.go](/C:/Users/11252/Desktop/cliproxy/sdk/api/handlers/openai/openai_responses_websocket.go)
  - [handlers.go](/C:/Users/11252/Desktop/cliproxy/sdk/api/handlers/handlers.go)
  - [codex_websockets_executor.go](/C:/Users/11252/Desktop/cliproxy/internal/runtime/executor/codex_websockets_executor.go)
- 目标：
  - 在进入当前 normalize / repair / transcript replacement 链路前判断当前请求是否应从头进入透明优先模式
  - 若启用，则进入透明优先分支
  - 若未启用，则保持现有逻辑

### 4.4 透明优先分支设计

透明优先分支应具备以下行为：

- 原始 websocket 请求语义尽量原样送上游
- 不执行 `normalizeResponsesWebsocketRequestWithMode` 中会改写请求语义的兼容分支
- 不执行 `repairResponsesWebsocketToolCalls`
- 不维护代理侧 transcript replacement 语义
- 不执行 synthetic prewarm
- 不主动删除或重写 `previous_response_id`
- 不执行本地 `response.append` 兼容拼接

同时保留以下行为：

- `pinnedAuthID` 选择与固定
- `executionSessionID` 贯通
- executor 层同 auth、同 session 的 websocket 重拨能力
- 首字节前有限重试
- 请求与错误日志

这里的“首字节前有限重试”必须显式收敛为：

- 首次请求可以沿用当前 `routing.strategy` 完成首个 auth 选择，因此需要兼容 `round-robin` 与 `fill-first`
- 一旦 `pinnedAuthID` 形成，后续 turn 和首字节前重试只能复用该 `pinnedAuthID`
- 只能在同一个 `pinnedAuthID` 上发生
- 不允许重新选 auth
- 不允许 provider fallback / model fallback
- 不允许借重试机会切到兼容恢复路径

同时明确：

- 若初次选路最终未得到支持 websocket 的 `Codex` auth，则该请求从头按旧兼容模式处理
- 不允许进入透明分支后再中途回退为兼容模式
- 透明模式 metadata 只能在 websocket handler 已确认“本次 turn 实际进入透明优先分支”后下发；不得在通用执行层仅凭 provider/model/downstream websocket 条件再次推断

### 4.5 executor 与通用重试层约束

除了 handler 分支外，本次还必须覆盖真正会改变透明语义的执行层改写：

- executor 当前会强制 `stream=true`
- executor 当前会删除 `previous_response_id`
- executor 当前会补缺省 `instructions`
- executor 当前会统一把 websocket 请求封装成 `response.create`
- 通用流式执行层当前存在首字节前 bootstrap retry

本次计划要求：

- 保留“上游 websocket 协议必需的最小封装”和必要头注入
- 禁止会改变会话语义的字段改写
- 透明优先模式下，bootstrap retry 不得换 auth，不得继续 provider/model fallback
- 若当前请求未真正进入透明分支，则 conductor / executor 不得收到 transparent metadata，也不得提前收窄重试或跳过 payload/thinking 改写

如现有 auth manager / conductor 仍会在透明优先模式下重选 auth，则将其纳入本次改动范围。

### 4.6 兼容边界控制

需要在代码中明确控制下列边界：

- 仅在 `Codex` provider 且走 websocket upstream 时启用
- 仅在最终选中的 auth 本身支持 websocket 时生效
- 透明模式判定必须以单次请求的最终 handler 分支为单一真相源，避免出现“上层兼容、下层透明”的半透明状态
- 若当前恢复动作需要改写 body 或跨 auth 切换，则直接失败，不走兼容恢复
- `response.append` 在透明优先模式下仅允许最小 transport 归一化，不允许本地 merge
- `generate=false` 在透明优先模式下不再触发 synthetic prewarm，而是直通上游；若上游不接受，则直接失败
- `execution session` 与同 auth 重拨只覆盖单个下游 websocket 连接生命周期，不扩展为跨 reconnect 的代理侧恢复

### 4.7 后端测试

至少补以下测试：

1. 配置解析测试：
   - 默认值为关闭
   - YAML 能正确读写新字段

2. 管理接口测试：
   - 能读取当前值
   - 能保存当前值

3. websocket 行为测试：
   - 开关关闭时沿用旧逻辑
   - 开关开启时跳过 request repair / tool-call repair / transcript replacement
   - 开关开启时跳过 synthetic prewarm
   - 开关开启时仍保留同 auth 重拨或首字节前重试
   - 开关开启时，bootstrap retry 不会换 auth / 不会 fallback
   - 开关开启但最终落到非 websocket auth 时，行为应回退到现有兼容模式，而不是半透明半兼容
   - 开关开启但本次请求未进入透明分支时，conductor / executor 不得收到 transparent metadata

## 5. 前端实施计划

### 5.1 前端落点

前端开关放在 `Codex` 相关设置区域，不放到系统总设置页。

建议实际落点：

- `AiProviders` 的 `CodexSection` 区域新增一张全局设置卡片或全局开关行
- 不放到单个 `Codex API key` 编辑页中，避免把全局开关误读成单 credential 配置

参考文件：

- `C:\Users\11252\Desktop\Cli-Proxy-API-Management-Center\src\components\providers\CodexSection\CodexSection.tsx`
- `C:\Users\11252\Desktop\Cli-Proxy-API-Management-Center\src\pages\AiProvidersPage.tsx`

### 5.2 前端数据接线

- 读取后端全局配置值
- 提供开关切换
- 保存后刷新或同步本地 config store
- 若使用专用管理接口，则在 `services/api` 和 `stores` 中补充对应调用
- 若复用整份 config 读写，则补充 config store 对新字段的映射
- 同时补齐 `Config` 类型、原始 config 解析类型和 transformer 映射

### 5.3 前端文案

需要新增中英文文案，至少包括：

- 开关标题
- 开关说明
- 风险提示
- 成功 / 失败提示

文案原则：

- 明确这是“Codex websocket 透明优先模式”
- 明确“只影响最终选中且支持 websocket 的 `Codex` 凭证场景”
- 明确“开启后更接近官方语义，但会减少代理兼容修补”

### 5.4 前端验证

至少完成：

- `npm run type-check`
- `npm run build`

## 6. 实施顺序

建议顺序：

1. 后端配置结构与管理接口
2. 后端 websocket 透明优先分支
3. 后端测试
4. 前端开关与文案接线
5. 前端构建验证
6. 联调验证

## 7. 联调验收清单

联调时至少验证以下场景：

1. 开关关闭，现有 `Codex Responses WebSocket` 行为不变。
2. 开关开启，且 auth 支持 websocket 时：
   - 请求原始 `previous_response_id` 不被代理删改
   - request repair / tool-call repair 不再触发
   - 同 auth 重拨仍可发生
   - `response.append` 不会触发本地 merge
   - `generate=false` 不再触发本地 synthetic prewarm

3. 开关开启，但请求未进入 websocket upstream 时：
   - 行为不变

4. 开关开启，且当前 `routing.strategy` 为 `round-robin` 或 `fill-first` 时：
   - 首次 auth 选择仍遵循现有路由策略
   - 一旦透明会话形成 `pinnedAuthID`，后续 turn 不再重新轮询或填充切换 auth

5. 前端切换开关后：
   - 后端配置持久化成功
   - 页面刷新后仍显示正确值

## 8. 风险点

- 当前 websocket handler 强依赖 normalize 流程，透明分支需要避免把已有兼容逻辑误带进去。
- 若透明分支与现有 `lastRequest / lastResponseOutput` 状态耦合过深，可能需要拆出更清晰的分支路径。
- 前端若只在 `CodexSection` 展示全局开关，需要注意不要与单个 credential 的 `websockets` 开关混淆。

## 9. 预计改动文件

### 9.1 后端

- [config.go](/C:/Users/11252/Desktop/cliproxy/internal/config/config.go)
- [config.example.yaml](/C:/Users/11252/Desktop/cliproxy/config.example.yaml)
- [server.go](/C:/Users/11252/Desktop/cliproxy/internal/api/server.go)
- [config_basic.go](/C:/Users/11252/Desktop/cliproxy/internal/api/handlers/management/config_basic.go)
- [openai_responses_websocket.go](/C:/Users/11252/Desktop/cliproxy/sdk/api/handlers/openai/openai_responses_websocket.go)
- [handlers.go](/C:/Users/11252/Desktop/cliproxy/sdk/api/handlers/handlers.go)
- [codex_websockets_executor.go](/C:/Users/11252/Desktop/cliproxy/internal/runtime/executor/codex_websockets_executor.go)
- 如有需要，相关 auth 选择 / conductor 文件
- 相关测试文件

### 9.2 前端

- `C:\Users\11252\Desktop\Cli-Proxy-API-Management-Center\src\components\providers\CodexSection\CodexSection.tsx`
- `C:\Users\11252\Desktop\Cli-Proxy-API-Management-Center\src\pages\AiProvidersPage.tsx`
- `C:\Users\11252\Desktop\Cli-Proxy-API-Management-Center\src\types\config.ts`
- `C:\Users\11252\Desktop\Cli-Proxy-API-Management-Center\src\services\api\transformers.ts`
- `C:\Users\11252\Desktop\Cli-Proxy-API-Management-Center\src\stores\useConfigStore.ts`
- `C:\Users\11252\Desktop\Cli-Proxy-API-Management-Center\src\services\api\*`
- `C:\Users\11252\Desktop\Cli-Proxy-API-Management-Center\src\i18n\*`

## 10. 第 3 阶段实施前提醒

根据完整流程要求，进入实施阶段前需先确认是否拆分子代理。  
本需求天然可拆成“后端改动”和“前端改动”两块并行实施。
