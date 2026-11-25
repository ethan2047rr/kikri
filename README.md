# Kikri

> Kikri 是 Kikiriki 的简写，即西班牙语中的公鸡打鸣声。

一个开源免费的菜单栏 / 系统托盘小工具：每天按排班自动给 AI 编程工具（Claude Code、Codex CLI 等）发一条极小的请求，**提前开启 5 小时额度窗口**，让第二轮窗口更早刷新。

- 只调用你本机已登录的官方 CLI，不读取、不复制、不上传任何凭证
- 隔离运行：一次唤醒约 633 tokens（Claude）/ 约 7.9k tokens（Codex）
- 睡眠、断网后按规则补发；用官方额度数据跳过已经在跑的窗口
- macOS + Windows，Go + Wails v3

> 状态：开发中（M1 骨架：托盘 + CLI 探测 + 隔离目录）。

## 开发

需要 Go 1.27+ 和 Node.js（只用于编译 TypeScript 前端）。

```bash
make run        # 编译并启动托盘应用
make app        # 打包 bin/Kikri.app（macOS，未签名）
make windows    # 交叉编译 bin/kikri.exe
make test vet   # Go 测试、go vet、TypeScript 类型检查
make diagnose   # 不启动界面，以 JSON 打印环境探测结果

./bin/kikri -wake claude -dry-run   # 打印将要执行的唤醒命令，不发送
./bin/kikri -wake codex             # 真的发一次唤醒并打印结果（会开启该套餐的 5 小时窗口）
./bin/kikri -plan claude=early-bird # 设置排班：early-bird / three / 06:30,11:35 / off（默认工作日）
./bin/kikri -status                 # 打印排班、下次唤醒时间和最近的日志
```

- `KIKRI_HOME=<dir>`：改用别的数据目录（默认 macOS 为 `~/Library/Application Support/Kikri`，Windows 为 `%AppData%\Kikri`）
- `KIKRI_DEBUG=1`：打开 Wails 调试日志

目录结构：

```
main.go              入口（-diagnose / -wake / -plan / -status / -version）
internal/app         Wails 应用：托盘、弹出窗口、前端调用的 Service
internal/detect      CLI 路径探测（登录 shell → 进程 PATH → 常见路径）、版本、登录状态
internal/appdir      数据目录与空的 sandbox 目录
internal/wake        唤醒执行器：隔离命令、输出解析、错误分类、网络探测、文件锁
internal/schedule    排班：预设、工作日、时间点、补发截止时间（纯函数）
internal/scheduler   调度引擎：每 30 秒检查、补发、重试、Smart Skip、暂停今日、日志
internal/store       SQLite：设置、每个时间点的状态、唤醒日志
internal/quota       Codex 额度只读查询（app-server）
internal/activity    会话文件修改时间（只读 mtime，不读内容）
internal/proc        子进程：stdin 接空设备、强制超时、杀掉整个进程组
internal/gui/assets  前端静态文件（app.js 由 frontend/ 编译生成）
frontend/            TypeScript 源码（esbuild 打包，tsc 类型检查）
build/darwin         Info.plist（LSUIElement：只在菜单栏显示）
```

## License

[MIT](LICENSE)

图标来自 Microsoft [Fluent UI Emoji](https://github.com/microsoft/fluentui-emoji) 的「Baby chick」（MIT，Copyright (c) Microsoft Corporation），镜像后使用，详见 [build/icons](build/icons/README.md)。
