// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package taskscheduler

import (
	"strconv"
	"strings"
	"time"
)

type DurationScheduleParser struct{}

func (DurationScheduleParser) NextRunAfter(scheduleSpec string, after time.Time) (time.Time, bool) {
	spec := strings.TrimSpace(strings.TrimPrefix(scheduleSpec, "every "))
	if spec == "" {
		return time.Time{}, false
	}

	duration, err := time.ParseDuration(spec)
	if err != nil || duration <= 0 {
		return nextWeeklyRun(spec, after)
	}
	return after.Add(duration), true
}

func nextWeeklyRun(spec string, after time.Time) (time.Time, bool) {
	fields := strings.Fields(spec)
	if len(fields) != 2 {
		return time.Time{}, false
	}
	weekday, ok := parseWeekday(fields[0])
	if !ok {
		return time.Time{}, false
	}
	hour, minute, ok := parseHourMinute(fields[1])
	if !ok {
		return time.Time{}, false
	}
	next := time.Date(after.Year(), after.Month(), after.Day(), hour, minute, 0, 0, after.Location())
	days := (int(weekday) - int(after.Weekday()) + 7) % 7
	if days == 0 && !next.After(after) {
		days = 7
	}
	return next.AddDate(0, 0, days), true
}

func parseHourMinute(value string) (int, int, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

func parseWeekday(value string) (time.Weekday, bool) {
	switch strings.ToLower(value) {
	case "monday", "mon", "montag", "mo":
		return time.Monday, true
	case "tuesday", "tue", "dienstag", "di":
		return time.Tuesday, true
	case "wednesday", "wed", "mittwoch", "mi":
		return time.Wednesday, true
	case "thursday", "thu", "donnerstag", "do":
		return time.Thursday, true
	case "friday", "fri", "freitag", "fr":
		return time.Friday, true
	case "saturday", "sat", "samstag", "sa":
		return time.Saturday, true
	case "sunday", "sun", "sonntag", "so":
		return time.Sunday, true
	default:
		return time.Sunday, false
	}
}
