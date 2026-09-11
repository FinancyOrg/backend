package ledger

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/FinancyOrg/backend/internal/domain"
)

const (
	ecbBase        = "https://data-api.ecb.europa.eu/service/data/EXR"
	ecbPublishHour = 16
	ecbPublishZone = "Europe/Berlin"
)

var (
	ecbHTTPClient = &http.Client{Timeout: 8 * time.Second}
	nowUTC        = func() time.Time { return time.Now().UTC() }
)

// EcbCache persists ECB daily spots in Mongo (ecb:{CCY}:{date}).
type EcbCache interface {
	GetEcbRate(ctx context.Context, currency, date string) (rate domain.Rational, missing, ok bool, err error)
	PutEcbRate(ctx context.Context, currency, date string, rate domain.Rational, missing bool) error
}

func ecbPublishLocation() *time.Location {
	return domain.LoadLedgerLocation(ecbPublishZone)
}

// ecbSpotAvailable is whether ECB may already have published a daily spot for date.
// Future dates are never fetched. Historical dates are always fetched.
// Today's Berlin civil date is fetched only at or after 16:00 Europe/Berlin
// (15:00 UTC in winter, 14:00 UTC in summer).
func ecbSpotAvailable(date string, now time.Time) bool {
	loc := ecbPublishLocation()
	now = now.In(loc)
	today := now.Format(domain.UTCDateLayout)
	if date > today {
		return false
	}
	if date != today {
		return true
	}
	published := time.Date(now.Year(), now.Month(), now.Day(), ecbPublishHour, 0, 0, 0, loc)
	return !now.Before(published)
}

func rejectFutureEcbDate(date string) error {
	if isoDate.MatchString(date) && date > nowUTC().Format(domain.UTCDateLayout) {
		return domain.InvalidEcbDate(date)
	}
	return nil
}

type RateFetcher func(ctx context.Context, currency, date string) (domain.Rational, error)

var (
	currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)
	isoDate      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	obsNumber    = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
)

// FetchEcbRatePerEur fetches the live ECB daily spot: units of currency per 1 EUR.
func FetchEcbRatePerEur(ctx context.Context, currency, date string) (domain.Rational, error) {
	code := strings.ToUpper(strings.TrimSpace(currency))
	if err := rejectFutureEcbDate(date); err != nil {
		return domain.Rational{}, err
	}
	if domain.IsEcbQuoteCurrency(code) {
		return domain.MustRational(1, 1), nil
	}
	if !currencyCode.MatchString(code) || !isoDate.MatchString(date) {
		return domain.Rational{}, domain.MissingEcbRate(code, date)
	}
	if !ecbSpotAvailable(date, nowUTC()) {
		return domain.Rational{}, domain.MissingEcbRate(code, date)
	}

	url := fmt.Sprintf("%s/D.%s.%s.SP00.A?startPeriod=%s&endPeriod=%s&format=csvdata", ecbBase, code, domain.EcbQuoteCurrency, date, date)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return domain.Rational{}, err
	}
	req.Header.Set("Accept", "text/csv")
	resp, err := ecbHTTPClient.Do(req)
	if err != nil {
		return domain.Rational{}, domain.MissingEcbRate(code, date)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return domain.Rational{}, domain.MissingEcbRate(code, date)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.Rational{}, domain.MissingEcbRate(code, date)
	}
	value := ParseEcbCsvObsValue(string(body))
	if value == "" {
		return domain.Rational{}, domain.MissingEcbRate(code, date)
	}
	frac := 0
	if i := strings.IndexByte(value, '.'); i >= 0 {
		frac = len(value) - i - 1
	}
	if frac < 1 {
		frac = 1
	}
	return domain.RationalFromDecimal(value, frac)
}

type ObservedRate struct {
	Rate         domain.Rational
	ObservedDate string
}

func FetchEcbRatePerEurWithLookback(
	ctx context.Context,
	currency, date string,
	maxLookbackDays int,
	fetchRate RateFetcher,
) (ObservedRate, error) {
	if fetchRate == nil {
		fetchRate = FetchEcbRatePerEur
	}
	if err := rejectFutureEcbDate(date); err != nil {
		return ObservedRate{}, err
	}
	cursor := date
	var last error
	for i := 0; i <= maxLookbackDays; i++ {
		rate, err := fetchRate(ctx, currency, cursor)
		if err == nil {
			return ObservedRate{Rate: rate, ObservedDate: cursor}, nil
		}
		if de, ok := domain.IsDomainError(err); !ok || de.Code != "MissingEcbRate" {
			return ObservedRate{}, err
		}
		last = err
		cursor = PreviousUTCDate(cursor)
	}
	if last != nil {
		return ObservedRate{}, last
	}
	return ObservedRate{}, domain.MissingEcbRate(currency, date)
}

func PreviousUTCDate(isoDate string) string {
	d, err := time.Parse("2006-01-02", isoDate)
	if err != nil {
		return isoDate
	}
	return d.AddDate(0, 0, -1).Format("2006-01-02")
}

func ParseEcbCsvObsValue(csv string) string {
	rawLines := strings.Split(strings.ReplaceAll(csv, "\r\n", "\n"), "\n")
	var lines []string
	for _, l := range rawLines {
		l = strings.TrimSpace(l)
		if l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) < 2 {
		return ""
	}
	header := splitCSVLine(lines[0])
	obsIndex := -1
	for i, h := range header {
		if h == "OBS_VALUE" {
			obsIndex = i
			break
		}
	}
	if obsIndex < 0 {
		return ""
	}
	for i := 1; i < len(lines); i++ {
		cols := splitCSVLine(lines[i])
		if obsIndex >= len(cols) {
			continue
		}
		raw := strings.TrimSpace(cols[obsIndex])
		if raw != "" && obsNumber.MatchString(raw) {
			return raw
		}
	}
	return ""
}

func splitCSVLine(line string) []string {
	var out []string
	var cur strings.Builder
	inQuotes := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if ch == '"' {
			inQuotes = !inQuotes
			continue
		}
		if ch == ',' && !inQuotes {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(ch)
	}
	out = append(out, cur.String())
	return out
}

func (s *Service) WithEcbCache(c EcbCache) *Service {
	s.ecbCache = c
	return s
}

func (s *Service) wrapEcbFetch(httpFetch RateFetcher) RateFetcher {
	if httpFetch == nil {
		httpFetch = FetchEcbRatePerEur
	}
	return func(ctx context.Context, currency, date string) (domain.Rational, error) {
		code := strings.ToUpper(strings.TrimSpace(currency))
		if err := rejectFutureEcbDate(date); err != nil {
			return domain.Rational{}, err
		}
		if domain.IsEcbQuoteCurrency(code) {
			return domain.MustRational(1, 1), nil
		}

		if s.ecbCache != nil {
			rate, missing, ok, err := s.ecbCache.GetEcbRate(ctx, code, date)
			if err != nil {
				log.Printf("ecb cache get: %v", err)
			} else if ok {
				if missing {
					return domain.Rational{}, domain.MissingEcbRate(code, date)
				}
				return rate, nil
			}
		}

		rate, err := httpFetch(ctx, code, date)
		if err == nil {
			if s.ecbCache != nil {
				if putErr := s.ecbCache.PutEcbRate(ctx, code, date, rate, false); putErr != nil {
					log.Printf("ecb cache put: %v", putErr)
				}
			}
			return rate, nil
		}
		if de, isDom := domain.IsDomainError(err); isDom && de.Code == "MissingEcbRate" && date < todayISO() {
			if s.ecbCache != nil {
				if putErr := s.ecbCache.PutEcbRate(ctx, code, date, domain.Rational{}, true); putErr != nil {
					log.Printf("ecb cache put: %v", putErr)
				}
			}
		}
		return rate, err
	}
}
