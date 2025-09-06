# 小米摄像头视频工具

这是一个使用 Golang 编写的实用工具。

此 README 文件支持两种语言：[English (英语)](https://github.com/realSunyz/xiaomi-camera-tools/blob/main/README.md) 和 [简体中文](https://github.com/realSunyz/xiaomi-camera-tools/blob/main/README.CN.md)。

## 功能

1. 按天合并小米摄像头分段视频
2. 自动清理旧视频

> [WARNING]
> 目前仅适配小米新款摄像头的命名方式：`00_YYYYMMDDHHMMSS_YYYYMMDDHHMMSS`（例如：`00_20250101000000_20250101010000`）。

## 使用方法

首次部署建议先以 dry-run 方式试运行一次，确认行为后再执行写入/删除操作。

### Docker（推荐）

1. 试运行（不写文件，不删除）

```bash
docker run --rm \
  -e TZ=Asia/Shanghai \
  -e XIAOMI_VIDEO_DIR=/data/input \
  -e XIAOMI_VIDEO_OUT_DIR=/data/output \
  -e XIAOMI_VIDEO_DAEMON=false \
  -e XIAOMI_VIDEO_DRY_RUN=true \
  -v [CAMERA-FOLDER]:/data/input \
  -v [OUTPUT-FOLDER]:/data/output \
  ghcr.io/realsunyz/xiaomi-video-tools:latest
```

2. 正式运行（常驻定时，默认每日 10:00，时区由 `TZ` 控制）

```bash
docker run -d --name xiaomi-video \
  -e TZ=Asia/Shanghai \
  -e XIAOMI_VIDEO_DIR=/data/input \
  -e XIAOMI_VIDEO_OUT_DIR=/data/output \
  -e XIAOMI_VIDEO_CRON="0 10 * * *" \
  -v [CAMERA-FOLDER]:/data/input \
  -v [OUTPUT-FOLDER]:/data/output \
  ghcr.io/realsunyz/xiaomi-video-tools:latest
```

3. 手动触发一次（在已运行容器内立即执行一次）

```bash
docker exec -it xiaomi-video /usr/local/bin/xiaomi-video --daemon=false
```

### 本地编译/运行

请预先安装 [FFmpeg](https://www.ffmpeg.org)。

```bash
go build -o xiaomi-video ./src

# 试运行
./xiaomi-video --dir [CAMERA-FOLDER] --out-dir [OUTPUT-FOLDER] --dry-run -v

# 正式运行（单次）
./xiaomi-video --dir [CAMERA-FOLDER] --out-dir [OUTPUT-FOLDER] -v
```

## 配置方法

| 命令行参数            | 环境变量                         | 含义                                   | 默认值                          |
| --------------------- | -------------------------------- | -------------------------------------- | ------------------------------- |
| `--dir`               | `XIAOMI_VIDEO_DIR`               | 扫描目录                               | `.`                             |
| `--out-dir`           | `XIAOMI_VIDEO_OUT_DIR`           | 输出目录                               | 与 `--dir` 相同                 |
| `--out-ext`           | `XIAOMI_VIDEO_OUT_EXT`           | 输出扩展名                             | `.mp4`                          |
| `--merge`             | `XIAOMI_VIDEO_MERGE`             | 是否执行合并                           | `true`                          |
| `--cleanup`           | `XIAOMI_VIDEO_CLEANUP`           | 是否执行清理任务                       | `true`                          |
| `--days`              | `XIAOMI_VIDEO_DAYS`              | 清理阈值（原始分段）                   | `14`                            |
| `--merged-days`       | `XIAOMI_VIDEO_MERGED_DAYS`       | 清理阈值（合并产物）                   | `30`                            |
| `--overwrite`         | `XIAOMI_VIDEO_OVERWRITE`         | 若输出已存在是否覆盖                   | `false`                         |
| `--dry-run`           | `XIAOMI_VIDEO_DRY_RUN`           | 只打印计划动作，不修改文件             | `false`                         |
| `--genpts`            | `XIAOMI_VIDEO_GENPTS`            | FFmpeg `-fflags +genpts`（重建 PTS）   | `true`                          |
| `--avoid-negative-ts` | `XIAOMI_VIDEO_AVOID_NEGATIVE_TS` | FFmpeg `-avoid_negative_ts make_zero`  | `true`                          |
| `--faststart`         | `XIAOMI_VIDEO_FASTSTART`         | MP4 `-movflags +faststart`             | `true`                          |
| `--mp4-timescale`     | `XIAOMI_VIDEO_MP4_TIMESCALE`     | MP4 `-video_track_timescale`           | `90000`                         |
| `--delete-segments`   | `XIAOMI_VIDEO_DELETE_SEGMENTS`   | 合并后立刻删除原分段（默认保留 14 天） | `false`                         |
| `--skip-today`        | `XIAOMI_VIDEO_SKIP_TODAY`        | 跳过当日内容                           | `true`                          |
| `--daemon`            | `XIAOMI_VIDEO_DAEMON`            | 守护模式（定时执行）                   | `false`（Docker 默认 `true`）   |
| `--daily-at`          | `XIAOMI_VIDEO_DAILY_AT`          | 每日执行时间（HH:MM）                  | `10:00`（在未设置 CRON 时生效） |
| `--cron`              | `XIAOMI_VIDEO_CRON`              | CRON 表达式                            | `0 10 * * *`                    |

## 技术细节

### 视频合并

- 同日合并：按“开始时间”的日期（YYYYMMDD）分组，按时间排序后使用 FFmpeg concat（`-f concat -c copy`）无重编码拼接。
- 跳过当日：默认仅处理“结束时间 < 今日 00:00”的分段，避免影响当天仍在录制的文件。
- 跨零点分段：在零点处按 `-ss/-t -c copy` 快速切分成当日/次日两段后再分别参与合并。
- 时间戳：默认启用 `-fflags +genpts`、`-avoid_negative_ts make_zero`；若输出为 MP4，另加 `-movflags +faststart` 与 `-video_track_timescale 90000`。
- 删除策略：默认不在合并后立刻删除原分段；可开启 `--delete-segments` 改为即时删除。
- 输出命名：`首段开始_末段结束.扩展名`，如：`20250101000000_20250101235959.mp4`。

### 视频清理

- 原始分段：默认保留 14 天，仅清理含前缀（如 `00_..._...`）的分段。
- 合并产物：默认保留 30 天，仅清理无前缀（如 `YYYY..._YYYY...`）的合并文件。

## 贡献

我们欢迎 Issues 和 Pull Requests。

请确保您的代码在提交 PR 前已通过本地测试。

## 许可证

本项目使用 MIT Licence 进行授权。
