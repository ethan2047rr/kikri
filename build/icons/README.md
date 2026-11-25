# 图标

Kikri 的标志是 Microsoft [Fluent UI Emoji](https://github.com/microsoft/fluentui-emoji) 里的「Baby chick」，MIT 协议（见 `LICENSE-fluentui-emoji.txt`），水平镜像成朝右，其余未改。

| 文件 | 来源 |
| --- | --- |
| `chick-mono.svg` | High Contrast 版，用于托盘和应用图标 |
| `chick-color.svg` | Flat 版，彩色备用 |

生成的 PNG / icns 已提交，改了 SVG 后运行：

```sh
make icons   # 需要 macOS 14+（NSImage 读取 SVG）
```
