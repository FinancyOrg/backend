package domain

import (
	"strings"
	"time"

	_ "time/tzdata"
)

// DefaultLedgerTimezone is the IANA zone used to turn stored UTC instants
// into civil days, months, and "today". Storage stays UTC; this is display
// and books-calendar only.
const DefaultLedgerTimezone = "Europe/Berlin"

// UTCLayout is the canonical stored timestamp: UTC, nanosecond precision, Z suffix.
const UTCLayout = "2006-01-02T15:04:05.000000000Z"

const UTCDateLayout = "2006-01-02"

func FormatUTC(t time.Time) string {
	return t.UTC().Format(UTCLayout)
}

func NowUTC() string {
	return FormatUTC(time.Now())
}

func ParseUTC(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, InvalidJournalEntry("datetime is required")
	}
	if IsUTCDateOnly(s) {
		t, err := time.Parse(UTCDateLayout, s)
		if err != nil {
			return time.Time{}, InvalidJournalEntry("invalid datetime: " + s)
		}
		return t.UTC(), nil
	}
	if t, err := time.Parse(UTCLayout, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, InvalidJournalEntry("invalid datetime: " + s)
}

func IsUTCDateOnly(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 10 {
		return false
	}
	_, err := time.Parse(UTCDateLayout, s)
	return err == nil
}

func UTCDate(s string) string {
	t, err := ParseUTC(s)
	if err != nil {
		if len(s) >= 10 {
			return s[:10]
		}
		return s
	}
	return t.UTC().Format(UTCDateLayout)
}

func CanonicalUTC(s string) (string, error) {
	t, err := ParseUTC(s)
	if err != nil {
		return "", err
	}
	return FormatUTC(t), nil
}

// IsFutureUTCDate is true when t's UTC calendar date is after today UTC.
func IsFutureUTCDate(t time.Time) bool {
	t = t.UTC()
	now := time.Now().UTC()
	tDay := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	nDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return tDay.After(nDay)
}

func RejectFutureUTC(canonical string) error {
	return RejectFutureIn(canonical, time.UTC, time.Now())
}

func RejectFutureIn(canonical string, loc *time.Location, now time.Time) error {
	t, err := ParseUTC(canonical)
	if err != nil {
		return err
	}
	if IsFutureCivilDate(t, now, loc) {
		return InvalidJournalEntry("datetime cannot be in the future")
	}
	return nil
}

func IsFutureCivilDate(t, now time.Time, loc *time.Location) bool {
	loc = locationOrUTC(loc)
	t = t.In(loc)
	now = now.In(loc)
	tDay := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	nDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	return tDay.After(nDay)
}

func RangeStartUTC(s string) (string, error) {
	return RangeStartIn(s, time.UTC)
}

func RangeEndUTC(s string) (string, error) {
	return RangeEndIn(s, time.UTC)
}

func RangeStartIn(s string, loc *time.Location) (string, error) {
	loc = locationOrUTC(loc)
	s = strings.TrimSpace(s)
	if IsUTCDateOnly(s) {
		t, err := time.ParseInLocation(UTCDateLayout, s, loc)
		if err != nil {
			return "", InvalidJournalEntry("invalid datetime: " + s)
		}
		return FormatUTC(t), nil
	}
	t, err := ParseUTC(s)
	if err != nil {
		return "", err
	}
	return FormatUTC(t), nil
}

func RangeEndIn(s string, loc *time.Location) (string, error) {
	loc = locationOrUTC(loc)
	s = strings.TrimSpace(s)
	if IsUTCDateOnly(s) {
		t, err := time.ParseInLocation(UTCDateLayout, s, loc)
		if err != nil {
			return "", InvalidJournalEntry("invalid datetime: " + s)
		}
		end := t.AddDate(0, 0, 1).Add(-time.Nanosecond)
		return FormatUTC(end), nil
	}
	t, err := ParseUTC(s)
	if err != nil {
		return "", err
	}
	return FormatUTC(t), nil
}

func LoadLedgerLocation(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" {
		name = DefaultLedgerTimezone
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		loc, err = time.LoadLocation(DefaultLedgerTimezone)
	}
	if err != nil {
		return time.UTC
	}
	return loc
}

func CivilDateIn(s string, loc *time.Location) (string, error) {
	t, err := ParseUTC(s)
	if err != nil {
		return "", err
	}
	return t.In(locationOrUTC(loc)).Format(UTCDateLayout), nil
}

func locationOrUTC(loc *time.Location) *time.Location {
	if loc == nil {
		return time.UTC
	}
	return loc
}

// ClockOnUTCDate keeps `at`'s time of day and places it on the given UTC calendar date.
func ClockOnUTCDate(date string, at time.Time) string {
	d, err := ParseUTC(date)
	if err != nil {
		return FormatUTC(at.UTC())
	}
	at = at.UTC()
	return FormatUTC(time.Date(d.Year(), d.Month(), d.Day(), at.Hour(), at.Minute(), at.Second(), at.Nanosecond(), time.UTC))
}

func IsOnUTCDate(timestamp, date string) bool {
	return UTCDate(timestamp) == UTCDate(date)
}
