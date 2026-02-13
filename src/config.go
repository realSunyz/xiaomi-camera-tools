package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	envDir        = "XIAOMI_VIDEO_DIR"
	envOutDir     = "XIAOMI_VIDEO_OUT_DIR"
	envDays       = "XIAOMI_VIDEO_DAYS"
	envMergedDays = "XIAOMI_VIDEO_MERGED_DAYS"
	envCron       = "XIAOMI_VIDEO_CRON"
)

func envString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func trimMatchingQuotes(s string) string {
	s = strings.TrimSpace(s)
	for len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			s = strings.TrimSpace(s[1 : len(s)-1])
			continue
		}
		break
	}
	return s
}

func envOptionalInt(key string) (*int, error) {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		i, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return nil, fmt.Errorf("%s must be an integer: %w", key, err)
		}
		if i < 0 {
			return nil, fmt.Errorf("%s must be >= 0", key)
		}
		return &i, nil
	}
	return nil, nil
}

func optionalDaysText(v *int) string {
	if v == nil {
		return "forever"
	}
	return strconv.Itoa(*v)
}

func parseFlags() Config {
	var cfg Config

	cfg.Dir = envString(envDir, ".")
	cfg.OutDir = envString(envOutDir, "")
	cfg.Cron = trimMatchingQuotes(envString(envCron, ""))

	days, err := envOptionalInt(envDays)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid %s: %v\n", envDays, err)
		os.Exit(2)
	}
	cfg.Days = days

	mergedDays, err := envOptionalInt(envMergedDays)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid %s: %v\n", envMergedDays, err)
		os.Exit(2)
	}
	cfg.MergedDays = mergedDays

	flag.StringVar(&cfg.Dir, "dir", cfg.Dir, "Input directory to scan")
	flag.StringVar(&cfg.OutDir, "out-dir", cfg.OutDir, "Output directory for merged files (default: dir/daily)")
	flag.StringVar(&cfg.Cron, "cron", cfg.Cron, "Cron schedule (5 fields: M H DOM MON DOW). If set, daemon mode is enabled")
	flag.Func("days", "Raw segment retention days (unset=keep forever, 0=delete merged-day segments immediately)", func(v string) error {
		i, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || i < 0 {
			return fmt.Errorf("--days must be >= 0")
		}
		cfg.Days = &i
		return nil
	})
	flag.Func("merged-days", "Merged output retention days (unset=keep forever)", func(v string) error {
		i, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || i < 0 {
			return fmt.Errorf("--merged-days must be >= 0")
		}
		cfg.MergedDays = &i
		return nil
	})
	flag.Parse()

	if cfg.OutDir == "" {
		cfg.OutDir = filepath.Join(cfg.Dir, "daily")
	}
	cfg.Cron = trimMatchingQuotes(cfg.Cron)

	return cfg
}
