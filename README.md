# Xiaomi Camera Tools

A utility written in Golang.

This README file is available in two languages: [English](https://github.com/realSunyz/xiaomi-camera-tools/blob/main/README.md) and [简体中文 (Simplified Chinese)](https://github.com/realSunyz/xiaomi-camera-tools/blob/main/README.CN.md).

## Features

1. Merge Xiaomi camera segmented videos by day
2. Automatically clean up old videos

> [!WARNING]
> Only supports the new Xiaomi camera file naming format:
> `00_YYYYMMDDHHMMSS_YYYYMMDDHHMMSS`

## Usage

| Command-line    | Environment Variable       | Meaning                      | Default          |
| --------------- | -------------------------- | ---------------------------- | ---------------- |
| `--dir`         | `XIAOMI_VIDEO_DIR`         | Input folder                 | `.`              |
| `--out-dir`     | `XIAOMI_VIDEO_OUT_DIR`     | Output folder                | `dir/daily`      |
| `--days`        | `XIAOMI_VIDEO_DAYS`        | Raw-segment retention days   | unset            |
| `--merged-days` | `XIAOMI_VIDEO_MERGED_DAYS` | Merged-output retention days | unset            |
| `--cron`        | `XIAOMI_VIDEO_CRON`        | CRON expression              | empty (run once) |

If `XIAOMI_VIDEO_DAYS` is not set, the original segments will be retained permanently; if set to `0`, they are deleted immediately after merging.

If `XIAOMI_VIDEO_MERGED_DAYS` is not set, the merged output will be retained permanently.

## Contributing

Issues and Pull Requests are definitely welcome!

Please make sure you have tested your code locally before submitting a PR.

## License

This project is licensed under the [MIT License](https://github.com/realSunyz/xiaomi-camera-tools/blob/main/LICENSE).
