# Kikri

[English](README.md) | **中文**

> Kikri 是 Kikiriki 的简写，即西班牙语中的公鸡打鸣声。

一个开源免费的菜单栏 / 系统托盘小工具：每天按排班自动给 AI 编程工具（Claude Code、Codex CLI 等）发一条极小的请求，**提前开启 5 小时额度窗口**，让第二轮窗口更早刷新。

- 只调用你本机已登录的官方 CLI，不读取、不复制、不上传任何凭证
- 隔离运行：一次唤醒约 633 tokens（Claude）/ 约 5.2k tokens（Codex）
- 睡眠、断网后按规则补发；用官方额度数据跳过已经在跑的窗口
- 显示每个窗口几点刷新、用了多少
- macOS + Windows，中文 / English，Go + Wails v3

> 状态：v0.1 准备发布。macOS 上已日常使用；Windows 版能编译，但还没在真机上验证。

## 工作原理

Claude 和 ChatGPT 的套餐按 5 小时窗口计量用量，窗口从你的第一条请求开始算。如果你 09:00 开始干活，第一个窗口到 14:00 才刷新；如果 Kikri 在 06:30 先唤醒一次，窗口在 11:30 就刷新了，下一个完整的窗口落在你的工作时间中间。

每次唤醒都在一个空目录里运行 CLI（不会带上任何项目文件），只要一个字的回复，不保存会话，并记进日志。

## 安装

先装好并登录 [Claude Code](https://code.claude.com) 或 [Codex CLI](https://github.com/openai/codex)（至少一个）。Kikri 只调用它们，不需要任何密钥。

### macOS（13 及以上，Apple 芯片和 Intel 通用）

1. 从 [Releases](../../releases) 下载 `Kikri-<版本>-macos.zip`，解压后把 **Kikri.app** 拖进「应用程序」。
2. 第一次打开时，系统会提示「无法验证开发者」：Kikri 没有付费的 Apple 开发者证书（只做了本机签名）。
   - 点「完成」，然后打开 **系统设置 → 隐私与安全性**，滚到底部，在 Kikri 那一行点 **「仍要打开」**。
   - 或者在终端运行：`xattr -dr com.apple.quarantine /Applications/Kikri.app`
3. Kikri 只在菜单栏显示（一只小鸡），没有 Dock 图标。点它，按引导选好唤醒时间即可。

### Windows（10 / 11）

1. 从 [Releases](../../releases) 下载 `Kikri-<版本>-windows-amd64.zip`，解压到一个固定的位置（比如 `%LocalAppData%\Programs\Kikri`）。
2. 双击 `kikri.exe`。SmartScreen 提示「Windows 已保护你的电脑」时，点 **「更多信息」→「仍要运行」**（程序没有代码签名证书）。
3. Kikri 在任务栏右下角的托盘里。界面使用系统自带的 WebView2（Windows 11 自带；Windows 10 装了新版 Edge 就有）。

### 校验下载

每个 Release 附带 `SHA256SUMS.txt`：

```bash
shasum -a 256 -c SHA256SUMS.txt --ignore-missing   # macOS
```

```powershell
Get-FileHash .\Kikri-<版本>-windows-amd64.zip -Algorithm SHA256   # Windows，和 SHA256SUMS.txt 对照
```

## Claude Code 状态栏

Claude 没有只读的额度查询接口，刷新时间唯一的官方来源是 Claude Code 交给状态栏的数据。Kikri 默认在 `~/.claude/settings.json` 的状态栏命令前加一层转发：

- 只保存额度信息（5 小时和每周的用量、刷新时间），再用同样的输入运行你原来的命令，状态栏显示不变。
- 原本没有状态栏的，Kikri 会显示一行：`5h 23% · 18:30 刷新 · 本周 54%`。
- 原配置会先备份，在设置里关掉就还原。

首次引导时可以不勾，之后也能在设置里关掉。不打开的话，Kikri 根据你最近使用 Claude Code 的时间估算刷新时间。

## 卸载

1. 打开 Kikri → 设置，**先关掉「读取 Claude Code 状态栏」**（会把 `~/.claude/settings.json` 的状态栏还原成你原来的样子），再关掉「开机自启」。忘了关就删了 App 也不要紧：macOS 上状态栏命令会自动退回到你原来的命令，只是 settings.json 里还留着 Kikri 那一段。
2. 退出 Kikri，删除 Kikri.app（Windows 删除解压的文件夹）。
3. 删除数据目录：macOS `~/Library/Application Support/Kikri`，Windows `%AppData%\Kikri`。

## 开发

需要 Go 1.27+ 和 Node.js（只用于编译 TypeScript 前端）。

```bash
make run        # 编译并启动托盘应用
make app        # 打包 bin/Kikri.app（macOS，未签名）
make windows    # 交叉编译 bin/kikri.exe
make dist       # 发布用压缩包：通用 Kikri.app、Windows exe、SHA256SUMS（在 bin/dist）
make test vet   # Go 测试、go vet、TypeScript 类型检查
make diagnose   # 不启动界面，以 JSON 打印环境探测结果

./bin/kikri -wake claude -dry-run   # 打印将要执行的唤醒命令，不发送
./bin/kikri -wake codex             # 真的发一次唤醒并打印结果（会开启该套餐的 5 小时窗口）
./bin/kikri -plan claude=early-bird # 设置排班：early-bird / three / 06:30,11:35 / off（默认工作日）
./bin/kikri -status                 # 打印排班、下次唤醒时间、额度用量和最近的日志
```

- `KIKRI_HOME=<dir>`：改用别的数据目录（默认 macOS 为 `~/Library/Application Support/Kikri`，Windows 为 `%AppData%\Kikri`）
- `KIKRI_DEBUG=1`：打开 Wails 调试日志

目录结构：

```
main.go              入口（-diagnose / -wake / -plan / -status / -version）
internal/app         Wails 应用：托盘、弹出窗口、前端调用的 Service、通知
internal/detect      CLI 路径探测（登录 shell → 进程 PATH → 常见路径）、版本、登录状态
internal/appdir      数据目录与空的 sandbox 目录
internal/wake        唤醒执行器：隔离命令、输出解析、错误分类、网络探测、文件锁
internal/schedule    排班：预设、工作日、时间点、补发截止时间（纯函数）
internal/scheduler   调度引擎：每 30 秒检查、补发、重试、Smart Skip、暂停今日、日志
internal/store       SQLite：设置、每个时间点的状态、唤醒日志
internal/quota       Codex 额度只读查询（app-server）
internal/activity    会话文件修改时间（只读 mtime，不读内容）
internal/statusline  从 Claude Code 状态栏数据读取 Claude 的刷新时间（默认打开，设置里关掉即还原）
internal/locale      界面语言：跟随系统，或用户自选
internal/proc        子进程：stdin 接空设备、强制超时、杀掉整个进程组
internal/autostart   开机自启（macOS LaunchAgent / Windows 注册表 Run）
internal/gui/assets  前端静态文件（app.js 由 frontend/ 编译生成）
frontend/src         弹出窗口：状态、日志、设置、首次引导（TypeScript，无框架）；文案在 i18n.ts
frontend/            TypeScript 源码（esbuild 打包，tsc 类型检查）
build/darwin         Info.plist（LSUIElement：只在菜单栏显示）
.github/workflows    推送 v* 标签时测试、构建两个平台的压缩包并发布 Release
```

## License

[MIT](LICENSE)

图标来自 Microsoft [Fluent UI Emoji](https://github.com/microsoft/fluentui-emoji) 的「Baby chick」（MIT，Copyright (c) Microsoft Corporation），镜像后使用，详见 [build/icons](build/icons/README.md)。

弹出窗口里的 Claude 和 OpenAI 标志来自 [lobe-icons](https://github.com/lobehub/lobe-icons)（MIT，Copyright (c) 2023 LobeHub），见 `frontend/src/logos.ts`。这些标志是 Anthropic 和 OpenAI 的商标，Kikri 只用它们标明卡片对应哪个服务，与这两家公司没有关联。
