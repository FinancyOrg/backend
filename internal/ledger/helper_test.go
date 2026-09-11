package ledger

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/FinancyOrg/backend/internal/domain"
	"github.com/FinancyOrg/backend/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testService(t *testing.T, fetch RateFetcher) (*Service, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		adminURL = "postgresql://root@localhost:26257/defaultdb?sslmode=disable"
	}
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Skipf("cockroach not available: %v", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := admin.Ping(pingCtx); err != nil {
		admin.Close()
		t.Skipf("cockroach not available: %v", err)
	}

	name := "financy_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		admin.Close()
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" CASCADE")
		admin.Close()
	})

	pool, err := store.Connect(ctx, rewriteDBName(adminURL, name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	if err := store.RunMigrations(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(pool, fetch), pool
}

func rewriteDBName(url, dbName string) string {
	withoutQuery := url
	query := ""
	if i := strings.Index(url, "?"); i >= 0 {
		withoutQuery = url[:i]
		query = url[i:]
	}
	slash := strings.LastIndex(withoutQuery, "/")
	if slash < 0 {
		return url
	}
	return withoutQuery[:slash+1] + dbName + query
}

func ensureCurrency(t *testing.T, s *Service, codes ...string) {
	t.Helper()
	ctx := context.Background()
	for _, code := range codes {
		code = strings.ToUpper(strings.TrimSpace(code))
		if code == "" {
			continue
		}
		existing, err := s.GetCommodity(ctx, code)
		if err != nil {
			t.Fatal(err)
		}
		if existing != nil {
			continue
		}
		if _, err := s.CreateCommodity(ctx, CreateCommodityInput{
			Code:       code,
			Name:       testCurrencyName(code),
			MinorUnits: testCurrencyMinorUnits(code),
			Kind:       domain.CommodityCurrency,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func testCurrencyName(code string) string {
	switch code {
	case "EUR":
		return "Euro"
	case "USD":
		return "US Dollar"
	case "INR":
		return "Indian Rupee"
	case "JPY":
		return "Japanese Yen"
	case "GBP":
		return "British Pound"
	default:
		return code
	}
}

func testCurrencyMinorUnits(code string) int {
	switch code {
	case "JPY", "ISK", "KRW":
		return 0
	default:
		return 2
	}
}

func assertNoFunctionalCurrency(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM app_config WHERE key = $1
	`, functionalCommodityConfigKey).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("functional_commodity_id must not be set")
	}
}

func assertFunctionalCurrency(t *testing.T, pool *pgxpool.Pool, want string) {
	t.Helper()
	var value string
	err := pool.QueryRow(context.Background(), `
		SELECT value FROM app_config WHERE key = $1
	`, functionalCommodityConfigKey).Scan(&value)
	if err != nil {
		t.Fatal(err)
	}
	if value != want {
		t.Fatalf("F=%s want %s", value, want)
	}
}

func boot(t *testing.T, s *Service, commodityID string) {
	t.Helper()
	if commodityID == "" {
		t.Fatal("boot requires a functional currency")
	}
	ensureCurrency(t, s, commodityID, "USD", "INR", "JPY")
	if _, _, err := s.SetDefaultCurrency(context.Background(), commodityID); err != nil {
		t.Fatal(err)
	}
}

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	de, ok := domain.IsDomainError(err)
	if !ok || de.Code != code {
		t.Fatalf("want domain error %s, got %v", code, err)
	}
}

type memFlags struct {
	items map[string]AccountFlags
}

func (m *memFlags) ListAccountFlags(context.Context) (map[string]AccountFlags, error) {
	out := map[string]AccountFlags{}
	for id, flags := range m.items {
		out[id] = flags
	}
	return out, nil
}

func (m *memFlags) PutAccountFlags(_ context.Context, accountID string, flags AccountFlags) error {
	if m.items == nil {
		m.items = map[string]AccountFlags{}
	}
	m.items[accountID] = flags
	return nil
}

func (m *memFlags) DeleteAccountFlags(_ context.Context, accountID string) error {
	delete(m.items, accountID)
	return nil
}

type memSettings struct {
	items map[string]string
}

func (m *memSettings) GetSetting(_ context.Context, key string) (*string, error) {
	if m.items == nil {
		return nil, nil
	}
	value, ok := m.items[key]
	if !ok {
		return nil, nil
	}
	copied := value
	return &copied, nil
}

func (m *memSettings) PutSetting(_ context.Context, key, value string) error {
	if m.items == nil {
		m.items = map[string]string{}
	}
	m.items[key] = value
	return nil
}

type memEcbCache struct {
	rates map[string]domain.Rational
}

func (m *memEcbCache) GetEcbRate(_ context.Context, currency, date string) (domain.Rational, bool, bool, error) {
	if m == nil || m.rates == nil {
		return domain.Rational{}, false, false, nil
	}
	rate, ok := m.rates[currency+"|"+date]
	if !ok {
		return domain.Rational{}, false, false, nil
	}
	return rate, false, true, nil
}

func (m *memEcbCache) PutEcbRate(_ context.Context, currency, date string, rate domain.Rational, missing bool) error {
	if m == nil || missing {
		return nil
	}
	if m.rates == nil {
		m.rates = map[string]domain.Rational{}
	}
	m.rates[currency+"|"+date] = rate
	return nil
}
