package services

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func rolePatch(role *string) models.PortPatch {
	return models.PortPatch{Role: models.Opt[string]{Set: true, Value: role}}
}

func TestDBPortPatchRoleSetsUser(t *testing.T) {
	db := testdb.Open(t)
	a, _ := rolesSite(t, db)
	svc := NewPortService(db, NewMetricsStore(db), NewIncidentService(db), NewSettingsService(db))
	access := models.PortRoleAccess
	_, _, err := svc.UpdatePort(context.Background(), a, 49, rolePatch(&access))
	testdb.Must(t, err)
	if got := roleOf(t, db, a, 49); got != [3]string{"access", "user", ""} {
		t.Errorf("after choosing Endpoint: %v, want access set by the user", got)
	}
}

func TestDBPortPatchRoleNullIsAutomatic(t *testing.T) {
	db := testdb.Open(t)
	a, _ := rolesSite(t, db)
	testdb.Exec(t, db, `UPDATE device_interfaces SET role = 'uplink', role_source = 'user' WHERE device_id = ? AND if_index = 49`, a)
	neighbour(t, db, a, 49, "B", "0/49")
	svc := NewPortService(db, NewMetricsStore(db), NewIncidentService(db), NewSettingsService(db))
	svc.SetRoles(NewPortRoleService(db))
	_, after, err := svc.UpdatePort(context.Background(), a, 49, rolePatch(nil))
	testdb.Must(t, err)
	want := [3]string{"uplink", "auto", "Found by LLDP: B port 49"}
	if got := roleOf(t, db, a, 49); got != want {
		t.Errorf("after choosing Automatic: %v, want %v", got, want)
	}
	if after.RoleSource != "auto" || after.Role != "uplink" {
		t.Errorf("returned port %s/%s, want the re-read uplink/auto", after.Role, after.RoleSource)
	}
}

// Review Focus 3: a far port pointed back is the user's; one let go is the
// rules' again.
func TestDBPortPatchFarPortSources(t *testing.T) {
	db := testdb.Open(t)
	a, b := rolesSite(t, db)
	svc := NewPortService(db, NewMetricsStore(db), NewIncidentService(db), NewSettingsService(db))
	ctx := context.Background()
	fifty := 50
	_, _, err := svc.UpdatePort(ctx, a, 49, models.PortPatch{
		NeighborDeviceID: models.Opt[uuid.UUID]{Set: true, Value: &b},
		NeighborIfIndex:  models.Opt[int]{Set: true, Value: &fifty},
	})
	testdb.Must(t, err)
	if got := roleOf(t, db, a, 49); got != [3]string{"uplink", "user", ""} {
		t.Errorf("A port 49 = %v, want uplink set by the user", got)
	}
	if got := roleOf(t, db, b, 50); got != [3]string{"uplink", "user", ""} {
		t.Errorf("B port 50 = %v, want uplink set by the user", got)
	}
	_, _, err = svc.UpdatePort(ctx, a, 49, models.PortPatch{NeighborDeviceID: models.Opt[uuid.UUID]{Set: true}})
	testdb.Must(t, err)
	if got := roleOf(t, db, b, 50); got != [3]string{"access", "default", ""} {
		t.Errorf("B port 50 after the link went = %v, want back to the default", got)
	}
}

func TestPortPatchRoleValidate(t *testing.T) {
	if err := rolePatch(nil).Validate(); err != nil {
		t.Errorf("role null: %v, want accepted (back to automatic)", err)
	}
	b := uuid.New()
	withNeighbour := rolePatch(nil)
	withNeighbour.NeighborDeviceID = models.Opt[uuid.UUID]{Set: true, Value: &b}
	if err := withNeighbour.Validate(); err == nil {
		t.Error("role null with a neighbour device was accepted")
	}
	bogus := "bogus"
	if err := rolePatch(&bogus).Validate(); err == nil || err.Error() != "role must be access, uplink or wan" {
		t.Errorf("role bogus: %v", err)
	}
}
