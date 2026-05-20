# rtk-ai/rtk 与主流 AI 编码工具兼容性调研报告

**调研日期**: 2026-05-20
**调研对象**: [rtk-ai/rtk](https://github.com/rtk-ai/rtk) (v0.40.0)
**调研范围**: Claude Code、OpenCode、Codex CLI 及主流 AI 编码工具的兼容性

---

## 执行摘要

rtk（Rust Token Killer）是一个高性能 CLI 代理工具，可将常见开发命令的 LLM token 消耗降低 60-90%。截至 v0.40.0，rtk 官方明确支持 **13 款 AI 编码工具**，涵盖 Hook 透明重写、插件级命令替换、提示级引导三种主要集成模式。

本报告聚焦于用户重点询问的 **Claude Code**、**OpenCode** 和 **Codex CLI**。调研发现：三者均获得官方一等支持，但集成深度和成熟度存在差异——Claude Code 采用最成熟的 PreToolUse Hook 机制；OpenCode 采用 TypeScript 插件但曾存在 Hook 不触发的问题（已修复）；Codex CLI 仅支持提示级引导（AGENTS.md），无程序化 Hook，且存在文件写入路径错误的问题。

---

## 1. Claude Code 兼容性

### 1.1 官方支持情况

**官方明确支持。** rtk 将 Claude Code 列为首要支持对象，在 README 和官方文档中均提供了专门的安装和配置说明。

### 1.2 支持方式：PreToolUse Hook（透明重写）

Claude Code 的集成采用 **Shell 脚本 Hook** 机制，具体技术细节如下：

| 属性 | 详情 |
|------|------|
| **Hook 类型** | `PreToolUse` Shell Hook |
| **Hook 文件** | `rtk-rewrite.sh`（由 `rtk init` 安装） |
| **配置位置** | `~/.claude/settings.json` |
| **JSON 输入格式** | `{"tool_name": "Bash", "tool_input": {"command": "git status"}}` |
| **JSON 输出格式** | `{"hookSpecificOutput": {"hookEventName": "PreToolUse", "permissionDecision": "allow", "updatedInput": {"command": "rtk git status"}}}` |
| **依赖** | 需要 `jq` 进行 JSON 解析 |
| **版本要求** | rtk >= 0.23.0 |

**工作原理**：当 Claude Code 即将执行 Bash 命令时，Hook 脚本拦截该命令，调用 `rtk rewrite <command>` 获取重写后的命令（如 `git status` → `rtk git status`），然后通过 `updatedInput` 字段透明替换原始命令。Claude Code 本身并不知道 RTK 的存在。

### 1.3 配置步骤

```bash
# 1. 安装 rtk（如果尚未安装）
brew install rtk
# 或
curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh | sh

# 2. 为 Claude Code 初始化 rtk（推荐全局安装）
rtk init -g

# 3. 重启 Claude Code
# Hook 会在 Bash 工具调用时自动生效

# 4. 验证安装
rtk init --show
```

**`rtk init -g` 具体会安装**：
- Hook 脚本（`rtk-rewrite.sh`）
- `~/.claude/CLAUDE.md`（RTK 使用说明）
- `~/.claude/settings.json`（Hook 配置）
- `@RTK.md` 引用

### 1.4 重要限制

- **仅作用于 Bash 工具调用**：Claude Code 内置工具（`Read`、`Grep`、`Glob`）**不经过 Bash Hook**，因此不会被自动重写。如需压缩这些工具的输出，应改用 shell 命令（`cat`/`rg`/`find`）或显式调用 `rtk read` / `rtk grep` / `rtk find`。
- **子代理不继承 Hook**：Claude Code 通过 Task/Agent 工具生成的子代理会话无法继承主代理的 RTK Hook，导致多代理工作流中产生大量 token 泄漏（详见 [#1820](https://github.com/rtk-ai/rtk/issues/1820)）。

---

## 2. OpenCode 兼容性

### 2.1 官方支持情况

**官方明确支持。** rtk 从早期版本即支持 OpenCode，并在 README 的 Supported AI Tools 表格中明确列出。

### 2.2 支持方式：TypeScript 插件（`tool.execute.before`）

OpenCode 的集成采用 **TypeScript 插件** 机制：

| 属性 | 详情 |
|------|------|
| **插件类型** | TypeScript 插件（非 Shell Hook） |
| **安装位置** | `~/.config/opencode/plugins/rtk.ts` |
| **Hook 事件** | `tool.execute.before` |
| **命令重写** | 原地修改 `args.command` |
| **依赖库** | `zx`（Google 的 shell 脚本库） |
| **失败处理** | `.quiet().nothrow()`，静默忽略失败 |

**工作原理**：插件拦截 `tool.execute.before` 事件，调用 `rtk rewrite ${command}` 获取重写后的命令，如果重写结果与原命令不同，则原地修改 `args.command`。

### 2.3 配置步骤

```bash
# 1. 安装 rtk（如果尚未安装）
brew install rtk

# 2. 为 OpenCode 初始化 rtk
rtk init -g --opencode

# 3. 插件会自动安装到 ~/.config/opencode/plugins/rtk.ts
# 4. 重启 OpenCode，插件自动加载
```

### 2.4 已知问题与用户反馈

| Issue | 状态 | 描述 |
|-------|------|------|
| [#1706](https://github.com/rtk-ai/rtk/issues/1706) | **已关闭** | **OpenCode 插件 `tool.execute.before` Hook 不触发**。用户报告插件加载成功但 `Bash` 命令从未被自动重写。最终该 issue 被关闭，表明问题已修复。 |
| [#1925](https://github.com/rtk-ai/rtk/issues/1925) | 开放 | 功能请求：为 OpenCode 添加 TUI 统计面板，实时显示 token 节省数据。 |
| [#1822](https://github.com/rtk-ai/rtk/pull/1822) | **已废弃** | 添加 OpenCode 配置文档的 PR 被关闭（废弃）。说明文档可能不够完善。 |
| [#732](https://github.com/rtk-ai/rtk/issues/732) | 开放 | `rtk git branch --show-current` 在 OpenCode 中发生循环。 |

### 2.5 兼容性评估

OpenCode 的支持属于**插件级（中维护成本）**。虽然官方支持明确，但实际运行中曾出现 Hook 不触发的问题（#1706）。目前该问题已修复，但相比 Claude Code 的成熟 Hook 系统，OpenCode 的集成稳定性略逊一筹。用户也可以通过手动运行 `rtk <command>` 来绕过自动重写问题。

---

## 3. Codex CLI 兼容性

### 3.1 官方支持情况

**官方明确支持。** rtk 在 README 中将 Codex（OpenAI）列为支持的工具之一。

### 3.2 支持方式：AGENTS.md + RTK.md（提示级引导）

Codex CLI 的集成采用 **提示级引导** 机制，与其他工具的 Hook/插件方式有本质区别：

| 属性 | 详情 |
|------|------|
| **集成层级** | **Rules file（提示级）** — 无程序化 Hook |
| **安装命令** | `rtk init -g --codex` |
| **配置文件** | `$CODEX_HOME/AGENTS.md` 或 `~/.codex/AGENTS.md` |
| **引导内容** | `rtk-awareness.md` 被注入到 AGENTS.md 中，附带 `@RTK.md` 引用 |
| **重写方式** | Codex 阅读 AGENTS.md 后**自主决定**是否使用 `rtk` 前缀 |
| **预期采用率** | 约 70-85%（非 100%） |

**工作原理**：rtk 将使用说明写入 Codex 的配置文件（AGENTS.md），Codex 在执行命令前阅读这些指令，**自主判断**是否添加 `rtk` 前缀。Codex 不会自动重写命令，而是通过提示引导其使用 rtk。

### 3.3 配置步骤

```bash
# 1. 安装 rtk
brew install rtk

# 2. 为 Codex 初始化 rtk（全局安装）
rtk init -g --codex

# 3. 配置会写入：
#    - $CODEX_HOME/AGENTS.md（如果设置了 $CODEX_HOME）
#    - 或 ~/.codex/AGENTS.md
#    - 以及对应的 RTK.md 文件

# 4. 重启 Codex CLI
```

### 3.4 已知问题与用户反馈

| Issue | 状态 | 描述 |
|-------|------|------|
| [#1943](https://github.com/rtk-ai/rtk/issues/1943) | **开放** | **Codex `rtk init` 写入错误的文件路径**。rtk 将指令写入了 `.codex/AGENTS.md`，但 Codex CLI 实际只读取 `.codex/AGENTS.override.md`（本地覆盖文件），导致 RTK 指令被忽略。这是一个影响功能的 bug。 |
| [#1864](https://github.com/rtk-ai/rtk/issues/1864) | 开放 | 功能请求：在 Windows 原生环境支持 Codex 的自动重写 Hook。目前 Codex 在 Windows 上也仅使用 AGENTS.md 回退模式。 |
| [#1893](https://github.com/rtk-ai/rtk/pull/1893) | 开放（PR） | 功能 PR：将 Codex 会话统计添加到 `rtk gain` 和 `rtk session` 中，说明 Codex 的统计数据追踪尚不完善。 |

### 3.5 兼容性评估

Codex CLI 的支持属于**Rules file 级（低维护成本）**，但功能也最弱。由于没有程序化 Hook，Codex 对 RTK 的采用率取决于模型对指令的遵循程度（约 70-85%），且存在 [#1943](https://github.com/rtk-ai/rtk/issues/1943) 文件写入错误的 bug，导致当前版本的自动配置实际上**不生效**。用户需要手动将 RTK 指令添加到 `.codex/AGENTS.override.md` 才能正常工作。

---

## 4. 所有支持工具的集成方式对比

| 工具 | 安装命令 | 集成机制 | 集成层级 | 能否修改命令 | 预期采用率 |
|------|----------|----------|----------|--------------|------------|
| **Claude Code** | `rtk init -g` | PreToolUse Shell Hook | Full Hook | 是（透明重写） | ~100% |
| **GitHub Copilot (VS Code)** | `rtk init -g --copilot` | Rust Binary Hook | Full Hook | 是（`updatedInput`） | ~100% |
| **GitHub Copilot CLI** | `rtk init -g --copilot` | Rust Binary Hook | Deny-with-suggestion | 否（建议重试） | ~70-85% |
| **Cursor** | `rtk init -g --agent cursor` | preToolUse Shell Hook | Full Hook | 是（`updated_input`） | ~100% |
| **Gemini CLI** | `rtk init -g --gemini` | Rust Binary Hook（`BeforeTool`） | Full Hook | 是 | ~100% |
| **Codex** | `rtk init -g --codex` | AGENTS.md 指令 | Rules file | N/A（提示引导） | ~70-85% |
| **Windsurf** | `rtk init --agent windsurf` | `.windsurfrules` | Rules file（项目级） | N/A | ~70-85% |
| **Cline / Roo Code** | `rtk init --agent cline` | `.clinerules` | Rules file（项目级） | N/A | ~70-85% |
| **OpenCode** | `rtk init -g --opencode` | TypeScript 插件 | Plugin | 是（原地替换） | ~100% |
| **OpenClaw** | `openclaw plugins install ./openclaw` | TypeScript 插件 | Plugin | 是 | ~100% |
| **Hermes** | `rtk init --agent hermes` | Python 插件 | Plugin | 是 | ~100% |
| **Kilo Code** | `rtk init --agent kilocode` | `.kilocode/rules` | Rules file（项目级） | N/A | ~70-85% |
| **Google Antigravity** | `rtk init --agent antigravity` | `.agents/rules` | Rules file（项目级） | N/A | ~70-85% |
| **Mistral Vibe** | — | — | Planned | — | — |

---

## 5. Issues 中关于兼容性的讨论汇总

### 5.1 Claude Code 相关问题

| Issue | 严重程度 | 状态 | 问题描述 |
|-------|----------|------|----------|
| [#1820](https://github.com/rtk-ai/rtk/issues/1820) | 高 | 开放 | **子代理不继承 RTK Hook**。Claude Code 的 Task/Agent 工具生成的子代理使用原始命令，多代理工作流中一次会话可浪费 10 万+ token。 |
| [#1556](https://github.com/rtk-ai/rtk/issues/1556) | **P0（严重）** | 开放 | **RTK 在 Claude Code 调用时崩溃**。在 Fedora 43 上，`rtk proxy` 和 `rtk git status` 等命令触发 SIGABRT（信号 6）核心转储。 |
| [#1319](https://github.com/rtk-ai/rtk/issues/1319) | 中 | 开放 | **PowerShell Hook 兼容性**。PowerShell 环境下 Claude Code 的 Hook 无法正常工作。 |
| [#1950](https://github.com/rtk-ai/rtk/issues/1950) | 中 | 开放 | Claude Code 工作流中 `node` 和 `npm test` 命令缺少处理器。 |
| [#1811](https://github.com/rtk-ai/rtk/issues/1811) | 高 | 开放 | `npm run tsc` 被重写成 `rtk tsc` 时丢失了 `--noEmit` 参数，导致生成多余的 `.js`/`.map` 文件。 |
| [#1564](https://github.com/rtk-ai/rtk/issues/1564) | **P1（关键）** | 开放 | 命令前导 `\` 换行符导致 Hook 匹配失败，命令不被重写。 |
| [#1892](https://github.com/rtk-ai/rtk/issues/1892) | 中 | 开放 | `php` 命令不再被重写到 `rtk php`。 |

### 5.2 OpenCode 相关问题

| Issue | 严重程度 | 状态 | 问题描述 |
|-------|----------|------|----------|
| [#1706](https://github.com/rtk-ai/rtk/issues/1706) | 高 | **已关闭** | **OpenCode 插件 `tool.execute.before` Hook 从未触发**，自动重写完全失效。 |
| [#1925](https://github.com/rtk-ai/rtk/issues/1925) | 低 | 开放 | 功能请求：添加 OpenCode TUI 统计面板。 |
| [#1822](https://github.com/rtk-ai/rtk/pull/1822) | 低 | **已废弃** | 添加 OpenCode 配置文档的 PR 被废弃。 |

### 5.3 Codex 相关问题

| Issue | 严重程度 | 状态 | 问题描述 |
|-------|----------|------|----------|
| [#1943](https://github.com/rtk-ai/rtk/issues/1943) | 高 | 开放 | **`rtk init` 写入错误的文件**。应写入 `.codex/AGENTS.override.md` 而非 `.codex/AGENTS.md`，导致 Codex 不读取 RTK 指令。 |
| [#1864](https://github.com/rtk-ai/rtk/issues/1864) | 中 | 开放 | Windows 原生环境不支持 Codex 的自动重写 Hook。 |
| [#1893](https://github.com/rtk-ai/rtk/pull/1893) | 低 | 开放（PR） | Codex 会话统计尚未集成到 `rtk gain` 中。 |

### 5.4 通用兼容性问题

| Issue | 严重程度 | 状态 | 问题描述 |
|-------|----------|------|----------|
| [#1774](https://github.com/rtk-ai/rtk/issues/1774) | 中 | 开放 | `rtk init -g --copilot` 声称全局安装，实际只安装了项目级配置，且 `rtk init --show` 不显示 Copilot 状态。 |
| [#1717](https://github.com/rtk-ai/rtk/issues/1717) | 中 | 开放 | **OpenClaw 插件** `before_tool_call` Hook 无法拦截 `exec` 命令。 |
| [#1597](https://github.com/rtk-ai/rtk/issues/1597) | 中 | 开放 | **rtk-hermes 1.0.0** 与当前 Hermes 插件加载机制不兼容，且当 `rtk rewrite` 返回非零退出码时会错过有效重写。 |
| [#1515](https://github.com/rtk-ai/rtk/issues/1515) | 中 | 开放 | RTK 与其他 CLI 包（如 `oh-my-codex`、`oh-my-claude`）一起使用时失效。 |
| [#1962](https://github.com/rtk-ai/rtk/issues/1962) | 中 | 开放 | `rtk-rewrite.sh: No such file or directory` — Hook 文件缺失。 |
| [#1248](https://github.com/rtk-ai/rtk/issues/1248) | 中 | 开放 | **Windows PowerShell 兼容性缺口**。原生 Windows 缺少 Hook 支持，只能使用 CLAUDE.md 回退模式。 |
| [#1436](https://github.com/rtk-ai/rtk/issues/1436) | 中 | 开放 | `rtk grep` 与原生 `grep` 不完全兼容。 |

---

## 6. 配置各工具使用 rtk 代理的详细步骤

### 6.1 Claude Code

```bash
# 步骤 1: 安装 RTK
brew install rtk

# 步骤 2: 初始化 Claude Code 集成（全局推荐）
rtk init -g

# 步骤 3: 验证安装状态
rtk init --show

# 步骤 4: 重启 Claude Code
# Bash 命令现在会自动重写
```

**验证 Hook 是否工作**：
```bash
# 在 Claude Code 中输入：
git status
# 期望实际执行：rtk git status（输出应为压缩后的 1-3 行）
```

### 6.2 OpenCode

```bash
# 步骤 1: 安装 RTK
brew install rtk

# 步骤 2: 初始化 OpenCode 集成
rtk init -g --opencode

# 步骤 3: 确认插件已安装
ls ~/.config/opencode/plugins/rtk.ts

# 步骤 4: 重启 OpenCode
```

**手动回退方案**（如果自动重写不工作）：
```bash
# 显式使用 rtk 前缀
rtk git status
rtk cargo test
rtk ls .
```

### 6.3 Codex CLI

```bash
# 步骤 1: 安装 RTK
brew install rtk

# 步骤 2: 初始化 Codex 集成
rtk init -g --codex

# 步骤 3: 手动检查并修正文件路径（重要！）
# 由于 bug #1943，需要确认写入的是正确的文件：
# 目标文件应为：
#   $CODEX_HOME/.codex/AGENTS.override.md（如果设置了 $CODEX_HOME）
#   或 ~/.codex/AGENTS.override.md

# 如果 rtk 写入了 AGENTS.md 而非 AGENTS.override.md，请手动复制内容。

# 步骤 4: 重启 Codex CLI
```

---

## 7. 综合评估与建议

### 7.1 集成成熟度排序

| 排名 | 工具 | 成熟度 | 原因 |
|------|------|--------|------|
| 1 | Claude Code | **最高** | PreToolUse Hook 成熟稳定，采用率近 100% |
| 2 | Cursor / Gemini / Copilot | **高** | 同样使用 Full Hook，机制类似 Claude Code |
| 3 | OpenCode | **中** | TypeScript 插件曾有不触发问题，现已修复，但文档较薄弱 |
| 4 | Hermes / OpenClaw | **中** | 插件型集成，社区报告存在兼容性问题 |
| 5 | Codex / Windsurf / Cline | **低** | 仅支持 Rules file（提示级），无程序化 Hook，Codex 还存在文件写入 bug |

### 7.2 使用建议

1. **Claude Code 用户**：强烈推荐使用，配置简单，效果最好。但需注意多代理工作流中的子代理 token 泄漏问题（可手动在子代理提示中添加 rtk 指令作为临时 workaround）。

2. **OpenCode 用户**：可以使用，但建议升级到最新版 rtk 以确保插件 Hook 正常工作。如遇自动重写失效，可显式使用 `rtk <command>`。

3. **Codex CLI 用户**：**当前版本存在配置 bug**（#1943），`rtk init -g --codex` 可能不生效。建议手动将 rtk 使用说明添加到 `.codex/AGENTS.override.md`，或在对话中显式要求 Codex 使用 `rtk` 前缀执行命令。

4. **Windows 用户**：RTK 在 WSL 中工作完美。原生 Windows 下 Hook 不可用，只能使用显式 `rtk <command>` 或 CLAUDE.md/AGENTS.md 回退模式。

---

## 8. 参考来源

- [rtk GitHub 仓库 README](https://github.com/rtk-ai/rtk/blob/develop/README.md)
- [rtk hooks/README.md](https://github.com/rtk-ai/rtk/blob/develop/hooks/README.md)
- [rtk hooks/claude/README.md](https://github.com/rtk-ai/rtk/blob/develop/hooks/claude/README.md)
- [rtk hooks/opencode/README.md](https://github.com/rtk-ai/rtk/blob/develop/hooks/opencode/README.md)
- [rtk hooks/codex/README.md](https://github.com/rtk-ai/rtk/blob/develop/hooks/codex/README.md)
- [rtk GitHub Issues](https://github.com/rtk-ai/rtk/issues)

---

*报告生成时间: 2026-05-20*
