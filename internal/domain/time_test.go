package domain

import (
	"testing"
	"time"
)

func TestParseUTC_ConvertsOffsetToZ(t *testing.T) {
	got, err := ParseUTC("2026-02-03T09:30:00+01:00")
	if err != nil {
		t.Fatal(err)
	}
	if FormatUTC(got) != "2026-02-03T08:30:00.000000000Z" {
		t.Fatal(FormatUTC(got))
	}
}

func TestParseUTC_DateOnlyIsMidnight(t *testing.T) {
	got, err := ParseUTC("2026-01-26")
	if err != nil {
		t.Fatal(err)
	}
	if FormatUTC(got) != "2026-01-26T00:00:00.000000000Z" {
		t.Fatal(FormatUTC(got))
	}
}

func TestRangeEndUTC_DateCoversWholeDay(t *testing.T) {
	end, err := RangeEndUTC("2026-01-26")
	if err != nil {
		t.Fatal(err)
	}
	if end != "2026-01-26T23:59:59.999999999Z" {
		t.Fatal(end)
	}
}

func TestRejectFutureUTC_BlocksTomorrow(t *testing.T) {
	future := time.Now().UTC().AddDate(0, 0, 1).Format(UTCDateLayout)
	err := RejectFutureUTC(future)
	if err == nil {
		t.Fatal("expected future datetime to be rejected")
	}
	de, ok := IsDomainError(err)
	if !ok || de.Code != "InvalidJournalEntry" {
		t.Fatalf("got %+v", err)
	}
}

func TestRejectFutureUTC_AllowsTodayAndPast(t *testing.T) {
	today := time.Now().UTC().Format(UTCDateLayout)
	if err := RejectFutureUTC(today); err != nil {
		t.Fatal(err)
	}
	if err := RejectFutureUTC("2020-01-01T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
}

func TestCivilDateIn_BerlinCrossesUTCMonth(t *testing.T) {
	berlin := LoadLedgerLocation(DefaultLedgerTimezone)
	got, err := CivilDateIn("2026-06-30T23:05:00.000000000Z", berlin)
	if err != nil {
		t.Fatal(err)
	}
	if got != "2026-07-01" {
		t.Fatal(got)
	}
}

func TestRangeStartEndIn_BerlinJuly(t *testing.T) {
	berlin := LoadLedgerLocation("Europe/Berlin")
	start, err := RangeStartIn("2026-07-01", berlin)
	if err != nil {
		t.Fatal(err)
	}
	if start != "2026-06-30T22:00:00.000000000Z" {
		t.Fatal(start)
	}
	end, err := RangeEndIn("2026-07-31", berlin)
	if err != nil {
		t.Fatal(err)
	}
	if end != "2026-07-31T21:59:59.999999999Z" {
		t.Fatal(end)
	}
	instant := "2026-06-30T23:05:00.000000000Z"
	if instant < start || instant > end {
		t.Fatalf("july CEST posting %s not in [%s, %s]", instant, start, end)
	}
}

func TestRangeStartEndIn_BerlinJuneExcludesNextMorning(t *testing.T) {
	berlin := LoadLedgerLocation("Europe/Berlin")
	end, err := RangeEndIn("2026-06-30", berlin)
	if err != nil {
		t.Fatal(err)
	}
	if "2026-06-30T23:05:00.000000000Z" <= end {
		t.Fatalf("CEST 1 July 01:05 should be after June end %s", end)
	}
}

func TestRejectFutureIn_UsesCivilDate(t *testing.T) {
	berlin := LoadLedgerLocation("Europe/Berlin")
	now := time.Date(2026, 7, 1, 1, 0, 0, 0, berlin)
	if err := RejectFutureIn("2026-07-01T01:05:00+02:00", berlin, now); err != nil {
		t.Fatal(err)
	}
	if err := RejectFutureIn("2026-07-02T00:00:00+02:00", berlin, now); err == nil {
		t.Fatal("expected next civil day to be rejected")
	}
}

func TestClockOnUTCDate_KeepsTimeOfDay(t *testing.T) {
	at, err := ParseUTC("2026-09-01T14:03:11.123456789Z")
	if err != nil {
		t.Fatal(err)
	}
	got := ClockOnUTCDate("2026-01-16", at)
	if got != "2026-01-16T14:03:11.123456789Z" {
		t.Fatal(got)
	}
	if !IsOnUTCDate(got, "2026-01-16") {
		t.Fatal(got)
	}
	if IsOnUTCDate(got, "2026-01-17") {
		t.Fatal(got)
	}
}
