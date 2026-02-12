package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

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
