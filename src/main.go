package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

func main() {
	log.SetOutput(os.Stdout)
	log.SetFlags(0)
	log.SetPrefix("")
	cfg := parseFlags()
	daemonMode := strings.TrimSpace(cfg.Cron) != ""
	logInfo("xiaomi-video starting: dir=%s outDir=%s ext=%s rawRetention=%s mergedRetention=%s skipToday=fixed(true) daemon=%v cron='%s'",
		cfg.Dir, cfg.OutDir, mergedOutExt, optionalDaysText(cfg.Days), optionalDaysText(cfg.MergedDays), daemonMode, cfg.Cron)

	if daemonMode {
		logInfo("Daemon mode enabled by CRON='%s' (TZ=%s)", cfg.Cron, os.Getenv("TZ"))
		// First run after startup: rebuild all historical days.
		if err := runOnce(cfg, false); err != nil {
			logError("Run failed: %v", err)
		}
		for {
			next, err := nextCronTime(cfg.Cron, time.Now())
			if err != nil {
				logError("Invalid --cron '%s': %v; fallback to 60s later", cfg.Cron, err)
				next = time.Now().Add(60 * time.Second)
			}
			wait := time.Until(next)
			if wait < 0 {
				wait = 0
			}
			logInfo("Next run at %s (in %s)", next.Format(time.RFC3339), wait.Truncate(time.Second))
			time.Sleep(wait)
			// Scheduled runs: only generate yesterday.
			if err := runOnce(cfg, true); err != nil {
				logError("Run failed: %v", err)
			}
		}
	} else {
		if err := runOnce(cfg, false); err != nil {
			logFatal("Run failed: %v", err)
			os.Exit(1)
		}
	}
}

func runOnce(cfg Config, onlyYesterday bool) error {
	start := time.Now()
	logInfo("Run started at %s", start.Format(time.RFC3339))
	if err := ensureFFmpeg(); err != nil {
		return fmt.Errorf("FFmpeg not found: %w", err)
	}
	if err := mergeByDay(cfg, onlyYesterday); err != nil {
		return err
	}
	if err := cleanupOld(cfg); err != nil {
		return err
	}
	if err := cleanupMerged(cfg); err != nil {
		return err
	}
	logInfo("Run finished in %s", time.Since(start).Truncate(time.Second))
	return nil
}
