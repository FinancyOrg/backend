package ledger

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/FinancyOrg/backend/internal/domain"
)

const (
	timezoneSettingKey              = "timezone"
	themeSettingKey                 = "theme"
	presentationCommoditySettingKey = "presentationCommodityId"
	functionalChangeJobKey          = "functionalChangeJob"

	FunctionalChangeRunning = "running"
	FunctionalChangeDone    = "done"
	FunctionalChangeError   = "error"

	// Settings polls GET /api/config at this interval while a restamp job runs.
	FunctionalChangePollInterval = 10 * time.Second

	functionalChangeInterrupted = "Functional currency restatement was interrupted. Retry."
)

// SettingsStore persists presentation/config values in Mongo collection
// `store` (not the CRDB-backed cache). Timezone belongs here: journals are
// UTC instants; months/years are bucketed in Go. See docs/rebuild/backend/14-time.md.
type SettingsStore interface {
	GetSetting(ctx context.Context, key string) (value *string, err error)
	PutSetting(ctx context.Context, key, value string) error
}

func (s *Service) WithSettings(store SettingsStore) *Service {
	if store != nil {
		s.settings = store
	}
	return s
}

func (s *Service) getSetting(ctx context.Context, key string) (*string, error) {
	if s.settings == nil {
		return nil, nil
	}
	return s.settings.GetSetting(ctx, key)
}

func (s *Service) putSetting(ctx context.Context, key, value string) error {
	if s.settings == nil {
		return nil
	}
	return s.settings.PutSetting(ctx, key, value)
}

func (s *Service) LedgerLocation(ctx context.Context) *time.Location {
	name := domain.DefaultLedgerTimezone
	stored, err := s.getSetting(ctx, timezoneSettingKey)
	if err != nil {
		log.Printf("ledger timezone get: %v", err)
	} else if stored != nil && strings.TrimSpace(*stored) != "" {
		name = strings.TrimSpace(*stored)
	} else if putErr := s.putSetting(ctx, timezoneSettingKey, domain.DefaultLedgerTimezone); putErr != nil {
		log.Printf("ledger timezone put: %v", putErr)
	}
	return domain.LoadLedgerLocation(name)
}

func (s *Service) SetLedgerTimezone(ctx context.Context, name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, domain.InvalidSetting("ledger timezone is required")
	}
	if _, err := time.LoadLocation(name); err != nil {
		return nil, domain.InvalidSetting("unknown IANA timezone: " + name)
	}
	if err := s.putSetting(ctx, timezoneSettingKey, name); err != nil {
		return nil, err
	}
	s.invalidate(ctx, CacheKindPeriods)
	return domain.LoadLedgerLocation(name), nil
}

func (s *Service) GetUiTheme(ctx context.Context) (UiTheme, error) {
	value, err := s.getSetting(ctx, themeSettingKey)
	if err != nil {
		return ThemeLight, err
	}
	if value != nil && *value == string(ThemeDark) {
		return ThemeDark, nil
	}
	return ThemeLight, nil
}

func (s *Service) SetUiTheme(ctx context.Context, theme UiTheme) (UiTheme, error) {
	if theme != ThemeLight && theme != ThemeDark {
		theme = ThemeLight
	}
	if err := s.putSetting(ctx, themeSettingKey, string(theme)); err != nil {
		return "", err
	}
	if err := s.RecordAudit(ctx, "ui_theme.set", map[string]any{"theme": theme}); err != nil {
		return "", err
	}
	return theme, nil
}

const (
	// Floor covers theme/timezone writes plus an empty restamp round-trip.
	functionalChangeProgressFloorMs = 800
	// Restamp is O(postings). Settings times the wait by posted journal
	// count, the user-visible unit. Underestimate is preferred: the bar
	// holds at 95% if PUT is still running.
	functionalChangeProgressPerJournalMs = 40
)

// FunctionalChangeEstimatedMs is how long Settings should take to fill the
// restamp progress bar. PUT /api/config is one blocking request; this is
// not measured progress.
func FunctionalChangeEstimatedMs(postedJournalCount int) int {
	if postedJournalCount < 0 {
		postedJournalCount = 0
	}
	return functionalChangeProgressFloorMs + postedJournalCount*functionalChangeProgressPerJournalMs
}

func (s *Service) CountPostedJournals(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM journal_entries WHERE status = 'posted'`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// FunctionalChangeJob is the async restamp started by a confirmed F change.
// PUT returns while Status is running; F is still the old commodity until done.
type FunctionalChangeJob struct {
	Status string `json:"status"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Error  string `json:"error,omitempty"`
}

// AppConfig is first-run onboarding. Functional currency lives in CRDB;
// theme and timezone live in Mongo. PostedJournalCount is for the Settings
// restamp progress bar, not a ledger setting.
type AppConfig struct {
	PresentationCommodityID     *string
	Theme                       UiTheme
	Timezone                    string
	PostedJournalCount          int
	FunctionalChangeEstimatedMs int
	FunctionalChangePollMs      int
	FunctionalChange            *FunctionalChangeJob
}

func (s *Service) AppConfig(ctx context.Context) (AppConfig, error) {
	id, err := s.GetDefaultCommodityID(ctx)
	if err != nil {
		return AppConfig{}, err
	}
	theme, err := s.GetUiTheme(ctx)
	if err != nil {
		return AppConfig{}, err
	}
	timezone := domain.DefaultLedgerTimezone
	stored, err := s.getSetting(ctx, timezoneSettingKey)
	if err != nil {
		return AppConfig{}, err
	}
	if stored != nil && strings.TrimSpace(*stored) != "" {
		timezone = strings.TrimSpace(*stored)
	}
	count, err := s.CountPostedJournals(ctx)
	if err != nil {
		return AppConfig{}, err
	}
	job, err := s.functionalChangeSnapshot(ctx)
	if err != nil {
		return AppConfig{}, err
	}
	return AppConfig{
		PresentationCommodityID:     id,
		Theme:                       theme,
		Timezone:                    timezone,
		PostedJournalCount:          count,
		FunctionalChangeEstimatedMs: FunctionalChangeEstimatedMs(count),
		FunctionalChangePollMs:      int(FunctionalChangePollInterval / time.Millisecond),
		FunctionalChange:            job,
	}, nil
}

// SetAppConfig writes theme and timezone to Mongo, then functional currency
// to CRDB. Currency is last so a failed currency write leaves onboarding
// open. Changing F requires confirm=true and starts an async restamp; PUT
// returns while F is still the old commodity.
func (s *Service) SetAppConfig(ctx context.Context, commodityID string, theme UiTheme, timezone string, confirm bool) (AppConfig, error) {
	if _, err := s.SetUiTheme(ctx, theme); err != nil {
		return AppConfig{}, err
	}
	if _, err := s.SetLedgerTimezone(ctx, timezone); err != nil {
		return AppConfig{}, err
	}
	existing, err := s.GetDefaultCommodityID(ctx)
	if err != nil {
		return AppConfig{}, err
	}
	if existing == nil || *existing == commodityID {
		if _, _, err := s.SetDefaultCurrency(ctx, commodityID); err != nil {
			return AppConfig{}, err
		}
	} else if confirm {
		if _, err := s.StartFunctionalCurrencyChange(ctx, commodityID); err != nil {
			return AppConfig{}, err
		}
	} else {
		return AppConfig{}, domain.FunctionalCurrencyChangeNotConfirmed(*existing, commodityID)
	}
	return s.AppConfig(ctx)
}

func (s *Service) StartFunctionalCurrencyChange(ctx context.Context, commodityID string) (*FunctionalChangeJob, error) {
	commodity, err := s.GetCommodity(ctx, commodityID)
	if err != nil {
		return nil, err
	}
	if commodity == nil || commodity.Kind != domain.CommodityCurrency {
		return nil, domain.InvalidCurrency("Unknown or non-currency commodity: " + commodityID)
	}
	if !domain.IsSupportedCurrency(commodity.ID) {
		return nil, domain.UnsupportedCurrency(commodity.ID)
	}
	existing, err := s.GetDefaultCommodityID(ctx)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, domain.FunctionalCurrencyNotSet()
	}
	if *existing == commodity.ID {
		job := s.memoryJob()
		return job, nil
	}

	s.changeMu.Lock()
	if s.changeRunning {
		from, to := s.changeJob.From, s.changeJob.To
		s.changeMu.Unlock()
		return nil, domain.FunctionalCurrencyChangeInProgress(from, to)
	}
	job := FunctionalChangeJob{
		Status: FunctionalChangeRunning,
		From:   *existing,
		To:     commodity.ID,
	}
	s.changeRunning = true
	s.changeJob = job
	s.changeMu.Unlock()
	if err := s.saveFunctionalChangeJob(ctx, job); err != nil {
		log.Printf("functional change job persist: %v", err)
	}
	go s.runFunctionalCurrencyChange(commodity.ID)
	return &job, nil
}

func (s *Service) runFunctionalCurrencyChange(commodityID string) {
	ctx := context.Background()
	_, _, err := s.ChangeFunctionalCurrency(ctx, commodityID)
	s.changeMu.Lock()
	s.changeRunning = false
	if err != nil {
		s.changeJob.Status = FunctionalChangeError
		s.changeJob.Error = err.Error()
		log.Printf("functional change job: %v", err)
	} else {
		s.changeJob.Status = FunctionalChangeDone
		s.changeJob.Error = ""
	}
	job := s.changeJob
	s.changeMu.Unlock()
	if saveErr := s.saveFunctionalChangeJob(ctx, job); saveErr != nil {
		log.Printf("functional change job persist: %v", saveErr)
	}
}

func (s *Service) memoryJob() *FunctionalChangeJob {
	s.changeMu.Lock()
	defer s.changeMu.Unlock()
	if s.changeJob.Status == "" {
		return nil
	}
	job := s.changeJob
	return &job
}

func (s *Service) functionalChangeSnapshot(ctx context.Context) (*FunctionalChangeJob, error) {
	s.changeMu.Lock()
	running := s.changeRunning
	mem := s.changeJob
	s.changeMu.Unlock()
	if running {
		job := mem
		return &job, nil
	}
	stored, err := s.loadFunctionalChangeJob(ctx)
	if err != nil {
		return nil, err
	}
	if stored != nil && stored.Status == FunctionalChangeRunning {
		stored.Status = FunctionalChangeError
		stored.Error = functionalChangeInterrupted
		if saveErr := s.saveFunctionalChangeJob(ctx, *stored); saveErr != nil {
			log.Printf("functional change job persist: %v", saveErr)
		}
		s.changeMu.Lock()
		s.changeJob = *stored
		s.changeMu.Unlock()
		return stored, nil
	}
	if mem.Status != "" {
		job := mem
		return &job, nil
	}
	return stored, nil
}

func (s *Service) loadFunctionalChangeJob(ctx context.Context) (*FunctionalChangeJob, error) {
	raw, err := s.getSetting(ctx, functionalChangeJobKey)
	if err != nil || raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, err
	}
	var job FunctionalChangeJob
	if err := json.Unmarshal([]byte(*raw), &job); err != nil {
		return nil, err
	}
	if job.Status == "" {
		return nil, nil
	}
	return &job, nil
}

func (s *Service) saveFunctionalChangeJob(ctx context.Context, job FunctionalChangeJob) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return s.putSetting(ctx, functionalChangeJobKey, string(payload))
}
