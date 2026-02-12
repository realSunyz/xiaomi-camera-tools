package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Segment struct {
	Path      string
	SourceKey string
	StartTime time.Time
	EndTime   time.Time
	Ext       string
}

const tsLayout = "20060102150405"

func isDigits14(s string) bool {
	if len(s) != 14 {
		return false
	}
	for i := 0; i < 14; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func parseRawSegment(name string) (start, end time.Time, ext string, ok bool) {
	if !strings.HasPrefix(name, "00_") {
		return time.Time{}, time.Time{}, "", false
	}
	rest := name[3:]
	if len(rest) < 29 {
		return time.Time{}, time.Time{}, "", false
	}
	startStr := rest[:14]
	if rest[14] != '_' {
		return time.Time{}, time.Time{}, "", false
	}
	tail := rest[15:]
	if len(tail) < 14 {
		return time.Time{}, time.Time{}, "", false
	}
	endStr := tail[:14]
	ext = tail[14:]
	if ext != "" && ext[0] != '.' {
		return time.Time{}, time.Time{}, "", false
	}
	if !isDigits14(startStr) || !isDigits14(endStr) {
		return time.Time{}, time.Time{}, "", false
	}
	st, err1 := time.ParseInLocation(tsLayout, startStr, time.Local)
	et, err2 := time.ParseInLocation(tsLayout, endStr, time.Local)
	if err1 != nil || err2 != nil {
		return time.Time{}, time.Time{}, "", false
	}
	return st, et, ext, true
}

// Expected merged output name format:
// YYYYMMDDHHMMSS_YYYYMMDDHHMMSS[.ext]
func parseMergedSegment(name string) (start, end time.Time, ext string, ok bool) {
	if len(name) < 29 {
		return time.Time{}, time.Time{}, "", false
	}
	startStr := name[:14]
	if name[14] != '_' {
		return time.Time{}, time.Time{}, "", false
	}
	tail := name[15:]
	if len(tail) < 14 {
		return time.Time{}, time.Time{}, "", false
	}
	endStr := tail[:14]
	ext = tail[14:]
	if ext != "" && ext[0] != '.' {
		return time.Time{}, time.Time{}, "", false
	}
	if !isDigits14(startStr) || !isDigits14(endStr) {
		return time.Time{}, time.Time{}, "", false
	}
	st, err1 := time.ParseInLocation(tsLayout, startStr, time.Local)
	et, err2 := time.ParseInLocation(tsLayout, endStr, time.Local)
	if err1 != nil || err2 != nil {
		return time.Time{}, time.Time{}, "", false
	}
	return st, et, ext, true
}

type DayGroup struct {
	Day       string
	SourceKey string
	Segments  []Segment
}

type Config struct {
	Dir        string
	OutDir     string
	Days       *int
	MergedDays *int
	Cron       string
}

const (
	mergedOutExt          = ".mp4"
	mp4VideoTrackTimebase = 90000
	logTimeLayout         = time.RFC3339
)

func logLine(level, component, format string, args ...any) {
	msg := strings.TrimSpace(fmt.Sprintf(format, args...))
	msg = strings.ReplaceAll(msg, "\n", "\\n")
	log.Printf("ts=%s level=%s component=%s msg=%s", time.Now().Format(logTimeLayout), level, component, msg)
}

func logInfo(format string, args ...any)  { logLine("INFO", "app", format, args...) }
func logWarn(format string, args ...any)  { logLine("WARN", "app", format, args...) }
func logError(format string, args ...any) { logLine("ERROR", "app", format, args...) }
func logFatal(format string, args ...any) { logLine("FATAL", "app", format, args...) }

func absClean(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func ensureFFmpeg() error {
	_, err := exec.LookPath("ffmpeg")
	return err
}

func runFFmpeg(args []string) error {
	ffmpegArgs := make([]string, 0, len(args)+4)
	ffmpegArgs = append(ffmpegArgs, "-hide_banner", "-nostats", "-loglevel", "error")
	ffmpegArgs = append(ffmpegArgs, args...)
	cmd := exec.Command("ffmpeg", ffmpegArgs...)
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

	var wg sync.WaitGroup
	wg.Add(2)
	go streamProcessOutput("ffmpeg", "stdout", stdout, &wg)
	go streamProcessOutput("ffmpeg", "stderr", stderr, &wg)

	waitErr := cmd.Wait()
	wg.Wait()
	return waitErr
}

func streamProcessOutput(component, stream string, r io.Reader, wg *sync.WaitGroup) {
	defer wg.Done()
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		logLine("INFO", component, "%s", line)
	}
	if err := scanner.Err(); err != nil {
		logWarn("%s %s stream read error: %v", component, stream, err)
	}
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

func runFFmpegConcat(listFile, outPath string) error {
	args := []string{"-y", "-f", "concat", "-safe", "0", "-i", listFile}
	args = append(args, "-fflags", "+genpts")
	args = append(args, "-c", "copy")
	args = append(args, "-avoid_negative_ts", "make_zero")
	if strings.EqualFold(filepath.Ext(outPath), ".mp4") {
		args = append(args, "-movflags", "+faststart")
		args = append(args, "-video_track_timescale", fmt.Sprintf("%d", mp4VideoTrackTimebase))
	}
	args = append(args, outPath)
	return runFFmpeg(args)
}

func collectSegments(root, outDir string) ([]Segment, error) {
	rootAbs := absClean(root)
	outAbs := absClean(outDir)
	segments := make([]Segment, 0, 1024)
	err := filepath.WalkDir(rootAbs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if outAbs != rootAbs && path == outAbs {
				return filepath.SkipDir
			}
			return nil
		}
		base := filepath.Base(path)
		s, e, ext, ok := parseRawSegment(base)
		if !ok {
			return nil
		}
		relDir, err := filepath.Rel(rootAbs, filepath.Dir(path))
		if err != nil {
			relDir = ""
		}
		if relDir == "." {
			relDir = ""
		}
		segments = append(segments, Segment{
			Path:      path,
			SourceKey: relDir,
			StartTime: s,
			EndTime:   e,
			Ext:       ext,
		})
		return nil
	})
	logInfo("Found %d raw segment(s) in %s", len(segments), rootAbs)
	return segments, err
}

func groupBySourceAndDay(segs []Segment) map[string]*DayGroup {
	groups := make(map[string]*DayGroup)
	for _, s := range segs {
		day := s.StartTime.Format("20060102")
		groupKey := s.SourceKey + "|" + day
		g, ok := groups[groupKey]
		if !ok {
			g = &DayGroup{Day: day, SourceKey: s.SourceKey}
			groups[groupKey] = g
		}
		g.Segments = append(g.Segments, s)
	}
	for _, g := range groups {
		sort.Slice(g.Segments, func(i, j int) bool { return g.Segments[i].StartTime.Before(g.Segments[j].StartTime) })
	}
	return groups
}

func mergeByDay(cfg Config) error {
	segs, err := collectSegments(cfg.Dir, cfg.OutDir)
	if err != nil {
		return err
	}
	if len(segs) == 0 {
		logInfo("No segments detected; nothing to merge")
		return nil
	}

	// Skip-today is always enabled to avoid touching files that may still be recording.
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	segsEligible := make([]Segment, 0, len(segs))
	for _, s := range segs {
		if s.EndTime.Before(todayStart) {
			segsEligible = append(segsEligible, s)
		}
	}
	logInfo("Skip-today(fixed): eligible %d/%d (end < %s)", len(segsEligible), len(segs), todayStart.Format(time.RFC3339))
	if len(segsEligible) == 0 {
		logInfo("No segments to process after skip-today filter")
		return nil
	}

	segsPre, tempCleanup, err := splitCrossDaySegments(segsEligible)
	if err != nil {
		return err
	}
	defer tempCleanup()

	groups := groupBySourceAndDay(segsPre)
	groupKeys := make([]string, 0, len(groups))
	for groupKey := range groups {
		groupKeys = append(groupKeys, groupKey)
	}
	sort.Strings(groupKeys)

	var mergeErr error
	successDays := 0
	for _, groupKey := range groupKeys {
		g := groups[groupKey]
		day := g.Day
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
		if err := validateExtConsistency(g.Segments); err != nil {
			logWarn("Skip merge for %s/%s: %v", g.SourceKey, day, err)
			mergeErr = err
			continue
		}

		outName := fmt.Sprintf("%s_%s%s", first.StartTime.Format(tsLayout), last.EndTime.Format(tsLayout), mergedOutExt)
		outDir := cfg.OutDir
		if g.SourceKey != "" {
			outDir = filepath.Join(cfg.OutDir, g.SourceKey)
		}
		outPath := filepath.Join(outDir, outName)

		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return fmt.Errorf("create out dir: %w", err)
		}

		listFile, cleanup, err := writeConcatList(g.Segments)
		if err != nil {
			return fmt.Errorf("make concat list: %w", err)
		}
		defer cleanup()

		logInfo("Merging source=%s day=%s: %d segments -> %s", g.SourceKey, day, len(g.Segments), outPath)
		if err := runFFmpegConcat(listFile, outPath); err != nil {
			logError("Merge failed for source=%s day=%s: %v", g.SourceKey, day, err)
			mergeErr = err
			continue
		}
		logInfo("Done source=%s day=%s -> %s", g.SourceKey, day, outPath)
		successDays++
	}
	if mergeErr != nil {
		logWarn("Merging finished with errors; successful days: %d", successDays)
		return mergeErr
	}
	logInfo("Merging finished; successful days: %d", successDays)

	return nil
}

func splitCrossDaySegments(segs []Segment) ([]Segment, func(), error) {
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
				ext = mergedOutExt
			}
			tf, err := os.CreateTemp("", fmt.Sprintf("split_%d_*%s", i, ext))
			if err != nil {
				cleanup()
				return nil, func() {}, err
			}
			tmp := tf.Name()
			_ = tf.Close()
			temps = append(temps, tmp)

			args := []string{"-y"}
			if p.off > 0 {
				args = append(args, "-ss", fmt.Sprintf("%d", p.off))
			}
			args = append(args, "-i", s.Path, "-t", fmt.Sprintf("%d", p.dur))
			args = append(args, "-fflags", "+genpts")
			args = append(args, "-c", "copy")
			args = append(args, "-avoid_negative_ts", "make_zero")
			if strings.EqualFold(ext, ".mp4") {
				args = append(args, "-movflags", "+faststart")
				args = append(args, "-video_track_timescale", fmt.Sprintf("%d", mp4VideoTrackTimebase))
			}
			args = append(args, tmp)
			if err := runFFmpeg(args); err != nil {
				cleanup()
				return nil, func() {}, fmt.Errorf("ffmpeg split failed: %w", err)
			}
			out = append(out, Segment{
				Path:      tmp,
				SourceKey: s.SourceKey,
				StartTime: p.ps,
				EndTime:   p.pe,
				Ext:       ext,
			})
		}
	}
	return out, cleanup, nil
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

func cleanupOld(cfg Config) error {
	if cfg.Days == nil {
		logInfo("Cleanup(raw): retention not set, keep forever")
		return nil
	}

	dirAbs := absClean(cfg.Dir)
	outAbs := absClean(cfg.OutDir)
	now := time.Now()
	days := *cfg.Days
	var cutoff time.Time
	if days == 0 {
		// Immediate mode: remove finished-day raw segments after merge,
		// while still keeping today's potentially active recordings.
		cutoff = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	} else {
		cutoff = now.AddDate(0, 0, -days)
	}
	var toDelete []string
	err := filepath.WalkDir(dirAbs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if outAbs != dirAbs && path == outAbs {
				return filepath.SkipDir
			}
			return nil
		}
		base := filepath.Base(path)
		s, e, _, ok := parseRawSegment(base)
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
		logInfo("Cleanup(raw): no files older than %d days", days)
		return nil
	}
	sort.Strings(toDelete)
	logInfo("Cleanup(raw): deleting %d file(s) older than %d days (end < %s)", len(toDelete), days, cutoff.Format(time.RFC3339))
	for _, p := range toDelete {
		if err := os.Remove(p); err != nil {
			logWarn("Failed to delete %s: %v", p, err)
		}
	}
	return nil
}

func cleanupMerged(cfg Config) error {
	if cfg.MergedDays == nil {
		logInfo("Cleanup(merged): retention not set, keep forever")
		return nil
	}

	if _, err := os.Stat(cfg.OutDir); err != nil {
		if os.IsNotExist(err) {
			logInfo("Cleanup(merged): output directory does not exist yet, skip: %s", cfg.OutDir)
			return nil
		}
		return err
	}

	days := *cfg.MergedDays
	cutoff := time.Now().AddDate(0, 0, -days)
	var toDelete []string
	err := filepath.WalkDir(cfg.OutDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasPrefix(base, "00_") {
			return nil
		}
		s, e, ext, ok := parseMergedSegment(base)
		if !ok {
			return nil
		}
		if !strings.EqualFold(ext, ".mp4") {
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
		logInfo("Cleanup(merged): no files older than %d days in %s", days, cfg.OutDir)
		return nil
	}
	sort.Strings(toDelete)
	logInfo("Cleanup(merged): deleting %d file(s) older than %d days (end < %s)", len(toDelete), days, cutoff.Format(time.RFC3339))
	for _, p := range toDelete {
		if err := os.Remove(p); err != nil {
			logWarn("Failed to delete merged %s: %v", p, err)
		}
	}
	return nil
}
