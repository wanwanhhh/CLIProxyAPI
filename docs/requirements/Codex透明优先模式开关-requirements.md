# Codex 透明优先模式开关需求文档

## 1. 背景

当前 `CLIProxyAPI` 在 `Codex Responses WebSocket` 场景下会主动参与请求语义：

- 规范化 websocket 请求
- repair tool call
- transcript replacement
- 基于本地状态做续链和恢复
- 结合 auth 选择、重试和路由策略决定实际上游行为

这套兼容代理模式有兼容性和兜底价值，但在 `Codex + websocket` 场景下，也会引入一类额外问题：

- 代理可能改写原始请求语义
- `previous_response_id`、`instructions`、`store` 等字段不再完全等价于上游官方语义
- websocket 断链后的恢复逻辑与官方 `Codex app` 行为不一致

用户希望新增一个只针对 `Codex` 的“透明优先模式”开关，使 `Codex Responses WebSocket` 请求尽量以接近客户端原始语义的形态直达上游，同时仍保留不改变请求语义的最小增强能力。

## 2. 目标

新增一个全局配置开关，用于启用 `Codex transparent-first mode`，满足以下目标：

- 只针对 `Codex`
- 第一版只针对 `Codex Responses WebSocket`
- 只影响最终选中且支持 websocket 的 `Codex` 凭证场景
- 开启后，客户端原始 websocket 请求语义和关键会话字段尽量原样送上游
- 尽量保留不改请求语义的增强能力
- 默认关闭，关闭时保持当前行为不变
- 在管理前端提供对应的全局开关入口

## 3. 适用范围

### 3.1 纳入范围

- 后端仓库 [cliproxy](/C:/Users/11252/Desktop/cliproxy)
- 前端仓库 `C:\Users\11252\Desktop\Cli-Proxy-API-Management-Center`
- `Codex Responses WebSocket` 透明优先模式全局配置
- 管理 API 配置读写能力
- 管理前端上的对应开关展示与保存
- 后端最小必要测试

### 3.2 不纳入范围

- `Claude / Gemini / OpenAI compatibility` 其他 provider 的透明模式
- 普通 HTTP `/responses`
- `/v1/chat/completions`
- `Codex API key` 非 websocket 直连链路
- 当前兼容恢复模式的整体重构
- 透明模式下新增 transcript cache、compact 自动消费、跨 auth 恢复

## 4. 用户故事

### 4.1 管理员视角

作为代理管理员，我希望在管理端开启或关闭 `Codex` 透明优先模式，以便根据客户端类型在“官方语义一致性”和“代理兼容修补能力”之间切换。

### 4.2 Codex websocket 用户视角

作为使用 `Codex + websocket` 的用户，我希望在开启透明优先模式后，代理尽量不要改写我的请求语义，使行为更接近 `Codex app` 直接请求上游。

## 5. 关键约束

- 开关必须是全局开关，而不是按 auth、按模型、按单会话配置。
- 开关只影响“最终选中且支持 websocket 的 `Codex` 凭证场景”。
- 若请求未进入 `Codex Responses WebSocket` 链路，则该开关不生效。
- 关闭开关时，现有行为必须保持不变。
- 开启开关后，代理应优先保持“请求透明”，不再主动执行会改写请求语义的兼容修补动作。
- 前端开关应放在 `Codex` 相关设置区域，而不是总系统设置页。
- 允许上游 websocket 协议必需的最小封装与头注入，但禁止改变会话语义的兼容改写。

## 6. 功能需求

### 6.1 后端配置

后端需新增一个全局配置项，建议放在新的 `codex` 顶层配置段下，例如：

```yaml
codex:
  transparent-websocket-mode: false
```

该配置项语义为：

- `false`：沿用当前兼容代理模式
- `true`：对 `Codex Responses WebSocket` 启用透明优先模式

### 6.2 生效条件

透明优先模式仅在同时满足以下条件时生效：

- 当前 provider 为 `Codex`
- 请求入口为 `Responses WebSocket`
- 当前请求实际上游走的是 websocket executor
- 当前最终选中的上游 `Codex` 凭证支持 websocket

若任一条件不满足，则保持原有逻辑。

透明优先模式的判定时机要求如下：

- 初次请求允许沿用当前全局 `routing.strategy` 进行首个 auth 选择，因此需兼容现有 `round-robin` 与 `fill-first` 两种策略。
- 一旦某次透明优先 websocket 会话已经选定实际执行该次 upstream websocket 请求的 auth，并形成 `pinnedAuthID`，后续 turn 与首字节前重试只能复用该 `pinnedAuthID`。
- 若初次选路最终未能选中支持 websocket 的 `Codex` auth，则该请求必须从一开始按旧兼容模式处理，而不是先进入透明分支后再回退。
- 若本次请求未真正进入透明优先分支，则透明模式标记不得继续向 conductor、executor 等后续执行层传播；整个链路都应保持旧兼容模式。

### 6.3 透明优先模式下必须保留的能力

以下能力允许保留：

- auth 选择
- 必要的鉴权头注入
- 同 auth、同 execution session 的上游 websocket 重拨
- 首字节前的有限自动重试
- 请求日志、调试日志、链路观测
- 不改变请求 body 语义的 keepalive / 连接维护

其中“首字节前的有限自动重试”必须收敛为：

- 初次请求在 `pinnedAuthID` 尚未形成前，只允许完成首次选路
- 只允许在同一个 `pinned auth` 上发生
- 只允许同链或同 execution session 的新 socket 重拨
- 不允许重新选 auth
- 不允许 provider fallback
- 不允许 model fallback

### 6.4 透明优先模式下必须关闭的兼容修补

以下能力在透明优先模式下必须关闭：

- request normalize / request repair
- transcript replacement
- tool-call repair
- synthetic prewarm / 本地下发预热响应
- 代理侧拼接完整 transcript
- 删除或改写客户端提供的 `previous_response_id`
- 自动切换到跨 auth 的恢复链路
- provider fallback / model fallback
- 向下游发出首字节后的透明重放

### 6.5 特殊请求边界

透明优先模式下，以下特殊请求边界必须明确：

- 对 `response.append`，代理只允许做 websocket 传输层所必需的最小封装，不允许执行本地 transcript merge 或 append 兼容拼接。
- 若某类 `response.append` 请求无法在“不改写会话语义”的前提下转发上游，则直接失败，不回退到本地兼容拼接。
- 对 `generate=false`，代理不再执行 synthetic prewarm；请求应原样直通上游。
- 若 `generate=false` 在上游链路下不被接受，则直接返回上游错误，不再本地伪造响应。

### 6.6 行为要求

开启透明优先模式后：

- 下游 websocket 请求语义应尽量原样发往上游
- `previous_response_id`、`instructions`、`store` 等关键字段不得被代理主动重写
- 代理仍可在连接层执行“同 auth 重拨”和“同 auth、首字节前有限重试”
- 若恢复需要改写 body 或切换到跨 auth 新链，应直接失败，不做兼容恢复
- 代理可以保留 websocket 协议必需的最小封装，例如必要的 `response.create` 传输层包装和必要 header 注入，但不得借此改写会话语义
- 透明模式是否生效必须以单次请求在 websocket handler 中的最终分支决策为准，后续通用执行层不得再按 provider/model 重新推断并擅自开启透明模式约束

### 6.7 前端要求

前端仓库 `Cli-Proxy-API-Management-Center` 需提供一个全局开关：

- 开关位置：`Codex` 相关设置区域
- 展示文案需清楚说明这是“只影响最终选中且支持 websocket 的 `Codex` 凭证场景的透明优先模式”
- 开关保存后应同步更新后端配置
- 前端需要展示明确提示：开启后会减少代理兼容修补行为，更接近官方直连语义

## 7. 非功能需求

- 关闭开关时无行为回归
- 开关切换应可通过管理端持久化到配置文件
- 开关值应能通过管理 API 正确读取
- 前后端命名一致，避免出现多个语义相近但不同名的开关
- 文案需避免把该模式表述成“完全透明”或“官方完全等价”

## 8. 验收标准

满足以下条件视为验收通过：

1. 后端存在可持久化的 `Codex` 透明优先模式全局开关，默认关闭。
2. 当前开关关闭时，`Codex Responses WebSocket` 行为与改动前保持一致。
3. 当前开关开启时，仅 `Codex Responses WebSocket` 且实际走 websocket upstream 的场景发生行为切换。
4. 开启后，代理不再执行 request repair、transcript replacement、tool-call repair、跨 auth 恢复等会改写请求语义的逻辑。
5. 开启后，仍保留同 auth 重拨、首字节前有限重试、必要鉴权头注入和日志能力。
6. 管理前端可以查看、切换并保存该开关。
7. 后端至少具备与该开关相关的配置读写和关键行为测试。

## 9. 风险与边界提示

- 透明优先模式不会消除 websocket 物理断连。
- 透明优先模式不会自动补齐客户端缺失的上下文恢复能力。
- 透明优先模式提升的是“请求语义一致性”，不是“代理容错最大化”。
- 对依赖代理兼容修补的非原生客户端，开启后可能暴露更多客户端自身问题。
- 当前验收只检查 `Codex Responses WebSocket` 实际发往上游的 payload 与行为，不把非 websocket 链路混入验收。

## 10. 本轮实施前提

- 后端工作区为 [cliproxy](/C:/Users/11252/Desktop/cliproxy)
- 前端工作区为 `C:\Users\11252\Desktop\Cli-Proxy-API-Management-Center`
- 本轮流程包含两个仓库的实际改动
