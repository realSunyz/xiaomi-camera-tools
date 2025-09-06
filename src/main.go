package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Segment struct {
	Path      string
	StartTime time.Time
	EndTime   time.Time
	Ext       string
}

var (
	// <prefix>_YYYYMMDDHHMMSS_YYYYMMDDHHMMSS[.ext]
	reWithPrefix = regexp.MustCompile(`^(?P<prefix>[^_]+)_(?P<start>\d{14})_(?P<end>\d{14})(?P<ext>\..+)?$`)
	// YYYYMMDDHHMMSS_YYYYMMDDHHMMSS[.ext]
	reMerged = regexp.MustCompile(`^(?P<start>\d{14})_(?P<end>\d{14})(?P<ext>\..+)?$`)
	tsLayout = "20060102150405"
)

func parseSegment(name string) (start, end time.Time, ext string, ok bool) {
	if m := reWithPrefix.FindStringSubmatch(name); m != nil {
		s := m[reWithPrefix.SubexpIndex("start")]
		e := m[reWithPrefix.SubexpIndex("end")]
		ex := m[reWithPrefix.SubexpIndex("ext")]
		st, err1 := time.ParseInLocation(tsLayout, s, time.Local)
		et, err2 := time.ParseInLocation(tsLayout, e, time.Local)
		if err1 == nil && err2 == nil {
			return st, et, ex, true
		}
		return time.Time{}, time.Time{}, "", false
	}
	if m := reMerged.FindStringSubmatch(name); m != nil {
		s := m[reMerged.SubexpIndex("start")]
		e := m[reMerged.SubexpIndex("end")]
		ex := m[reMerged.SubexpIndex("ext")]
		st, err1 := time.ParseInLocation(tsLayout, s, time.Local)
		et, err2 := time.ParseInLocation(tsLayout, e, time.Local)
		if err1 == nil && err2 == nil {
			return st, et, ex, true
		}
	}
	return time.Time{}, time.Time{}, "", false
}

type DayGroup struct {
	Day      string
	Segments []Segment
}

type Config struct {
	Dir        string
	OutDir     string
	OutExt     string
	DoMerge    bool
	DoCleanup  bool
    Days       int
    MergedDays int
	Overwrite  bool
	DryRun     bool
	Verbose    bool
    GenPTS     bool
    DeleteSegs bool
    SkipToday  bool
    Daemon     bool
    DailyAt    string
    Cron       string
    AvoidNegTS bool
    FastStart  bool
    MP4Scale   int
}

var gVerbose bool
var currentCfg Config

func logInfo(format string, args ...any)  { log.Printf("[INFO] "+format, args...) }
func logWarn(format string, args ...any)  { log.Printf("[WARN] "+format, args...) }
func logError(format string, args ...any) { log.Printf("[ERROR] "+format, args...) }
func logDebug(format string, args ...any) {
	if gVerbose {
		log.Printf("[DEBUG] "+format, args...)
	}
}

func main() {
    cfg := parseFlags()
    log.SetOutput(os.Stdout)
    gVerbose = cfg.Verbose
    currentCfg = cfg
    logInfo("xiaomi-video starting: dir=%s outDir=%s outExt=%s merge=%v cleanup=%v days=%d mergedDays=%d overwrite=%v dryRun=%v genpts=%v avoidNegTS=%v faststart=%v mp4Timescale=%d deleteSegments=%v skipToday=%v daemon=%v dailyAt=%s cron='%s'",
        cfg.Dir, cfg.OutDir, cfg.OutExt, cfg.DoMerge, cfg.DoCleanup, cfg.Days, cfg.MergedDays, cfg.Overwrite, cfg.DryRun, cfg.GenPTS, cfg.AvoidNegTS, cfg.FastStart, cfg.MP4Scale, cfg.DeleteSegs, cfg.SkipToday, cfg.Daemon, cfg.DailyAt, cfg.Cron)

	if cfg.Daemon {
		if strings.TrimSpace(cfg.Cron) != "" {
			logInfo("Daemon mode using CRON='%s' (TZ=%s)", cfg.Cron, os.Getenv("TZ"))
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
				if err := runOnce(cfg); err != nil {
					logError("Run failed: %v", err)
				}
			}
		} else {
			logInfo("Daemon mode enabled; schedule daily at %s (TZ=%s)", cfg.DailyAt, os.Getenv("TZ"))
			for {
				next, err := nextRunTime(cfg.DailyAt)
				if err != nil {
					logError("Invalid --daily-at '%s': %v; fallback to 24h later", cfg.DailyAt, err)
					next = time.Now().Add(24 * time.Hour)
				}
				wait := time.Until(next)
				if wait < 0 {
					wait = 0
				}
				logInfo("Next run at %s (in %s)", next.Format(time.RFC3339), wait.Truncate(time.Second))
				time.Sleep(wait)
				if err := runOnce(cfg); err != nil {
					logError("Run failed: %v", err)
				}
			}
		}
	} else {
		if err := runOnce(cfg); err != nil {
			log.Fatalf("[FATAL] run failed: %v", err)
		}
	}
}

func runOnce(cfg Config) error {
	start := time.Now()
	logInfo("Run started at %s", start.Format(time.RFC3339))
	if cfg.DoMerge {
		if err := ensureFFmpeg(); err != nil {
			return fmt.Errorf("ffmpeg not found: %w", err)
		}
		if err := mergeByDay(cfg); err != nil {
			return err
		}
	}
    if cfg.DoCleanup {
        if err := cleanupOld(cfg); err != nil { return err }
        if err := cleanupMerged(cfg); err != nil { return err }
    }
	logInfo("Run finished in %s", time.Since(start).Truncate(time.Second))
	return nil
}

func nextRunTime(hhmm string) (time.Time, error) {
    parts := strings.Split(hhmm, ":")
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf("invalid HH:MM: %s", hhmm)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return time.Time{}, fmt.Errorf("invalid hour: %s", parts[0])
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return time.Time{}, fmt.Errorf("invalid minute: %s", parts[1])
	}
	now := time.Now()
	candidate := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, time.Local)
	if candidate.After(now) {
		return candidate, nil
	}
	n := now.Add(24 * time.Hour)
	return time.Date(n.Year(), n.Month(), n.Day(), h, m, 0, 0, time.Local), nil
}

func nextCronTime(spec string, now time.Time) (time.Time, error) {
	fields := strings.Fields(spec)
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("cron requires 5 fields (M H DOM MON DOW): %s", spec)
	}
	mins, err := parseCronField(fields[0], 0, 59)
	if err != nil {
		return time.Time{}, fmt.Errorf("minute: %w", err)
	}
	hours, err := parseCronField(fields[1], 0, 23)
	if err != nil {
		return time.Time{}, fmt.Errorf("hour: %w", err)
	}
	dom, domWild, err := parseCronFieldWild(fields[2], 1, 31)
	if err != nil {
		return time.Time{}, fmt.Errorf("dom: %w", err)
	}
	mon, err := parseCronField(fields[3], 1, 12)
	if err != nil {
		return time.Time{}, fmt.Errorf("mon: %w", err)
	}
	dow, dowWild, err := parseCronFieldWild(fields[4], 0, 6)
	if err != nil {
		return time.Time{}, fmt.Errorf("dow: %w", err)
	}

	t := now.Add(time.Minute).Truncate(time.Minute)
	limit := t.AddDate(1, 1, 0)
	for t.Before(limit) {
		if !mins[t.Minute()] {
			t = t.Add(time.Minute - time.Duration(t.Second())*time.Second)
			continue
		}
		if !hours[t.Hour()] {
			t = t.Add(time.Hour).Truncate(time.Hour)
			continue
		}
		if !mon[int(t.Month())] {
			t = t.AddDate(0, 1, 0).Add(-time.Duration(t.Hour()) * time.Hour).Add(-time.Duration(t.Minute()) * time.Minute).Truncate(time.Hour)
			continue
		}
		d_match := dom[t.Day()]
		wd := int(t.Weekday())
		if wd == 7 {
			wd = 0
		}
		w_match := dow[wd]
		ok := false
		if domWild && dowWild {
			ok = true
		} else if domWild {
			ok = w_match
		} else if dowWild {
			ok = d_match
		} else {
			ok = d_match || w_match
		}
		if ok {
			return t, nil
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("no matching time within search window for cron '%s'", spec)
}

func parseCronFieldWild(expr string, min, max int) (map[int]bool, bool, error) {
	if strings.TrimSpace(expr) == "*" {
		m := make(map[int]bool, max-min+1)
		for v := min; v <= max; v++ {
			m[v] = true
		}
		return m, true, nil
	}
	m, err := parseCronField(expr, min, max)
	return m, false, err
}

func parseCronField(expr string, min, max int) (map[int]bool, error) {
	m := make(map[int]bool, max-min+1)
	add := func(v int) error {
		if v < min || v > max {
			return fmt.Errorf("value %d out of range [%d,%d]", v, min, max)
		}
		m[v] = true
		return nil
	}
	parts := strings.Split(expr, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "*" {
			for v := min; v <= max; v++ {
				m[v] = true
			}
			continue
		}
		step := 1
		base := p
		if strings.Contains(p, "/") {
			ss := strings.SplitN(p, "/", 2)
			base = ss[0]
			s, err := strconv.Atoi(ss[1])
			if err != nil || s <= 0 {
				return nil, fmt.Errorf("invalid step '%s'", ss[1])
			}
			step = s
		}
		if strings.Contains(base, "-") {
			rr := strings.SplitN(base, "-", 2)
			lo, err1 := strconv.Atoi(rr[0])
			hi, err2 := strconv.Atoi(rr[1])
			if err1 != nil || err2 != nil || lo > hi {
				return nil, fmt.Errorf("invalid range '%s'", base)
			}
			for v := lo; v <= hi; v += step {
				if err := add(v); err != nil {
					return nil, err
				}
			}
			continue
		}
		if base == "" {
			for v := min; v <= max; v += step {
				m[v] = true
			}
			continue
		}
		iv, err := strconv.Atoi(base)
		if err != nil {
			return nil, fmt.Errorf("invalid value '%s'", base)
		}
		if err := add(iv); err != nil {
			return nil, err
		}
	}
	if len(m) == 0 {
		return nil, fmt.Errorf("empty field after parsing: %s", expr)
	}
	return m, nil
}

const (
	envDir        = "XIAOMI_VIDEO_DIR"
	envOutDir     = "XIAOMI_VIDEO_OUT_DIR"
	envOutExt     = "XIAOMI_VIDEO_OUT_EXT"
	envMerge      = "XIAOMI_VIDEO_MERGE"
	envCleanup    = "XIAOMI_VIDEO_CLEANUP"
    envDays       = "XIAOMI_VIDEO_DAYS"
    envMergedDays = "XIAOMI_VIDEO_MERGED_DAYS"
	envOverwrite  = "XIAOMI_VIDEO_OVERWRITE"
	envDryRun     = "XIAOMI_VIDEO_DRY_RUN"
	envVerbose    = "XIAOMI_VIDEO_VERBOSE"
	envGenPTS     = "XIAOMI_VIDEO_GENPTS"
	envDeleteSegs = "XIAOMI_VIDEO_DELETE_SEGMENTS"
	envSkipToday  = "XIAOMI_VIDEO_SKIP_TODAY"
	envDaemon     = "XIAOMI_VIDEO_DAEMON"
	envDailyAt    = "XIAOMI_VIDEO_DAILY_AT"
    envCron       = "XIAOMI_VIDEO_CRON"
    envAvoidNegTS = "XIAOMI_VIDEO_AVOID_NEGATIVE_TS"
    envFastStart  = "XIAOMI_VIDEO_FASTSTART"
    envMP4Scale   = "XIAOMI_VIDEO_MP4_TIMESCALE"
)

func envString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		vv := strings.ToLower(strings.TrimSpace(v))
		switch vv {
		case "1", "true", "t", "yes", "y", "on":
			return true
		case "0", "false", "f", "no", "n", "off":
			return false
		}
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return i
		}
	}
	return def
}

func parseFlags() Config {
	defDir := envString(envDir, ".")
	defOutDir := envString(envOutDir, "")
	defOutExt := envString(envOutExt, ".mp4")
	defMerge := envBool(envMerge, true)
	defCleanup := envBool(envCleanup, true)
    defDays := envInt(envDays, 14)
    defMergedDays := envInt(envMergedDays, 30)
	defOverwrite := envBool(envOverwrite, false)
	defDryRun := envBool(envDryRun, false)
    defVerbose := envBool(envVerbose, false)
    defGenPTS := envBool(envGenPTS, true)
    defDelete := envBool(envDeleteSegs, false)
    defSkipToday := envBool(envSkipToday, true)
    defDaemon := envBool(envDaemon, false)
    defDailyAt := envString(envDailyAt, "10:00")
    defCron := envString(envCron, "0 10 * * *")
    defAvoidNegTS := envBool(envAvoidNegTS, true)
    defFastStart := envBool(envFastStart, true)
    defMP4Scale := envInt(envMP4Scale, 90000)

	var cfg Config
	flag.StringVar(&cfg.Dir, "dir", defDir, "Input directory to scan")
	flag.StringVar(&cfg.OutDir, "out-dir", defOutDir, "Output directory for merged files (default: same as dir)")
	flag.StringVar(&cfg.OutExt, "out-ext", defOutExt, "Extension for merged output (e.g., .mp4)")
	flag.BoolVar(&cfg.DoMerge, "merge", defMerge, "Run daily merge")
	flag.BoolVar(&cfg.DoCleanup, "cleanup", defCleanup, "Delete videos older than --days")
    flag.IntVar(&cfg.Days, "days", defDays, "Delete files older than N days (by end timestamp)")
    flag.IntVar(&cfg.MergedDays, "merged-days", defMergedDays, "Delete merged outputs older than N days (by end timestamp)")
	flag.BoolVar(&cfg.Overwrite, "overwrite", defOverwrite, "Overwrite existing merged outputs")
	flag.BoolVar(&cfg.DryRun, "dry-run", defDryRun, "Show actions without making changes")
	flag.BoolVar(&cfg.Verbose, "v", defVerbose, "Verbose logging")
    flag.BoolVar(&cfg.GenPTS, "genpts", defGenPTS, "Use -fflags +genpts (rebuild PTS)")
    flag.BoolVar(&cfg.DeleteSegs, "delete-segments", defDelete, "Delete original segments after successful merge")
    flag.BoolVar(&cfg.SkipToday, "skip-today", defSkipToday, "Skip processing content that belongs to today (default true)")
    flag.BoolVar(&cfg.Daemon, "daemon", defDaemon, "Run as a daily scheduler (do not exit)")
    flag.StringVar(&cfg.DailyAt, "daily-at", defDailyAt, "Daily time to run in HH:MM (local time). Deprecated if --cron is set")
    flag.StringVar(&cfg.Cron, "cron", defCron, "Cron schedule (5 fields: M H DOM MON DOW). Overrides --daily-at if set")
    flag.BoolVar(&cfg.AvoidNegTS, "avoid-negative-ts", defAvoidNegTS, "Apply -avoid_negative_ts make_zero (default on)")
    flag.BoolVar(&cfg.FastStart, "faststart", defFastStart, "Apply -movflags +faststart for MP4 (default on)")
    flag.IntVar(&cfg.MP4Scale, "mp4-timescale", defMP4Scale, "Apply -video_track_timescale for MP4 (default 90000)")
	flag.Parse()

	if cfg.OutDir == "" {
		cfg.OutDir = cfg.Dir
	}
	if !strings.HasPrefix(cfg.OutExt, ".") {
		cfg.OutExt = "." + cfg.OutExt
	}
	return cfg
}

func ensureFFmpeg() error {
	_, err := exec.LookPath("ffmpeg")
	return err
}

func collectSegments(root string) ([]Segment, error) {
	segments := make([]Segment, 0, 1024)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		s, e, ext, ok := parseSegment(base)
		if !ok {
			return nil
		}
		segments = append(segments, Segment{Path: path, StartTime: s, EndTime: e, Ext: ext})
		return nil
	})
	logInfo("Found %d segments in %s", len(segments), root)
	return segments, err
}

func groupByDay(segs []Segment) map[string]*DayGroup {
	groups := make(map[string]*DayGroup)
	for _, s := range segs {
		day := s.StartTime.Format("20060102")
		g, ok := groups[day]
		if !ok {
			g = &DayGroup{Day: day}
			groups[day] = g
		}
		g.Segments = append(g.Segments, s)
	}
	for _, g := range groups {
		sort.Slice(g.Segments, func(i, j int) bool { return g.Segments[i].StartTime.Before(g.Segments[j].StartTime) })
	}
	return groups
}

func mergeByDay(cfg Config) error {
	segs, err := collectSegments(cfg.Dir)
	if err != nil {
		return err
	}
	if len(segs) == 0 {
		logInfo("No segments detected; nothing to merge")
		return nil
	}

	segsEligible := segs
	if cfg.SkipToday {
		now := time.Now()
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
		tmp := make([]Segment, 0, len(segs))
		for _, s := range segs {
			if s.EndTime.Before(todayStart) {
				tmp = append(tmp, s)
			}
		}
		logInfo("Skip-today: eligible %d/%d (end < %s)", len(tmp), len(segs), todayStart.Format(time.RFC3339))
		segsEligible = tmp
		if len(segsEligible) == 0 {
			logInfo("No segments to process after skip-today filter")
			return nil
		}
	}

	segsPre, tempCleanup, err := splitCrossDaySegments(segsEligible, cfg)
	if err != nil {
		return err
	}
	defer tempCleanup()

	groups := groupByDay(segsPre)
	days := make([]string, 0, len(groups))
	for day := range groups {
		days = append(days, day)
	}
	sort.Strings(days)

	var mergeErr error
	successDays := 0
	for _, day := range days {
		g := groups[day]
		if len(g.Segments) == 0 {
			continue
		}
		first := g.Segments[0]
		last := g.Segments[len(g.Segments)-1]

		if first.StartTime.Format("20060102") != day || last.EndTime.Format("20060102") != day {
			logWarn("Skip merge for %s due to cross-day parts after preprocessing", day)
			mergeErr = fmt.Errorf("group %s not single-day after preprocessing", day)
			continue
		}
		outName := fmt.Sprintf("%s_%s%s", first.StartTime.Format(tsLayout), last.EndTime.Format(tsLayout), cfg.OutExt)
		outPath := filepath.Join(cfg.OutDir, outName)

		if !cfg.Overwrite {
			if _, err := os.Stat(outPath); err == nil {
				logInfo("Skip day %s: output exists %s", day, outPath)
				continue
			}
		}

		if cfg.DryRun {
			logInfo("[dry-run] Merge %d segments -> %s", len(g.Segments), outPath)
			continue
		}

		if err := os.MkdirAll(cfg.OutDir, 0o755); err != nil {
			return fmt.Errorf("create out dir: %w", err)
		}

		listFile, cleanup, err := writeConcatList(g.Segments)
		if err != nil {
			return fmt.Errorf("make concat list: %w", err)
		}
		defer cleanup()

		logInfo("Merging day %s: %d segments -> %s", day, len(g.Segments), outPath)
		if err := runFFmpegConcat(listFile, outPath, cfg.GenPTS); err != nil {
			logError("Merge failed for day %s: %v", day, err)
			mergeErr = err
			continue
		}
		logInfo("Done day %s -> %s", day, outPath)
		successDays++
	}
	if mergeErr != nil {
		logWarn("Merging finished with errors; successful days: %d", successDays)
		return mergeErr
	}
	logInfo("Merging finished; successful days: %d", successDays)

	if cfg.DeleteSegs {
		if err := deleteFiles(segsEligible, cfg); err != nil {
			return err
		}
	}
	return nil
}

func splitCrossDaySegments(segs []Segment, cfg Config) ([]Segment, func(), error) {
	temps := make([]string, 0)
	cleanup := func() {
		for _, p := range temps {
			_ = os.Remove(p)
		}
	}

	out := make([]Segment, 0, len(segs))
	for _, s := range segs {
		if s.StartTime.Format("20060102") == s.EndTime.Format("20060102") {
			out = append(out, s)
			continue
		}

		totalDur := int(s.EndTime.Sub(s.StartTime).Seconds())
		if totalDur <= 0 {
			logWarn("Skip invalid duration segment: %s", s.Path)
			continue
		}

		type part struct {
			off int
			dur int
			ps  time.Time
			pe  time.Time
		}
		parts := []part{}
		cur := s.StartTime
		offset := 0
		for cur.Before(s.EndTime) {
			dayEnd := time.Date(cur.Year(), cur.Month(), cur.Day(), 23, 59, 59, int(time.Second-time.Nanosecond), cur.Location())
			end := s.EndTime
			if dayEnd.Before(end) {
				end = dayEnd
			}
			dur := int(end.Sub(cur).Seconds()) + 1
			if dur <= 0 {
				break
			}
			parts = append(parts, part{off: offset, dur: dur, ps: cur, pe: end})
			offset += dur
			cur = dayEnd.Add(time.Second)
		}

		for i, p := range parts {
			ext := s.Ext
			if ext == "" {
				ext = cfg.OutExt
			}
			tf, err := os.CreateTemp("", fmt.Sprintf("split_%d_*%s", i, ext))
			if err != nil {
				cleanup()
				return nil, func() {}, err
			}
			tmp := tf.Name()
			_ = tf.Close()
			temps = append(temps, tmp)

			if cfg.DryRun {
				logInfo("[dry-run] Split %s (off=%ds dur=%ds)", s.Path, p.off, p.dur)
				continue
			}

			args := []string{"-y"}
			if p.off > 0 {
				args = append(args, "-ss", fmt.Sprintf("%d", p.off))
			}
            args = append(args, "-i", s.Path, "-t", fmt.Sprintf("%d", p.dur))
            if cfg.GenPTS { args = append(args, "-fflags", "+genpts") }
            // Output options before output file
            args = append(args, "-c", "copy")
            if cfg.AvoidNegTS { args = append(args, "-avoid_negative_ts", "make_zero") }
            if strings.EqualFold(ext, ".mp4") {
                if cfg.FastStart { args = append(args, "-movflags", "+faststart") }
                if cfg.MP4Scale > 0 { args = append(args, "-video_track_timescale", fmt.Sprintf("%d", cfg.MP4Scale)) }
            }
            args = append(args, tmp)
			logDebug("ffmpeg trim %s -> %s (off=%ds dur=%ds)", s.Path, tmp, p.off, p.dur)
			if err := runFFmpeg(args); err != nil {
				cleanup()
				return nil, func() {}, fmt.Errorf("ffmpeg split failed: %w", err)
			}
			out = append(out, Segment{Path: tmp, StartTime: p.ps, EndTime: p.pe, Ext: ext})
		}
	}
	return out, cleanup, nil
}

func runFFmpeg(args []string) error {
	cmd := exec.Command("ffmpeg", args...)
	var stdout, stderr io.ReadCloser
	var err error
	if stdout, err = cmd.StdoutPipe(); err != nil {
		return err
	}
	if stderr, err = cmd.StderrPipe(); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go io.Copy(os.Stdout, stdout)
	go io.Copy(os.Stderr, stderr)
	return cmd.Wait()
}

func deleteFiles(segs []Segment, cfg Config) error {
	seen := map[string]struct{}{}
	var files []string
	for _, s := range segs {
		if _, ok := seen[s.Path]; ok {
			continue
		}
		seen[s.Path] = struct{}{}
		files = append(files, s.Path)
	}
	sort.Strings(files)
	logInfo("Deleting %d original segment(s)", len(files))
	for _, p := range files {
		if cfg.DryRun {
			logInfo("[dry-run] Delete original %s", p)
			continue
		}
		logDebug("Deleting original %s", p)
		if err := os.Remove(p); err != nil {
			return fmt.Errorf("delete %s: %w", p, err)
		}
	}
	return nil
}

func writeConcatList(segs []Segment) (string, func(), error) {
	f, err := os.CreateTemp("", "concat_*.txt")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() {
		_ = os.Remove(f.Name())
	}
	w := bufio.NewWriter(f)
	for _, s := range segs {
		abs, err := filepath.Abs(s.Path)
		if err != nil {
			_ = f.Close()
			cleanup()
			return "", func() {}, err
		}
		safe := strings.ReplaceAll(abs, "'", "'\\''")
		if _, err := fmt.Fprintf(w, "file '%s'\n", safe); err != nil {
			_ = f.Close()
			cleanup()
			return "", func() {}, err
		}
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return f.Name(), cleanup, nil
}

func runFFmpegConcat(listFile, outPath string, genpts bool) error {
    args := []string{"-y", "-f", "concat", "-safe", "0", "-i", listFile}
    if genpts {
        args = append(args, "-fflags", "+genpts")
    }
    args = append(args, "-c", "copy")
    // Global options for output
    if currentCfg.AvoidNegTS { args = append(args, "-avoid_negative_ts", "make_zero") }
    // If mp4 output, apply mp4-related flags
    if strings.EqualFold(filepath.Ext(outPath), ".mp4") {
        if currentCfg.FastStart { args = append(args, "-movflags", "+faststart") }
        if currentCfg.MP4Scale > 0 { args = append(args, "-video_track_timescale", fmt.Sprintf("%d", currentCfg.MP4Scale)) }
    }
    args = append(args, outPath)

    cmd := exec.Command("ffmpeg", args...)
	var stdout, stderr io.ReadCloser
	var err error
	if stdout, err = cmd.StdoutPipe(); err != nil {
		return err
	}
	if stderr, err = cmd.StderrPipe(); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go io.Copy(os.Stdout, stdout)
	go io.Copy(os.Stderr, stderr)
	if err := cmd.Wait(); err != nil {
		return err
	}
	return nil
}

func cleanupOld(cfg Config) error {
    cutoff := time.Now().AddDate(0, 0, -cfg.Days)
    var toDelete []string
    err := filepath.WalkDir(cfg.Dir, func(path string, d os.DirEntry, err error) error {
        if err != nil {
            return err
        }
        if d.IsDir() {
            return nil
        }
        base := filepath.Base(path)
        // 仅清理“原始分段”文件：要求带前缀的命名，如 00_YYYY..._YYYY...
        if !reWithPrefix.MatchString(base) {
            return nil
        }
        s, e, _, ok := parseSegment(base)
        if !ok {
            return nil
        }
        if e.Before(s) {
            return nil
        }
        if e.Before(cutoff) {
            toDelete = append(toDelete, path)
        }
        return nil
    })
	if err != nil {
		return err
	}

	if len(toDelete) == 0 {
		logInfo("Cleanup: no files older than %d days", cfg.Days)
		return nil
	}
	sort.Strings(toDelete)
	logInfo("Cleanup: deleting %d file(s) older than %d days (end < %s)", len(toDelete), cfg.Days, cutoff.Format(time.RFC3339))
	for _, p := range toDelete {
		if cfg.DryRun {
			logInfo("[dry-run] Delete %s", p)
			continue
		}
		logDebug("Deleting %s", p)
		if err := os.Remove(p); err != nil {
			logWarn("Failed to delete %s: %v", p, err)
		}
	}
	return nil
}

// cleanupMerged removes merged output files older than cfg.MergedDays by end timestamp.
func cleanupMerged(cfg Config) error {
    cutoff := time.Now().AddDate(0, 0, -cfg.MergedDays)
    var toDelete []string
    err := filepath.WalkDir(cfg.OutDir, func(path string, d os.DirEntry, err error) error {
        if err != nil { return err }
        if d.IsDir() { return nil }
        base := filepath.Base(path)
        if !reMerged.MatchString(base) { return nil }
        s, e, _, ok := parseSegment(base)
        if !ok { return nil }
        if e.Before(s) { return nil }
        if e.Before(cutoff) { toDelete = append(toDelete, path) }
        return nil
    })
    if err != nil { return err }
    if len(toDelete) == 0 {
        logInfo("Cleanup(merged): no files older than %d days in %s", cfg.MergedDays, cfg.OutDir)
        return nil
    }
    sort.Strings(toDelete)
    logInfo("Cleanup(merged): deleting %d file(s) older than %d days (end < %s)", len(toDelete), cfg.MergedDays, cutoff.Format(time.RFC3339))
    for _, p := range toDelete {
        if cfg.DryRun { logInfo("[dry-run] Delete merged %s", p); continue }
        logDebug("Deleting merged %s", p)
        if err := os.Remove(p); err != nil { logWarn("Failed to delete merged %s: %v", p, err) }
    }
    return nil
}

func validateExtConsistency(segs []Segment) error {
	if len(segs) == 0 {
		return nil
	}
	first := segs[0].Ext
	for _, s := range segs[1:] {
		if s.Ext != first {
			return errors.New("segments have differing file extensions; concat demuxer may fail")
		}
	}
	return nil
}
