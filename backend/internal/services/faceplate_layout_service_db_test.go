package services

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func es16XGGroups() []portmon.LayoutGroup {
	return []portmon.LayoutGroup{
		{First: 1, Last: 12, Kind: portmon.GroupKindSFP, Rows: 2, Numbering: portmon.NumberingAlternate},
		{First: 13, Last: 16, Kind: portmon.GroupKindRJ45, Rows: 1},
	}
}

// seedModelDevice adds a device whose SNMP-reported model is model.
func seedModelDevice(t *testing.T, db *gorm.DB, host, model string) uuid.UUID {
	t.Helper()
	s := seedDevice(t, db, "site-"+host, host)
	testdb.Exec(t, db, `UPDATE devices SET model = ? WHERE id = ?`, model, s.DeviceID)
	return s.DeviceID
}

func TestDBFaceplateLayoutSaveListDelete(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	seedModelDevice(t, db, "10.0.0.1", "UBNT-ES16XG")
	seedModelDevice(t, db, "10.0.0.2", "UBNT-ES16XG")
	svc := NewFaceplateLayoutService(db)
	testdb.Must(t, svc.Load(ctx))

	before, after, err := svc.Save(ctx, "UBNT-ES16XG", es16XGGroups(), admin, nil)
	if err != nil || before != nil || after == nil || after.ModelKey != "UBNT-ES16XG" {
		t.Fatalf("save: before %v after %+v err %v", before, after, err)
	}
	if svc.For("ubnt-es16xg") == nil {
		t.Fatal("For did not find the saved layout")
	}
	list, err := svc.List(ctx)
	testdb.Must(t, err)
	if len(list) == 0 || list[0].Source != "saved" || list[0].Devices != 2 || list[0].ID == nil || list[0].PreviewDeviceID == nil {
		t.Fatalf("list: %+v", list)
	}
	for _, row := range list[1:] {
		if portmon.NormalizeModel(row.Model) == "UBNT-ES16XG" {
			t.Errorf("built-in row shown although a saved layout overrides it: %+v", row)
		}
	}

	if _, err := svc.Delete(ctx, after.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if svc.For("UBNT-ES16XG") != nil {
		t.Error("For still finds a deleted layout")
	}
	list, err = svc.List(ctx)
	testdb.Must(t, err)
	if len(list) != 1 || list[0].Source != "builtin" || list[0].Devices != 2 || list[0].ID != nil {
		t.Errorf("after delete: %+v", list)
	}
	if _, err := svc.Delete(ctx, after.ID); !errors.Is(err, ErrFaceplateLayoutNotFound) {
		t.Errorf("second delete: %v", err)
	}
}

func TestDBFaceplateLayoutReplace(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	svc := NewFaceplateLayoutService(db)
	_, first, err := svc.Save(ctx, "USW-24", es16XGGroups(), admin, nil)
	testdb.Must(t, err)
	one := []portmon.LayoutGroup{{First: 1, Last: 16, Kind: portmon.GroupKindRJ45, Rows: 1}}
	before, after, err := svc.Save(ctx, " usw-24", one, admin, nil)
	testdb.Must(t, err)
	if before == nil || len(before.Groups) != 2 || after.ID != first.ID || len(after.Groups) != 1 {
		t.Errorf("replace: before %+v after %+v", before, after)
	}
	var n int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM faceplate_layouts`).Scan(&n).Error)
	if n != 1 {
		t.Errorf("%d rows, want 1", n)
	}
}

func TestDBFaceplateLayoutClearsDeviceSettings(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	dev := seedModelDevice(t, db, "10.0.0.3", "UBNT-ES16XG")
	testdb.Exec(t, db, `UPDATE devices SET faceplate_rows = 2, faceplate_port_style = 'rj45', faceplate_sfp_ports = '[1]' WHERE id = ?`, dev)
	svc := NewFaceplateLayoutService(db)

	bad := []portmon.LayoutGroup{{First: 1, Last: 4, Rows: 1}}
	if _, _, err := svc.Save(ctx, "UBNT-ES16XG", bad, admin, &dev); err == nil {
		t.Fatal("invalid groups saved")
	}
	var set int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM devices WHERE id = ? AND faceplate_rows IS NOT NULL`, dev).Scan(&set).Error)
	if set != 1 {
		t.Fatal("a refused save cleared the device's settings")
	}

	_, _, err := svc.Save(ctx, "UBNT-ES16XG", es16XGGroups(), admin, &dev)
	testdb.Must(t, err)
	var cleared int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM devices WHERE id = ? AND faceplate_rows IS NULL
		AND faceplate_port_style IS NULL AND faceplate_sfp_ports IS NULL`, dev).Scan(&cleared).Error)
	if cleared != 1 {
		t.Error("device settings not cleared")
	}
}

// A model typed with other case or spacing in Edit details still counts.
func TestDBFaceplateLayoutNormalizedModel(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	dev := seedModelDevice(t, db, "10.0.0.4", "something-else")
	testdb.Exec(t, db, `UPDATE devices SET model_override = ' ubnt-es16xg ' WHERE id = ?`, dev)
	n, err := NewFaceplateLayoutService(db).DeviceCount(ctx, "UBNT-ES16XG")
	testdb.Must(t, err)
	if n != 1 {
		t.Errorf("count %d, want 1", n)
	}
}

// Changing a device's model leaves the old model's layout in place.
func TestDBFaceplateLayoutModelChange(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	dev := seedModelDevice(t, db, "10.0.0.5", "A")
	svc := NewFaceplateLayoutService(db)
	_, _, err := svc.Save(ctx, "A", es16XGGroups(), admin, nil)
	testdb.Must(t, err)
	testdb.Exec(t, db, `UPDATE devices SET model = 'B' WHERE id = ?`, dev)
	n, err := svc.DeviceCount(ctx, "A")
	testdb.Must(t, err)
	if n != 0 || svc.For("A") == nil {
		t.Errorf("count %d (want 0), layout for A kept: %v", n, svc.For("A") != nil)
	}
}

func TestDBFaceplateLayoutNoModel(t *testing.T) {
	db := testdb.Open(t)
	admin := testdb.NewUser(t, db, true)
	if _, _, err := NewFaceplateLayoutService(db).Save(context.Background(), "  ", es16XGGroups(), admin, nil); !errors.Is(err, ErrFaceplateLayoutNoModel) {
		t.Errorf("blank model: %v", err)
	}
}

// Saves that overlap never fail on the unique model key, and the in-memory
// copy ends up matching what the database holds.
func TestDBFaceplateLayoutConcurrentSaves(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	svc := NewFaceplateLayoutService(db)
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func(n int) {
			_, _, err := svc.Save(ctx, "USW-RACE", []portmon.LayoutGroup{{First: 1, Last: 1 + n, Kind: portmon.GroupKindRJ45, Rows: 1}}, admin, nil)
			errs <- err
		}(i)
	}
	for i := 0; i < 16; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent save: %v", err)
		}
	}
	var last int
	testdb.Must(t, db.Raw(`SELECT (groups->0->>'last')::int FROM faceplate_layouts WHERE model_key = 'USW-RACE'`).Scan(&last).Error)
	if l := svc.For("USW-RACE"); l == nil || l.Groups[0].Last != last {
		t.Errorf("cache %+v, database last %d", l, last)
	}
}
