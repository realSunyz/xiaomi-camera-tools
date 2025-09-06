# Xiaomi Camera Tools

A utility written in Golang.

This README file is available in two languages: [English](https://github.com/realSunyz/xiaomi-camera-tools/blob/main/README.md) and [简体中文 (Simplified Chinese)](https://github.com/realSunyz/xiaomi-camera-tools/blob/main/README.CN.md).

## Features

1. Merge Xiaomi camera segmented videos by day
2. Automatically clean up old videos

> [WARNING]
> Currently only supports the new Xiaomi camera file naming format:
> 00_YYYYMMDDHHMMSS_YYYYMMDDHHMMSS (e.g., 00_20250101000000_20250101010000).

## Usage

When deploying for the first time, it’s recommended to run in dry-run mode to verify behavior before actually writing or deleting files.

### Docker (Recommended)

1. Dry Run (no files written and deletions will be performed)

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

2. Regular Run (runs as a scheduled daemon, default every day at 10:00, timezone controlled by `TZ`)

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

3. Manual Trigger (execute once immediately in a running container)

```bash
docker exec -it xiaomi-video /usr/local/bin/xiaomi-video --daemon=false
```

### Local Build / Run

Please make sure [FFmpeg](https://www.ffmpeg.org) is installed first.

```bash
go build -o xiaomi-video ./src

# Dry Run
./xiaomi-video --dir [CAMERA-FOLDER] --out-dir [OUTPUT-FOLDER] --dry-run -v

# Regular Run (once)
./xiaomi-video --dir [CAMERA-FOLDER] --out-dir [OUTPUT-FOLDER] -v
```

## Configuration

## Configuration

| Command-line Argument | Environment Variable             | Meaning                                | Default                            |
| --------------------- | -------------------------------- | -------------------------------------- | ---------------------------------- |
| `--dir`               | `XIAOMI_VIDEO_DIR`               | Input folder to scan                   | `.`                                |
| `--out-dir`           | `XIAOMI_VIDEO_OUT_DIR`           | Output folder                          | Same as `--dir`                    |
| `--out-ext`           | `XIAOMI_VIDEO_OUT_EXT`           | Output file extension                  | `.mp4`                             |
| `--merge`             | `XIAOMI_VIDEO_MERGE`             | Whether to perform merge               | `true`                             |
| `--cleanup`           | `XIAOMI_VIDEO_CLEANUP`           | Whether to perform cleanup             | `true`                             |
| `--days`              | `XIAOMI_VIDEO_DAYS`              | Cleanup threshold (raw segments)       | `14`                               |
| `--merged-days`       | `XIAOMI_VIDEO_MERGED_DAYS`       | Cleanup threshold (merged output)      | `30`                               |
| `--overwrite`         | `XIAOMI_VIDEO_OVERWRITE`         | Overwrite if output already exists     | `false`                            |
| `--dry-run`           | `XIAOMI_VIDEO_DRY_RUN`           | Print planned actions, do not modify   | `false`                            |
| `--genpts`            | `XIAOMI_VIDEO_GENPTS`            | FFmpeg `-fflags +genpts` (rebuild PTS) | `true`                             |
| `--avoid-negative-ts` | `XIAOMI_VIDEO_AVOID_NEGATIVE_TS` | FFmpeg `-avoid_negative_ts make_zero`  | `true`                             |
| `--faststart`         | `XIAOMI_VIDEO_FASTSTART`         | MP4 `-movflags +faststart`             | `true`                             |
| `--mp4-timescale`     | `XIAOMI_VIDEO_MP4_TIMESCALE`     | MP4 `-video_track_timescale`           | `90000`                            |
| `--delete-segments`   | `XIAOMI_VIDEO_DELETE_SEGMENTS`   | Delete segments right after merging    | `false` (keeps 14 days by default) |
| `--skip-today`        | `XIAOMI_VIDEO_SKIP_TODAY`        | Skip today’s videos                    | `true`                             |
| `--daemon`            | `XIAOMI_VIDEO_DAEMON`            | Run in daemon (scheduled) mode         | `false` (Docker default `true`)    |
| `--daily-at`          | `XIAOMI_VIDEO_DAILY_AT`          | Daily execution time (HH:MM)           | `10:00` (used if CRON not set)     |
| `--cron`              | `XIAOMI_VIDEO_CRON`              | CRON expression                        | `0 10 * * *`                       |

## Technical Details

### Video Merging
- **Per-Day Merge:** Segments are grouped by the "start time" date (`YYYYMMDD`), sorted by time, and merged with FFmpeg concat (`-f concat -c copy`) without re-encoding.  
- **Skip Today:** By default, only segments with `end_time < today 00:00` are processed to avoid merging files that are still recording.  
- **Cross-Midnight Segments:** Segments that span midnight are split at 00:00 using `-ss/-t -c copy` and then merged into the correct day.  
- **Timestamps:** By default, uses `-fflags +genpts` and `-avoid_negative_ts make_zero`; for MP4 output, also adds `-movflags +faststart` and `-video_track_timescale 90000`.  
- **Deletion Strategy:** Original segments are not deleted immediately after merging by default. You can enable `--delete-segments` to remove them right away.  
- **Output Naming:** `first_segment_start_last_segment_end.extension`, e.g., `20250101000000_20250101235959.mp4`.

### Video Cleanup
- **Raw Segments:** Retains for 14 days by default, only deletes segments with prefix (e.g., `00_..._...`).  
- **Merged Files:** Retains for 30 days by default, only deletes files without prefix (e.g., `YYYY..._YYYY...`).  

## Contributing

Issues and Pull Requests are definitely welcome!

Please make sure you have tested your code locally before submitting a PR.

## License

This project is licensed under the MIT License - see the [LICENSE](https://github.com/realSunyz/xiaomi-camera-tools/blob/main/LICENSE) file for details.