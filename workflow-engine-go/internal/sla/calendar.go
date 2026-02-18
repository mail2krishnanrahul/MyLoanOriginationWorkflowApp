package sla

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// BusinessCalendar represents working hours and holidays.
type BusinessCalendar struct {
	ID           string      `db:"id"`
	Name         string      `db:"name"`
	Timezone     string      `db:"timezone"`
	StartTime    string      `db:"start_time"`    // "09:00"
	EndTime      string      `db:"end_time"`      // "17:00"
	WorkingDays  int         `db:"working_days"`   // bitfield: Mon=1, Tue=2... Sun=64
	HolidayDates []time.Time `db:"-"`
}

type holidayRow struct {
	Date        time.Time `db:"holiday_date"`
	IsRecurring bool      `db:"is_recurring"`
}

type loadedCalendar struct {
	BusinessCalendar
	holidayDateSet  map[string]struct{}
	holidayMonthDay map[string]struct{}
}

// AddBusinessHours adds a duration to a start time,
// skipping non-working hours, weekends, and holidays.
func AddBusinessHours(
	ctx context.Context,
	db *sqlx.DB,
	start time.Time,
	duration time.Duration,
	calendarID string,
) (time.Time, error) {
	if db == nil {
		return time.Time{}, fmt.Errorf("AddBusinessHours: db is nil")
	}
	if duration < 0 {
		return time.Time{}, fmt.Errorf("AddBusinessHours: duration must be non-negative")
	}
	if duration == 0 {
		return start.UTC(), nil
	}

	cal, loc, err := loadBusinessCalendar(ctx, db, calendarID)
	if err != nil {
		return time.Time{}, fmt.Errorf("AddBusinessHours: %w", err)
	}

	current := start.In(loc)
	current, err = alignToWorkingStart(current, cal)
	if err != nil {
		return time.Time{}, fmt.Errorf("AddBusinessHours: %w", err)
	}

	remaining := duration
	for guard := 0; guard < 50000; guard++ {
		if !isBusinessDay(current, cal) {
			current = nextDayAtCalendarStart(current, cal)
			continue
		}

		dayStart, dayEnd, err := workingWindow(current, cal)
		if err != nil {
			return time.Time{}, fmt.Errorf("AddBusinessHours: %w", err)
		}

		if current.Before(dayStart) {
			current = dayStart
		}
		if !current.Before(dayEnd) {
			current = nextDayAtCalendarStart(current, cal)
			continue
		}

		available := dayEnd.Sub(current)
		if remaining <= available {
			return current.Add(remaining).UTC(), nil
		}

		remaining -= available
		current = nextDayAtCalendarStart(current, cal)
	}

	return time.Time{}, fmt.Errorf("AddBusinessHours: exceeded iteration safety limit")
}

// BusinessHoursElapsed calculates how many business hours
// have passed between two timestamps.
func BusinessHoursElapsed(
	ctx context.Context,
	db *sqlx.DB,
	start time.Time,
	end time.Time,
	calendarID string,
) (time.Duration, error) {
	if db == nil {
		return 0, fmt.Errorf("BusinessHoursElapsed: db is nil")
	}
	if end.Before(start) {
		return 0, fmt.Errorf("BusinessHoursElapsed: end is before start")
	}
	if end.Equal(start) {
		return 0, nil
	}

	cal, loc, err := loadBusinessCalendar(ctx, db, calendarID)
	if err != nil {
		return 0, fmt.Errorf("BusinessHoursElapsed: %w", err)
	}

	current := start.In(loc)
	target := end.In(loc)
	elapsed := time.Duration(0)

	for guard := 0; guard < 50000 && current.Before(target); guard++ {
		if !isBusinessDay(current, cal) {
			current = nextDayAtCalendarStart(current, cal)
			continue
		}

		dayStart, dayEnd, err := workingWindow(current, cal)
		if err != nil {
			return 0, fmt.Errorf("BusinessHoursElapsed: %w", err)
		}

		if current.Before(dayStart) {
			current = dayStart
		}
		if !current.Before(dayEnd) {
			current = nextDayAtCalendarStart(current, cal)
			continue
		}

		segmentEnd := dayEnd
		if target.Before(segmentEnd) {
			segmentEnd = target
		}

		if segmentEnd.After(current) {
			elapsed += segmentEnd.Sub(current)
		}

		current = segmentEnd
		if !current.Before(target) {
			break
		}
		if !current.Before(dayEnd) {
			current = nextDayAtCalendarStart(current, cal)
		}
	}

	if elapsed < 0 {
		return 0, fmt.Errorf("BusinessHoursElapsed: computed negative elapsed duration")
	}

	return elapsed, nil
}

func loadBusinessCalendar(ctx context.Context, db *sqlx.DB, calendarID string) (loadedCalendar, *time.Location, error) {
	if calendarID == "" {
		return loadedCalendar{}, nil, fmt.Errorf("calendar id is required")
	}

	var cal BusinessCalendar
	err := db.GetContext(ctx, &cal, `
		SELECT
			id,
			name,
			timezone,
			to_char(start_time, 'HH24:MI') AS start_time,
			to_char(end_time, 'HH24:MI') AS end_time,
			working_days_bitfield AS working_days
		FROM business_calendars
		WHERE id::text = $1 OR name = $1
		ORDER BY CASE WHEN id::text = $1 THEN 0 ELSE 1 END
		LIMIT 1
	`, calendarID)
	if err != nil {
		return loadedCalendar{}, nil, fmt.Errorf("loadBusinessCalendar: calendar %s not found: %w", calendarID, err)
	}

	loc, err := time.LoadLocation(cal.Timezone)
	if err != nil {
		return loadedCalendar{}, nil, fmt.Errorf("loadBusinessCalendar: invalid timezone %s: %w", cal.Timezone, err)
	}

	var holidayRows []holidayRow
	err = db.SelectContext(ctx, &holidayRows, `
		SELECT holiday_date, is_recurring
		FROM holiday_calendars
		WHERE calendar_id = $1::uuid
	`, cal.ID)
	if err != nil {
		return loadedCalendar{}, nil, fmt.Errorf("loadBusinessCalendar: load holidays: %w", err)
	}

	cal.HolidayDates = make([]time.Time, 0, len(holidayRows))
	loaded := loadedCalendar{
		BusinessCalendar: cal,
		holidayDateSet:   make(map[string]struct{}, len(holidayRows)),
		holidayMonthDay:  make(map[string]struct{}, len(holidayRows)),
	}

	for _, h := range holidayRows {
		localDate := time.Date(h.Date.Year(), h.Date.Month(), h.Date.Day(), 0, 0, 0, 0, loc)
		cal.HolidayDates = append(cal.HolidayDates, localDate)
		loaded.BusinessCalendar.HolidayDates = append(loaded.BusinessCalendar.HolidayDates, localDate)
		if h.IsRecurring {
			loaded.holidayMonthDay[monthDayKey(localDate)] = struct{}{}
			continue
		}
		loaded.holidayDateSet[localDate.Format("2006-01-02")] = struct{}{}
	}

	return loaded, loc, nil
}

func alignToWorkingStart(t time.Time, cal loadedCalendar) (time.Time, error) {
	current := t
	for guard := 0; guard < 20000; guard++ {
		if !isBusinessDay(current, cal) {
			current = nextDayAtCalendarStart(current, cal)
			continue
		}

		dayStart, dayEnd, err := workingWindow(current, cal)
		if err != nil {
			return time.Time{}, err
		}
		if current.Before(dayStart) {
			return dayStart, nil
		}
		if !current.Before(dayEnd) {
			current = nextDayAtCalendarStart(current, cal)
			continue
		}
		return current, nil
	}
	return time.Time{}, fmt.Errorf("alignToWorkingStart: exceeded iteration safety limit")
}

func workingWindow(t time.Time, cal loadedCalendar) (time.Time, time.Time, error) {
	startHour, startMin, err := parseClockHM(cal.StartTime)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("workingWindow: parse start_time: %w", err)
	}
	endHour, endMin, err := parseClockHM(cal.EndTime)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("workingWindow: parse end_time: %w", err)
	}

	dayStart := time.Date(t.Year(), t.Month(), t.Day(), startHour, startMin, 0, 0, t.Location())
	dayEnd := time.Date(t.Year(), t.Month(), t.Day(), endHour, endMin, 0, 0, t.Location())
	if !dayEnd.After(dayStart) {
		return time.Time{}, time.Time{}, fmt.Errorf("workingWindow: end_time must be after start_time")
	}
	return dayStart, dayEnd, nil
}

func parseClockHM(s string) (int, int, error) {
	parsed, err := time.Parse("15:04", s)
	if err != nil {
		return 0, 0, err
	}
	return parsed.Hour(), parsed.Minute(), nil
}

func nextDayAtCalendarStart(t time.Time, cal loadedCalendar) time.Time {
	startHour, startMin, err := parseClockHM(cal.StartTime)
	if err != nil {
		return t.Add(24 * time.Hour)
	}
	nextDay := t.AddDate(0, 0, 1)
	return time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), startHour, startMin, 0, 0, nextDay.Location())
}

func isBusinessDay(t time.Time, cal loadedCalendar) bool {
	if !isWorkingWeekday(t.Weekday(), cal.WorkingDays) {
		return false
	}

	dateKey := t.Format("2006-01-02")
	if _, ok := cal.holidayDateSet[dateKey]; ok {
		return false
	}

	if _, ok := cal.holidayMonthDay[monthDayKey(t)]; ok {
		return false
	}

	return true
}

func isWorkingWeekday(weekday time.Weekday, bitfield int) bool {
	return bitfield&weekdayMask(weekday) != 0
}

func weekdayMask(weekday time.Weekday) int {
	switch weekday {
	case time.Monday:
		return 1
	case time.Tuesday:
		return 2
	case time.Wednesday:
		return 4
	case time.Thursday:
		return 8
	case time.Friday:
		return 16
	case time.Saturday:
		return 32
	case time.Sunday:
		return 64
	default:
		return 0
	}
}

func monthDayKey(t time.Time) string {
	return fmt.Sprintf("%02d-%02d", int(t.Month()), t.Day())
}
