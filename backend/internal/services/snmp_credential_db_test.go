package services

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func str(s string) *string { return &s }

func TestDBCredentialSecretsEncrypted(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := NewSNMPCredentialService(db)

	view, err := svc.Create(ctx, models.CredentialInput{Name: "core-v3", Version: "3", Username: "mon",
		AuthProtocol: "SHA256", AuthPassword: str("auth-secret-1"), PrivProtocol: "AES", PrivPassword: str("priv-secret-1")}, uuid.Nil)
	testdb.Must(t, err)

	var raw models.SNMPCredential
	testdb.Must(t, db.First(&raw, "id = ?", view.ID).Error)
	if raw.AuthPassword == "" || raw.AuthPassword == "auth-secret-1" || raw.PrivPassword == "priv-secret-1" {
		t.Fatalf("secrets stored in plaintext or missing: %+v", raw)
	}
	plain, err := svc.Decrypted(ctx, view.ID)
	testdb.Must(t, err)
	if plain.AuthPassword != "auth-secret-1" || plain.PrivPassword != "priv-secret-1" || plain.Username != "mon" || plain.Version != "3" {
		t.Errorf("decrypted %+v", plain)
	}

	// A blank secret on update keeps the stored one.
	_, after, err := svc.Update(ctx, view.ID, models.CredentialInput{Name: "core-v3", Version: "3", Username: "mon2",
		AuthProtocol: "SHA256", PrivProtocol: "AES"})
	testdb.Must(t, err)
	plain, _ = svc.Decrypted(ctx, view.ID)
	if plain.AuthPassword != "auth-secret-1" || after.Username != "mon2" || !after.HasPrivPassword {
		t.Errorf("update lost a secret: %+v / %+v", plain, after)
	}

	// Switching to v2c clears every v3 field, secrets included.
	_, after, err = svc.Update(ctx, view.ID, models.CredentialInput{Name: "core-v3", Version: "2c", Community: str("public")})
	testdb.Must(t, err)
	testdb.Must(t, db.First(&raw, "id = ?", view.ID).Error)
	if raw.AuthPassword != "" || raw.PrivPassword != "" || raw.Username != "" || !after.HasCommunity || after.HasAuthPassword {
		t.Errorf("v3 fields survived the switch to v2c: %+v", raw)
	}
}

func TestDBCredentialLifecycle(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := NewSNMPCredentialService(db)
	s := seedDevice(t, db, "HQ", "10.0.0.2") // its credential is in use by the device

	if _, err := svc.Delete(ctx, s.CredentialID); !errors.Is(err, ErrCredentialInUse) {
		t.Errorf("deleting a credential in use: %v, want ErrCredentialInUse", err)
	}
	if _, err := svc.Create(ctx, models.CredentialInput{Name: "CRED-" + s.CredentialID.String()[:8], Version: "2c", Community: str("c")}, uuid.Nil); !errors.Is(err, ErrCredentialNameTaken) {
		t.Errorf("duplicate name (case-insensitive): %v, want ErrCredentialNameTaken", err)
	}

	other := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'Branch')`, other)
	branchOnly, err := svc.Create(ctx, models.CredentialInput{Name: "branch", SiteID: &other, Version: "1", Community: str("c")}, uuid.Nil)
	testdb.Must(t, err)

	opts, err := svc.ForSite(ctx, s.SiteID)
	testdb.Must(t, err)
	for _, o := range opts {
		if o.ID == branchOnly.ID {
			t.Error("HQ was offered Branch's site-scoped credential")
		}
	}
	if ok, _ := svc.UsableAt(ctx, branchOnly.ID, s.SiteID); ok {
		t.Error("UsableAt: branch-only credential usable at HQ")
	}
	if ok, _ := svc.UsableAt(ctx, s.CredentialID, other); !ok {
		t.Error("UsableAt: global credential not usable at Branch")
	}

	// Scoping a credential in use at HQ to Branch would strand HQ's device.
	if _, _, err := svc.Update(ctx, s.CredentialID, models.CredentialInput{Name: "x", SiteID: &other, Version: "2c"}); !errors.Is(err, ErrCredentialSiteMismatch) {
		t.Errorf("rescoping away from its devices: %v, want ErrCredentialSiteMismatch", err)
	}

	list, err := svc.List(ctx)
	testdb.Must(t, err)
	for _, v := range list {
		if v.ID == s.CredentialID && v.UsedBy != 1 {
			t.Errorf("used_by = %d, want 1", v.UsedBy)
		}
		if v.ID == branchOnly.ID && v.SiteName != "Branch" {
			t.Errorf("site_name = %q", v.SiteName)
		}
	}

	if _, err := svc.Delete(ctx, branchOnly.ID); err != nil {
		t.Errorf("deleting an unused credential: %v", err)
	}
}
