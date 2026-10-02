// ProfileService manages metric profiles: named sets of custom metrics
// (profile_metrics) that apply to devices by sysObjectID prefix, with
// per-device attach/detach overrides and poll-run bookkeeping. It also owns
// the custom metric key registry (metrics_catalog.go's customMetrics),
// refreshed by Load whenever the set of metric keys changes.
package services

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/custommetric"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

var (
	ErrProfileNotFound    = errors.New("metric profile not found")
	ErrMetricNotFound     = errors.New("custom metric not found")
	ErrProfileBuiltin     = errors.New("the built-in profile cannot be deleted; copy it instead")
	ErrProfileNameTaken   = errors.New("a metric profile with this name already exists")
	ErrMetricKeyTaken     = errors.New("a custom metric with this key already exists")
	ErrMetricKeyImmutable = errors.New("a metric's key cannot be changed once created")
)

// ProfileInput is what the API accepts to create or update a profile.
type ProfileInput struct {
	Name                string   `json:"name"`
	Description         string   `json:"description"`
	MatchPrefixes       []string `json:"match_prefixes"`
	PollIntervalMinutes int      `json:"poll_interval_minutes"`
}

// ProfileView is a profile as the list page shows it.
type ProfileView struct {
	models.MetricProfile
	Metrics int `json:"metrics" gorm:"column:metrics"`
	Devices int `json:"devices" gorm:"column:devices"`
}

// ProfileDetail is a profile with its metrics, in position order.
type ProfileDetail struct {
	models.MetricProfile
	Metrics []models.ProfileMetric `json:"metrics"`
}

// ProfileWithMetrics is a profile that applies to a device, with its metrics.
type ProfileWithMetrics struct {
	Profile models.MetricProfile   `json:"profile"`
	Metrics []models.ProfileMetric `json:"metrics"`
}

// DeviceProfileView is one profile's standing with a particular device: Mode
// is "auto" (no override), "attach" or "detach"; Matched is what sysObjectID
// prefix matching alone would say; Applies accounts for the override too.
type DeviceProfileView struct {
	Profile models.MetricProfile     `json:"profile"`
	Applies bool                     `json:"applies"`
	Matched bool                     `json:"matched"`
	Mode    string                   `json:"mode"`
	LastRun *models.DeviceProfileRun `json:"last_run"`
}

// ProfileService stores metric profiles and their custom metrics.
type ProfileService struct {
	db *gorm.DB
}

func NewProfileService(db *gorm.DB) *ProfileService { return &ProfileService{db: db} }

// MatchesPrefix reports whether objectID starts with any prefix on whole
// arcs: "1.3.6.1.4.1.9.1" matches "1.3.6.1.4.1.9.1.2066" but not
// "1.3.6.1.4.1.9.12.3". Leading dots are ignored on both sides.
func MatchesPrefix(objectID string, prefixes []string) bool {
	oid := strings.Trim(objectID, ".")
	if oid == "" {
		return false
	}
	for _, p := range prefixes {
		p = strings.Trim(p, ".")
		if p != "" && (oid == p || strings.HasPrefix(oid, p+".")) {
			return true
		}
	}
	return false
}

var (
	metricKey  = regexp.MustCompile(`^[a-z][a-z0-9_]{2,62}$`)
	numericOID = regexp.MustCompile(`^\d+(\.\d+)+$`)
)

// ValidateMetric checks a profile metric's shape: the key rule (reserved
// against if_/ups_ and built-in names, not just BuiltinMetric since a
// profile metric must never collide with another profile's key either, which
// the database's UNIQUE constraint on profile_metrics.key enforces), numeric
// OIDs, and that each source/kind/label/rule combination has what it needs.
func ValidateMetric(m models.ProfileMetric) error {
	switch {
	case strings.TrimSpace(m.Name) == "":
		return errors.New("give the metric a name")
	case !metricKey.MatchString(m.Key):
		return errors.New("the key must be 3–63 lowercase letters, digits or underscores, starting with a letter")
	case strings.HasPrefix(m.Key, "if_") || strings.HasPrefix(m.Key, "ups_") || BuiltinMetric(m.Key):
		return errors.New("that key is reserved for Sentinel's own metrics")
	case m.Scale == 0:
		return errors.New("the scale cannot be 0")
	}
	oids := map[string]string{"OID": m.OID, "second OID": m.OID2, "precision OID": m.PrecisionOID, "filter OID": m.FilterOID,
		"label OID": m.LabelOID, "pointer OID": m.LabelPointerOID, "target OID": m.LabelTargetOID}
	for what, o := range oids {
		if o != "" && !numericOID.MatchString(o) {
			return fmt.Errorf("the %s must be numeric, like 1.3.6.1.2.1.1.3", what)
		}
	}
	switch m.Source {
	case "scalar", "column":
		if m.OID == "" {
			return errors.New("choose the OID to read")
		}
	case "used_free_pct":
		if m.OID == "" || m.OID2 == "" {
			return errors.New("used/free needs both the used and the free column")
		}
	default:
		return errors.New("source must be scalar, column or used_free_pct")
	}
	switch m.Kind {
	case "gauge", "counter":
	case "status":
		if len(m.OKStates) == 0 {
			return errors.New("a status metric needs at least one OK state")
		}
	default:
		return errors.New("kind must be gauge, counter or status")
	}
	switch m.LabelMode {
	case "index":
	case "column", "same_index":
		if m.LabelOID == "" {
			return errors.New("choose the column the label comes from")
		}
	case "pointer":
		if m.LabelPointerOID == "" || m.LabelTargetOID == "" {
			return errors.New("a pointer label needs the pointer column and the target column")
		}
	default:
		return errors.New("label must be index, column, same_index or pointer")
	}
	switch m.RuleKind {
	case "":
	case "above", "below":
		if m.RuleValue == nil {
			return errors.New("the rule needs a value")
		}
	case "not_ok":
		if m.Kind != "status" {
			return errors.New(`"not OK" rules are for status metrics`)
		}
	default:
		return errors.New("rule must be above, below or not_ok")
	}
	if m.RuleHoldMinutes < 0 || m.RuleHoldMinutes > 1440 {
		return errors.New("the hold must be 0–1440 minutes")
	}
	return nil
}

// ToDefinition converts a stored profile metric into the pure evaluator's
// Definition.
func ToDefinition(m models.ProfileMetric) custommetric.Definition {
	d := custommetric.Definition{Name: m.Name, Key: m.Key, Source: m.Source, Kind: m.Kind, Scale: m.Scale,
		OID: m.OID, OID2: m.OID2, PrecisionOID: m.PrecisionOID, FilterOID: m.FilterOID, FilterValues: m.FilterValues,
		LabelMode: m.LabelMode, LabelOID: m.LabelOID, LabelPointerOID: m.LabelPointerOID, LabelTargetOID: m.LabelTargetOID,
		OKStates: m.OKStates, StateNames: map[int64]string(m.StateNames),
		Rule: custommetric.Rule{Kind: m.RuleKind, Hold: time.Duration(m.RuleHoldMinutes) * time.Minute, Enabled: m.RuleEnabled}}
	if m.RuleValue != nil {
		d.Rule.Value = *m.RuleValue
	}
	return d
}

// copyKey suffixes key with _copy (then _copy2, _copy3 …), trimming the base
// so the result fits in 63 characters.
func copyKey(key string, taken func(string) bool) string {
	for i := 1; ; i++ {
		suffix := "_copy"
		if i > 1 {
			suffix += strconv.Itoa(i)
		}
		base := key
		if len(base)+len(suffix) > 63 {
			base = base[:63-len(suffix)]
		}
		if k := base + suffix; !taken(k) {
			return k
		}
	}
}

// appliesTo reports whether profile p applies to a device with sysObjectID,
// given its override on that profile (overrideMode, hasOverride): attach
// forces it on, detach forces it off, and with no override it is whatever
// MatchesPrefix says.
func appliesTo(p models.MetricProfile, sysObjectID, overrideMode string, hasOverride bool) bool {
	if hasOverride {
		return overrideMode == models.ProfileOverrideAttach
	}
	return MatchesPrefix(sysObjectID, p.MatchPrefixes)
}

// queueMetricSeriesDeletion queues a metric key's series for the nightly
// cleanup and removes them, inside the caller's transaction. Run before (or
// as part of) deleting the profile_metrics row(s) that used the key.
func queueMetricSeriesDeletion(tx *gorm.DB, key string) error {
	if err := tx.Exec(`INSERT INTO metrics.deleted_series (series_id)
		SELECT id FROM metrics.series WHERE metric = ? ON CONFLICT DO NOTHING`, key).Error; err != nil {
		return fmt.Errorf("queueing metric series for cleanup: %w", err)
	}
	if err := tx.Exec(`DELETE FROM metrics.series WHERE metric = ?`, key).Error; err != nil {
		return fmt.Errorf("deleting metric series: %w", err)
	}
	return nil
}

// normalizeProfileInput validates a ProfileInput and returns its normalized
// name and match prefixes.
func normalizeProfileInput(in ProfileInput) (string, models.StringArray, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", nil, errors.New("give the profile a name")
	}
	switch in.PollIntervalMinutes {
	case 1, 5, 15:
	default:
		return "", nil, errors.New("the poll interval must be 1, 5 or 15 minutes")
	}
	prefixes := make(models.StringArray, 0, len(in.MatchPrefixes))
	for _, raw := range in.MatchPrefixes {
		p := strings.Trim(strings.TrimSpace(raw), ".")
		if p == "" {
			continue
		}
		if !numericOID.MatchString(p) {
			return "", nil, fmt.Errorf("the match prefix %q must be numeric, like 1.3.6.1.4.1.9", p)
		}
		prefixes = append(prefixes, p)
	}
	return name, prefixes, nil
}

// Load refreshes the custom metric key registry from every stored
// profile_metrics row. Call it after any change to the set of metric keys.
func (s *ProfileService) Load(ctx context.Context) error {
	var keys []string
	if err := s.db.WithContext(ctx).Model(&models.ProfileMetric{}).Pluck("key", &keys).Error; err != nil {
		return fmt.Errorf("loading custom metric keys: %w", err)
	}
	SetCustomMetricKeys(keys)
	return nil
}

// SeedStarter creates the "Cisco switch health" built-in profile once, by
// name. It never overwrites an existing profile of that name (an admin may
// have edited or even renamed a copy, but the original stays untouched once
// created).
func (s *ProfileService) SeedStarter(ctx context.Context) error {
	var n int64
	if err := s.db.WithContext(ctx).Model(&models.MetricProfile{}).Where("name = ?", "Cisco switch health").Count(&n).Error; err != nil {
		return fmt.Errorf("checking for the starter profile: %w", err)
	}
	if n > 0 {
		return nil
	}
	p, metrics := starterProfile()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&p).Error; err != nil {
			return fmt.Errorf("creating starter profile: %w", err)
		}
		for i := range metrics {
			metrics[i].ProfileID = p.ID
		}
		if err := tx.Create(&metrics).Error; err != nil {
			return fmt.Errorf("creating starter metrics: %w", err)
		}
		return nil
	})
}

// metricsFor loads a profile's metrics, in position order.
func (s *ProfileService) metricsFor(ctx context.Context, profileID uuid.UUID) ([]models.ProfileMetric, error) {
	var rows []models.ProfileMetric
	if err := s.db.WithContext(ctx).Where("profile_id = ?", profileID).Order("position").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("loading profile metrics: %w", err)
	}
	return rows, nil
}

// overridesFor loads a device's profile overrides, by profile id.
func (s *ProfileService) overridesFor(ctx context.Context, deviceID uuid.UUID) (map[uuid.UUID]string, error) {
	var rows []models.DeviceProfileOverride
	if err := s.db.WithContext(ctx).Where("device_id = ?", deviceID).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("loading device profile overrides: %w", err)
	}
	out := make(map[uuid.UUID]string, len(rows))
	for _, r := range rows {
		out[r.ProfileID] = r.Mode
	}
	return out, nil
}

// runsFor loads a device's most recent profile runs, by profile id.
func (s *ProfileService) runsFor(ctx context.Context, deviceID uuid.UUID) (map[uuid.UUID]models.DeviceProfileRun, error) {
	var rows []models.DeviceProfileRun
	if err := s.db.WithContext(ctx).Where("device_id = ?", deviceID).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("loading device profile runs: %w", err)
	}
	out := make(map[uuid.UUID]models.DeviceProfileRun, len(rows))
	for _, r := range rows {
		out[r.ProfileID] = r
	}
	return out, nil
}

// List returns every profile, by name, with its metric and matching-device
// counts.
func (s *ProfileService) List(ctx context.Context) ([]ProfileView, error) {
	var profiles []models.MetricProfile
	if err := s.db.WithContext(ctx).Order("name").Find(&profiles).Error; err != nil {
		return nil, fmt.Errorf("listing profiles: %w", err)
	}
	var counts []struct {
		ProfileID uuid.UUID `gorm:"column:profile_id"`
		N         int       `gorm:"column:n"`
	}
	if err := s.db.WithContext(ctx).Model(&models.ProfileMetric{}).
		Select("profile_id, count(*) AS n").Group("profile_id").Scan(&counts).Error; err != nil {
		return nil, fmt.Errorf("counting profile metrics: %w", err)
	}
	metricCounts := make(map[uuid.UUID]int, len(counts))
	for _, c := range counts {
		metricCounts[c.ProfileID] = c.N
	}
	var devices []struct {
		ID          uuid.UUID `gorm:"column:id"`
		SysObjectID string    `gorm:"column:sys_object_id"`
	}
	if err := s.db.WithContext(ctx).Table("devices").Select("id, sys_object_id").Scan(&devices).Error; err != nil {
		return nil, fmt.Errorf("loading devices: %w", err)
	}
	var overrides []models.DeviceProfileOverride
	if err := s.db.WithContext(ctx).Find(&overrides).Error; err != nil {
		return nil, fmt.Errorf("loading device profile overrides: %w", err)
	}
	type pair struct {
		device, profile uuid.UUID
	}
	overrideMode := make(map[pair]string, len(overrides))
	for _, o := range overrides {
		overrideMode[pair{o.DeviceID, o.ProfileID}] = o.Mode
	}
	out := make([]ProfileView, 0, len(profiles))
	for _, p := range profiles {
		n := 0
		for _, d := range devices {
			mode, has := overrideMode[pair{d.ID, p.ID}]
			if appliesTo(p, d.SysObjectID, mode, has) {
				n++
			}
		}
		out = append(out, ProfileView{MetricProfile: p, Metrics: metricCounts[p.ID], Devices: n})
	}
	return out, nil
}

// Get returns one profile with its metrics.
func (s *ProfileService) Get(ctx context.Context, id uuid.UUID) (*ProfileDetail, error) {
	var p models.MetricProfile
	if err := s.db.WithContext(ctx).First(&p, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProfileNotFound
		}
		return nil, fmt.Errorf("loading profile: %w", err)
	}
	metrics, err := s.metricsFor(ctx, id)
	if err != nil {
		return nil, err
	}
	return &ProfileDetail{MetricProfile: p, Metrics: metrics}, nil
}

// Create stores a new (non-built-in) profile with no metrics yet.
func (s *ProfileService) Create(ctx context.Context, in ProfileInput) (*models.MetricProfile, error) {
	name, prefixes, err := normalizeProfileInput(in)
	if err != nil {
		return nil, err
	}
	p := models.MetricProfile{Name: name, Description: in.Description, MatchPrefixes: prefixes, PollIntervalMinutes: in.PollIntervalMinutes}
	if err := s.db.WithContext(ctx).Create(&p).Error; err != nil {
		if isDuplicateKey(err) {
			return nil, ErrProfileNameTaken
		}
		return nil, fmt.Errorf("creating profile: %w", err)
	}
	return &p, nil
}

// Update changes a profile's name, description, match prefixes and poll
// interval. A built-in profile may still be updated (only deleting it is
// refused).
func (s *ProfileService) Update(ctx context.Context, id uuid.UUID, in ProfileInput) (*models.MetricProfile, *models.MetricProfile, error) {
	var before models.MetricProfile
	if err := s.db.WithContext(ctx).First(&before, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrProfileNotFound
		}
		return nil, nil, fmt.Errorf("loading profile: %w", err)
	}
	name, prefixes, err := normalizeProfileInput(in)
	if err != nil {
		return nil, nil, err
	}
	err = s.db.WithContext(ctx).Model(&models.MetricProfile{}).Where("id = ?", id).Updates(map[string]any{
		"name": name, "description": in.Description, "match_prefixes": prefixes,
		"poll_interval_minutes": in.PollIntervalMinutes, "updated_at": gorm.Expr("now()"),
	}).Error
	if err != nil {
		if isDuplicateKey(err) {
			return nil, nil, ErrProfileNameTaken
		}
		return nil, nil, fmt.Errorf("updating profile: %w", err)
	}
	var after models.MetricProfile
	if err := s.db.WithContext(ctx).First(&after, "id = ?", id).Error; err != nil {
		return nil, nil, fmt.Errorf("loading profile: %w", err)
	}
	return &before, &after, nil
}

// Delete removes a non-built-in profile and everything that references it
// (profile_metrics, device_profile_overrides and device_profile_runs cascade
// from metric_profiles), queuing each of its metrics' series for the nightly
// cleanup first.
func (s *ProfileService) Delete(ctx context.Context, id uuid.UUID) (*models.MetricProfile, error) {
	var p models.MetricProfile
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&p, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProfileNotFound
			}
			return fmt.Errorf("loading profile: %w", err)
		}
		if p.Builtin {
			return ErrProfileBuiltin
		}
		var keys []string
		if err := tx.Model(&models.ProfileMetric{}).Where("profile_id = ?", id).Pluck("key", &keys).Error; err != nil {
			return fmt.Errorf("loading profile metric keys: %w", err)
		}
		if err := tx.Delete(&models.MetricProfile{}, "id = ?", id).Error; err != nil {
			return fmt.Errorf("deleting profile: %w", err)
		}
		for _, k := range keys {
			if err := queueMetricSeriesDeletion(tx, k); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.Load(ctx); err != nil {
		return nil, err
	}
	return &p, nil
}

// Copy duplicates a profile (built-in or not) as a new, non-built-in one:
// "<name> (copy)", then "<name> (copy 2)" and so on while the name is taken;
// each metric gets a unique key via copyKey.
func (s *ProfileService) Copy(ctx context.Context, id uuid.UUID) (*ProfileDetail, error) {
	var detail *ProfileDetail
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var p models.MetricProfile
		if err := tx.First(&p, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProfileNotFound
			}
			return fmt.Errorf("loading profile: %w", err)
		}
		var metrics []models.ProfileMetric
		if err := tx.Where("profile_id = ?", id).Order("position").Find(&metrics).Error; err != nil {
			return fmt.Errorf("loading profile metrics: %w", err)
		}
		nameTaken := func(name string) bool {
			var n int64
			tx.Model(&models.MetricProfile{}).Where("name = ?", name).Count(&n)
			return n > 0
		}
		newName := p.Name + " (copy)"
		for i := 2; nameTaken(newName); i++ {
			newName = fmt.Sprintf("%s (copy %d)", p.Name, i)
		}
		newProfile := models.MetricProfile{Name: newName, Description: p.Description,
			MatchPrefixes: p.MatchPrefixes, PollIntervalMinutes: p.PollIntervalMinutes}
		if err := tx.Create(&newProfile).Error; err != nil {
			return fmt.Errorf("creating profile copy: %w", err)
		}
		assigned := map[string]bool{}
		keyTaken := func(k string) bool {
			if assigned[k] {
				return true
			}
			var n int64
			tx.Model(&models.ProfileMetric{}).Where("key = ?", k).Count(&n)
			return n > 0
		}
		newMetrics := make([]models.ProfileMetric, len(metrics))
		for i, m := range metrics {
			nk := copyKey(m.Key, keyTaken)
			assigned[nk] = true
			nm := m
			nm.ID = uuid.Nil
			nm.ProfileID = newProfile.ID
			nm.Key = nk
			nm.CreatedAt, nm.UpdatedAt = time.Time{}, time.Time{}
			newMetrics[i] = nm
		}
		if len(newMetrics) > 0 {
			if err := tx.Create(&newMetrics).Error; err != nil {
				return fmt.Errorf("creating metric copies: %w", err)
			}
		}
		detail = &ProfileDetail{MetricProfile: newProfile, Metrics: newMetrics}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.Load(ctx); err != nil {
		return nil, err
	}
	return detail, nil
}

// GetMetric loads one custom metric by id.
func (s *ProfileService) GetMetric(ctx context.Context, id uuid.UUID) (*models.ProfileMetric, error) {
	var m models.ProfileMetric
	if err := s.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMetricNotFound
		}
		return nil, fmt.Errorf("loading metric: %w", err)
	}
	return &m, nil
}

// CreateMetric adds a metric to a profile, appended after its current ones.
func (s *ProfileService) CreateMetric(ctx context.Context, profileID uuid.UUID, m models.ProfileMetric) (*models.ProfileMetric, error) {
	m.ID = uuid.Nil
	m.ProfileID = profileID
	if err := ValidateMetric(m); err != nil {
		return nil, err
	}
	var next int64
	if err := s.db.WithContext(ctx).Model(&models.ProfileMetric{}).Where("profile_id = ?", profileID).Count(&next).Error; err != nil {
		return nil, fmt.Errorf("counting profile metrics: %w", err)
	}
	m.Position = int(next)
	if err := s.db.WithContext(ctx).Create(&m).Error; err != nil {
		switch {
		case isDuplicateKey(err):
			return nil, ErrMetricKeyTaken
		case isForeignKeyViolation(err):
			return nil, ErrProfileNotFound
		default:
			return nil, fmt.Errorf("creating metric: %w", err)
		}
	}
	if err := s.Load(ctx); err != nil {
		return nil, err
	}
	return &m, nil
}

// UpdateMetric changes a metric's definition. Its key cannot change.
func (s *ProfileService) UpdateMetric(ctx context.Context, id uuid.UUID, m models.ProfileMetric) (*models.ProfileMetric, *models.ProfileMetric, error) {
	before, err := s.GetMetric(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if m.Key != before.Key {
		return nil, nil, ErrMetricKeyImmutable
	}
	m.ProfileID = before.ProfileID
	if err := ValidateMetric(m); err != nil {
		return nil, nil, err
	}
	updates := map[string]any{
		"name": m.Name, "source": m.Source, "kind": m.Kind, "units": m.Units, "scale": m.Scale,
		"oid": m.OID, "oid2": m.OID2, "precision_oid": m.PrecisionOID, "filter_oid": m.FilterOID,
		"filter_values": m.FilterValues, "label_mode": m.LabelMode, "label_oid": m.LabelOID,
		"label_pointer_oid": m.LabelPointerOID, "label_target_oid": m.LabelTargetOID,
		"ok_states": m.OKStates, "state_names": m.StateNames, "rule_kind": m.RuleKind,
		"rule_value": m.RuleValue, "rule_hold_minutes": m.RuleHoldMinutes, "rule_enabled": m.RuleEnabled,
		"position": m.Position, "updated_at": gorm.Expr("now()"),
	}
	if err := s.db.WithContext(ctx).Model(&models.ProfileMetric{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, nil, fmt.Errorf("updating metric: %w", err)
	}
	after, err := s.GetMetric(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if err := s.Load(ctx); err != nil {
		return nil, nil, err
	}
	return before, after, nil
}

// DeleteMetric removes one metric and queues its series for the nightly
// cleanup.
func (s *ProfileService) DeleteMetric(ctx context.Context, id uuid.UUID) (*models.ProfileMetric, error) {
	var m models.ProfileMetric
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&m, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrMetricNotFound
			}
			return fmt.Errorf("loading metric: %w", err)
		}
		if err := tx.Delete(&models.ProfileMetric{}, "id = ?", id).Error; err != nil {
			return fmt.Errorf("deleting metric: %w", err)
		}
		return queueMetricSeriesDeletion(tx, m.Key)
	})
	if err != nil {
		return nil, err
	}
	if err := s.Load(ctx); err != nil {
		return nil, err
	}
	return &m, nil
}

// MetricDataDevices counts the devices that have ever had data for a metric
// key, so the UI can warn before deleting one with history.
func (s *ProfileService) MetricDataDevices(ctx context.Context, key string) (int, error) {
	var n int64
	if err := s.db.WithContext(ctx).Raw(`SELECT count(DISTINCT device_id) FROM metrics.series WHERE metric = ?`, key).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("counting devices with metric data: %w", err)
	}
	return int(n), nil
}

// ProfilesForDevice returns the profiles that apply to a device (by
// sysObjectID prefix match, or an attach override; excluding a detach
// override), each with its metrics.
func (s *ProfileService) ProfilesForDevice(ctx context.Context, d models.Device) ([]ProfileWithMetrics, error) {
	var profiles []models.MetricProfile
	if err := s.db.WithContext(ctx).Order("name").Find(&profiles).Error; err != nil {
		return nil, fmt.Errorf("loading profiles: %w", err)
	}
	overrides, err := s.overridesFor(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	out := make([]ProfileWithMetrics, 0, len(profiles))
	for _, p := range profiles {
		mode, has := overrides[p.ID]
		if !appliesTo(p, d.SysObjectID, mode, has) {
			continue
		}
		metrics, err := s.metricsFor(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, ProfileWithMetrics{Profile: p, Metrics: metrics})
	}
	return out, nil
}

// DeviceProfiles returns every profile's standing with a device: whether its
// prefixes match, whether it actually applies once overrides are accounted
// for, the override mode, and its last poll run.
func (s *ProfileService) DeviceProfiles(ctx context.Context, d models.Device) ([]DeviceProfileView, error) {
	var profiles []models.MetricProfile
	if err := s.db.WithContext(ctx).Order("name").Find(&profiles).Error; err != nil {
		return nil, fmt.Errorf("loading profiles: %w", err)
	}
	overrides, err := s.overridesFor(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	runs, err := s.runsFor(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	out := make([]DeviceProfileView, 0, len(profiles))
	for _, p := range profiles {
		mode, has := overrides[p.ID]
		modeOut := "auto"
		if has {
			modeOut = mode
		}
		view := DeviceProfileView{
			Profile: p,
			Matched: MatchesPrefix(d.SysObjectID, p.MatchPrefixes),
			Applies: appliesTo(p, d.SysObjectID, mode, has),
			Mode:    modeOut,
		}
		if r, ok := runs[p.ID]; ok {
			rc := r
			view.LastRun = &rc
		}
		out = append(out, view)
	}
	return out, nil
}

// SetDeviceProfile sets a device's override for a profile: "auto" clears any
// override (falling back to prefix matching), "attach"/"detach" upsert one.
func (s *ProfileService) SetDeviceProfile(ctx context.Context, deviceID, profileID uuid.UUID, mode string) error {
	var err error
	switch mode {
	case "auto":
		err = s.db.WithContext(ctx).Exec(`DELETE FROM device_profile_overrides WHERE device_id = ? AND profile_id = ?`,
			deviceID, profileID).Error
	case models.ProfileOverrideAttach, models.ProfileOverrideDetach:
		err = s.db.WithContext(ctx).Exec(`INSERT INTO device_profile_overrides (device_id, profile_id, mode) VALUES (?, ?, ?)
			ON CONFLICT (device_id, profile_id) DO UPDATE SET mode = EXCLUDED.mode`, deviceID, profileID, mode).Error
	default:
		return errors.New("mode must be auto, attach or detach")
	}
	if err == nil {
		return nil
	}
	if isForeignKeyViolation(err) {
		return ErrProfileNotFound
	}
	return fmt.Errorf("setting device profile: %w", err)
}

// SaveRun upserts a device's most recent poll of a profile.
func (s *ProfileService) SaveRun(ctx context.Context, deviceID, profileID uuid.UUID, at time.Time, ok bool, errText string) error {
	err := s.db.WithContext(ctx).Exec(`INSERT INTO device_profile_runs (device_id, profile_id, ran_at, ok, error)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (device_id, profile_id) DO UPDATE SET ran_at = EXCLUDED.ran_at, ok = EXCLUDED.ok, error = EXCLUDED.error`,
		deviceID, profileID, at, ok, errText).Error
	if err != nil {
		return fmt.Errorf("saving device profile run: %w", err)
	}
	return nil
}
