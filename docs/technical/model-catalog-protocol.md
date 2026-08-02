# 模型目录 Section 状态协议

## 状态

- 状态：Accepted
- 版本：1
- 日期：2026-08-02
- 适用仓库：`joslynSmall/models`、`CLIProxyAPI`
- 适用载体：`models.json`

本文档定义模型目录中 provider section 的状态语义。目录生产者和消费者必须共同遵守本协议，避免一个 provider 的缺失、下线或坏数据阻断其他 provider 的模型更新。

本文中的“必须”“不得”“应该”是规范性要求。

## 背景

CLIProxyAPI 同时使用两类模型目录：

1. 构建时嵌入二进制的完整快照 `internal/registry/models/models.json`。
2. 启动时及每 3 小时从远端获取的运行时目录。

旧协议要求所有已支持 section 都存在且非空。该规则无法区分“provider 没有参与本次更新”和“provider 已被明确清空”，还会让一个无关的空或缺失 section 导致整份远端目录回退。

## 术语

- **已支持 section**：CLIProxyAPI 当前能够解析并注册的顶层模型列表，例如 `codex-plus`、`qwen`。
- **未知 section**：目录中存在、但当前 CLIProxyAPI 尚未实现的顶层字段，例如当前的 `xai`。
- **当前值**：模型目录 store 在应用本次远端更新前持有的 section 快照。
- **完整快照**：构建时嵌入二进制、能够独立初始化模型 store 的目录。
- **远端 patch**：运行时下载并与当前值逐 section 合并的目录。

## Section 三态语义

对于每个已支持 section，消费者必须根据 JSON 顶层字段是否存在及其数组内容应用以下规则：

| 输入状态 | 语义 | 消费者行为 |
|---|---|---|
| section 缺失 | 本次不更新 | 保留当前值，不产生该 provider 的变更通知 |
| section 显式为 `[]` | 有意清空 | 将当前值替换为空列表，并产生该 provider 的变更通知（值确有变化时） |
| section 为非空数组 | 替换 | 校验通过后完整替换当前值，并产生该 provider 的变更通知（值确有变化时） |

### 缺失：保留当前值

```json
{
  "codex-plus": [
    {"id": "gpt-5.5"}
  ]
}
```

如果上述远端 patch 没有 `qwen`，消费者必须保留当前 Qwen 模型。消费者不得把字段缺失解释为清空。

### 显式空数组：有意清空

```json
{
  "qwen": []
}
```

消费者必须清空 Qwen section。刷新回调随后应重新绑定 Qwen Auth；空模型列表应移除其旧模型注册。

### 非空数组：完整替换

```json
{
  "codex-plus": [
    {"id": "gpt-5.5"},
    {"id": "gpt-5.6-sol"}
  ]
}
```

非空数组不是增量追加。校验通过后，它必须完整替换当前 `codex-plus` section。

## 未知 Section

未知 section 必须向前兼容：

- 不得导致顶层目录解析或已支持 section 更新失败。
- 不得自动注册为 provider。
- 可以记录 debug 级诊断信息，但不得记录模型目录之外的敏感运行时数据。
- 当 CLIProxyAPI 后续实现该 provider 时，必须将其加入已支持 section 集合并补充测试。

例如，当前消费者尚未支持 `xai` 时，应忽略其内容，同时继续应用有效的 Codex 更新。

## 校验规则

### 顶层规则

- 顶层必须是 JSON object。
- 顶层 JSON 无法解析时，必须拒绝该远端 URL 的整个响应。
- 远端 patch 至少必须包含一个可识别且可应用的已支持 section；只有未知字段的响应不得替换当前目录。

### 已支持 Section 规则

- section 存在时必须是 JSON array。
- 空数组是合法的“有意清空”状态。
- 非空数组中的每一项必须是非 null object。
- 每一项的 `id` 必须是去除首尾空白后的非空字符串。
- 同一 section 内的模型 ID 不得重复。

### Section 级失败隔离

远端 patch 中某个已支持 section 校验失败时：

1. 该 section 必须保留当前值。
2. 其他校验通过的 section 必须继续应用。
3. 必须记录包含来源 URL、section 名称和失败原因的 warning。
4. 不得因该 section 的错误把整个远端目录描述为“下载失败”。

如果所有已识别 section 都不可应用，消费者必须拒绝该 URL，并尝试下一个目录地址；所有地址都失败时才保留完整当前目录。

## 完整快照与远端 Patch

### 完整快照

构建时嵌入的 `models.json` 必须显式包含 CLIProxyAPI 的所有已支持 section。已下线 provider 必须写成显式空数组，不得省略。

完整快照允许空数组，因为空数组是有意状态，而不是结构损坏。完整快照仍必须满足模型项结构和 ID 唯一性规则。

### 远端 Patch

运行时远端目录按 patch 语义处理：

- 缺失 section 保留当前值。
- 显式空数组清空。
- 有效非空数组替换。
- 无效 section 单独回退。

合并必须生成新的不可变目录快照；不得原地修改正在被并发读取的当前快照。

## 变更通知

消费者只对合并前后实际发生变化的 provider 发出通知：

- `codex-free`、`codex-team`、`codex-plus`、`codex-pro` 统一映射为 provider `codex`。
- 显式清空与非空替换使用相同的变更检测规则。
- 缺失且被保留的 section 不构成变化。
- 同一 provider 在一次刷新中最多通知一次。

Service 收到通知后负责重新注册该 provider 的所有启用 Auth。目录更新不得要求用户重新执行 OAuth。

## 当前目录约定

基于 2026-08-02 的上游目录状态：

- Qwen 已明确下线，生产者应发布 `"qwen": []`。
- iFlow 已明确下线，生产者应发布 `"iflow": []`。
- `xai` 是当前 CLIProxyAPI 未支持的未知 section，消费者应忽略。
- Codex Plus/Pro 的有效非空数组应独立于 Qwen、iFlow 和 xAI 的状态正常更新。

## 兼容性与失败行为

- 旧消费者可能仍拒绝显式空数组；因此目录变更必须与支持本协议的 CLIProxyAPI 版本协调发布。
- 新消费者读取旧目录时，对缺失的 Qwen/iFlow 保留当前嵌入值，不会误清空。
- 回滚到旧二进制后，旧消费者可以拒绝新目录并使用自身嵌入快照，恢复旧行为。
- 本协议不定义模型是否真实可被某个账号调用；账号套餐、排除列表和运行时可用性仍由现有 Auth 与 registry 逻辑处理。

## 非目标

本协议不负责：

- 自动实现未知 provider。
- 动态探测 OAuth 账号在上游实际可调用的模型。
- 修改 OAuth 套餐识别规则。
- 定义模型项的全部能力字段；本协议仅定义 section 状态、基础结构和更新行为。
