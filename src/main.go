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
	Overwrite  bool
	DryRun     bool
	Verbose    bool
	GenPTS     bool
	DeleteSegs bool
}

func main() {
	cfg := parseFlags()

	if cfg.Verbose {
		log.Printf("dir=%s outDir=%s outExt=%s merge=%v cleanup=%v days=%d overwrite=%v dryRun=%v genpts=%v", cfg.Dir, cfg.OutDir, cfg.OutExt, cfg.DoMerge, cfg.DoCleanup, cfg.Days, cfg.Overwrite, cfg.DryRun, cfg.GenPTS)
	}

	if cfg.DoMerge {
		if err := ensureFFmpeg(); err != nil {
			log.Fatalf("ffmpeg not found: %v", err)
		}
		if err := mergeByDay(cfg); err != nil {
			log.Fatalf("merge failed: %v", err)
		}
	}

	if cfg.DoCleanup {
		if err := cleanupOld(cfg); err != nil {
			log.Fatalf("cleanup failed: %v", err)
		}
	}
}

const (
	envDir        = "XIAOMI_VIDEO_DIR"
	envOutDir     = "XIAOMI_VIDEO_OUT_DIR"
	envOutExt     = "XIAOMI_VIDEO_OUT_EXT"
	envMerge      = "XIAOMI_VIDEO_MERGE"
	envCleanup    = "XIAOMI_VIDEO_CLEANUP"
	envDays       = "XIAOMI_VIDEO_DAYS"
	envOverwrite  = "XIAOMI_VIDEO_OVERWRITE"
	envDryRun     = "XIAOMI_VIDEO_DRY_RUN"
	envVerbose    = "XIAOMI_VIDEO_VERBOSE"
	envGenPTS     = "XIAOMI_VIDEO_GENPTS"
	envDeleteSegs = "XIAOMI_VIDEO_DELETE_SEGMENTS"
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
	defDays := envInt(envDays, 30)
	defOverwrite := envBool(envOverwrite, false)
	defDryRun := envBool(envDryRun, false)
	defVerbose := envBool(envVerbose, false)
	defGenPTS := envBool(envGenPTS, false)
	defDelete := envBool(envDeleteSegs, true)

	var cfg Config
	flag.StringVar(&cfg.Dir, "dir", defDir, "Input directory to scan")
	flag.StringVar(&cfg.OutDir, "out-dir", defOutDir, "Output directory for merged files (default: same as dir)")
	flag.StringVar(&cfg.OutExt, "out-ext", defOutExt, "Extension for merged output (e.g., .mp4)")
	flag.BoolVar(&cfg.DoMerge, "merge", defMerge, "Run daily merge")
	flag.BoolVar(&cfg.DoCleanup, "cleanup", defCleanup, "Delete videos older than --days")
	flag.IntVar(&cfg.Days, "days", defDays, "Delete files older than N days (by end timestamp)")
	flag.BoolVar(&cfg.Overwrite, "overwrite", defOverwrite, "Overwrite existing merged outputs")
	flag.BoolVar(&cfg.DryRun, "dry-run", defDryRun, "Show actions without making changes")
	flag.BoolVar(&cfg.Verbose, "v", defVerbose, "Verbose logging")
	flag.BoolVar(&cfg.GenPTS, "genpts", defGenPTS, "Use -fflags +genpts with ffmpeg concat (may help PTS issues)")
	flag.BoolVar(&cfg.DeleteSegs, "delete-segments", defDelete, "Delete original segments after successful merge")
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

func collectSegments(root string, verbose bool) ([]Segment, error) {
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
	if verbose {
		log.Printf("found %d segments", len(segments))
	}
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
	segs, err := collectSegments(cfg.Dir, cfg.Verbose)
	if err != nil {
		return err
	}
	if len(segs) == 0 {
		if cfg.Verbose {
			log.Printf("no segments detected; nothing to merge")
		}
		return nil
	}

	segsPre, tempCleanup, err := splitCrossDaySegments(segs, cfg)
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
	for _, day := range days {
		g := groups[day]
		if len(g.Segments) == 0 {
			continue
		}
		first := g.Segments[0]
		last := g.Segments[len(g.Segments)-1]

		if first.StartTime.Format("20060102") != day || last.EndTime.Format("20060102") != day {
			if cfg.Verbose {
				log.Printf("skip merge for %s due to cross-day parts present after preprocessing", day)
			}
			mergeErr = fmt.Errorf("group %s not single-day after preprocessing", day)
			continue
		}
		outName := fmt.Sprintf("%s_%s%s", first.StartTime.Format(tsLayout), last.EndTime.Format(tsLayout), cfg.OutExt)
		outPath := filepath.Join(cfg.OutDir, outName)

		if !cfg.Overwrite {
			if _, err := os.Stat(outPath); err == nil {
				if cfg.Verbose {
					log.Printf("skip day %s: output exists %s", day, outPath)
				}
				continue
			}
		}

		if cfg.DryRun {
			log.Printf("[dry-run] would merge %d segments -> %s", len(g.Segments), outPath)
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

		if cfg.Verbose {
			log.Printf("merging %d segments for %s -> %s", len(g.Segments), day, outPath)
		}
		if err := runFFmpegConcat(listFile, outPath, cfg.GenPTS, cfg.Verbose); err != nil {
			log.Printf("merge failed for %s: %v", day, err)
			mergeErr = err
			continue
		}
	}
	if mergeErr != nil {
		return mergeErr
	}

	if cfg.DeleteSegs {
		if err := deleteFiles(segs, cfg); err != nil {
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
			if cfg.Verbose {
				log.Printf("skip invalid duration segment: %s", s.Path)
			}
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
				continue
			}

			args := []string{"-y"}
			if p.off > 0 {
				args = append(args, "-ss", fmt.Sprintf("%d", p.off))
			}
			args = append(args, "-i", s.Path, "-t", fmt.Sprintf("%d", p.dur))
			if cfg.GenPTS {
				args = append(args, "-fflags", "+genpts")
			}
			args = append(args, "-c", "copy", tmp)
			if cfg.Verbose {
				log.Printf("ffmpeg trim %s -> %s (off=%ds dur=%ds)", s.Path, tmp, p.off, p.dur)
			}
			if err := runFFmpeg(args, cfg.Verbose); err != nil {
				cleanup()
				return nil, func() {}, fmt.Errorf("ffmpeg split failed: %w", err)
			}
			out = append(out, Segment{Path: tmp, StartTime: p.ps, EndTime: p.pe, Ext: ext})
		}
	}
	return out, cleanup, nil
}

func runFFmpeg(args []string, verbose bool) error {
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
	if verbose {
		go io.Copy(os.Stdout, stdout)
		go io.Copy(os.Stderr, stderr)
	} else {
		go io.Copy(io.Discard, stdout)
		go io.Copy(io.Discard, stderr)
	}
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
	for _, p := range files {
		if cfg.DryRun {
			log.Printf("[dry-run] would delete original %s", p)
			continue
		}
		if cfg.Verbose {
			log.Printf("deleting original %s", p)
		}
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

func runFFmpegConcat(listFile, outPath string, genpts, verbose bool) error {
	args := []string{"-y", "-f", "concat", "-safe", "0", "-i", listFile}
	if genpts {
		args = append(args, "-fflags", "+genpts")
	}
	args = append(args, "-c", "copy", outPath)

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
	if verbose {
		go io.Copy(os.Stdout, stdout)
		go io.Copy(os.Stderr, stderr)
	} else {
		go io.Copy(io.Discard, stdout)
		go io.Copy(io.Discard, stderr)
	}
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
		if cfg.Verbose {
			log.Printf("no files older than %d days", cfg.Days)
		}
		return nil
	}
	sort.Strings(toDelete)
	for _, p := range toDelete {
		if cfg.DryRun {
			log.Printf("[dry-run] would delete %s", p)
			continue
		}
		if cfg.Verbose {
			log.Printf("deleting %s", p)
		}
		if err := os.Remove(p); err != nil {
			log.Printf("failed to delete %s: %v", p, err)
		}
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
