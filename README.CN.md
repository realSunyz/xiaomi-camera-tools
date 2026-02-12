# 小米摄像头视频工具

这是一个使用 Golang 编写的实用工具。

此 README 文件支持两种语言：[English (英语)](https://github.com/realSunyz/xiaomi-camera-tools/blob/main/README.md) 和 [简体中文](https://github.com/realSunyz/xiaomi-camera-tools/blob/main/README.CN.md)。

## 功能

1. 按天合并小米摄像头分段视频
2. 自动清理旧视频

> [!WARNING]
> 目前仅适配新款小米摄像头的命名方式：
> `00_YYYYMMDDHHMMSS_YYYYMMDDHHMMSS.mp4`

## 使用方法

| 命令行参数      | 环境变量                   | 含义             | 默认值         |
| --------------- | -------------------------- | ---------------- | -------------- |
| `--dir`         | `XIAOMI_VIDEO_DIR`         | 输入目录         | `.`            |
| `--out-dir`     | `XIAOMI_VIDEO_OUT_DIR`     | 输出目录         | `dir/daily`    |
| `--days`        | `XIAOMI_VIDEO_DAYS`        | 原始分段保留天数 | 不设置         |
| `--merged-days` | `XIAOMI_VIDEO_MERGED_DAYS` | 合并产物保留天数 | 不设置         |
| `--cron`        | `XIAOMI_VIDEO_CRON`        | CRON 表达式      | 空（单次运行） |

若不设置 `XIAOMI_VIDEO_DAYS`，原分段将被永久保留；该项设置为 `0` 时，合并后会立即删除该日原分段。

若不设置 `XIAOMI_VIDEO_MERGED_DAYS`，合并产物将被永久保留。

## 贡献

我们欢迎 Issues 和 Pull Requests。

请确保您的代码在提交 PR 前已在本地进行测试。

## 许可证

本项目使用 [MIT License](https://github.com/realSunyz/xiaomi-camera-tools/blob/main/LICENSE) 进行授权。
