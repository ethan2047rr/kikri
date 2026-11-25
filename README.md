# Kikri

> Kikri 是 Kikiriki 的简写，即西班牙语中的公鸡打鸣声。

一个开源免费的菜单栏 / 系统托盘小工具：每天按排班自动给 AI 编程工具（Claude Code、Codex CLI 等）发一条极小的请求，**提前开启 5 小时额度窗口**，让第二轮窗口更早刷新。

- 只调用你本机已登录的官方 CLI，不读取、不复制、不上传任何凭证
- 隔离运行：一次唤醒约 633 tokens（Claude）/ 约 7.9k tokens（Codex）
- 睡眠、断网后按规则补发；用官方额度数据跳过已经在跑的窗口
- macOS + Windows，Go + Wails v3

> 状态：开发中（M1 骨架）。

## License

[MIT](LICENSE)
