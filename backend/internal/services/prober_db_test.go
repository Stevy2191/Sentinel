package services

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// TargetFor applies the same network policy as IdentifyWith (both go through
// resolveAllowed): a blocked address is refused before anything is sent, and
// an allowed one builds the target at the device's resolved IP. This needs a
// real, decryptable credential profile (TargetFor decrypts before checking
// policy), so it runs as a DB test rather than against a bare Prober.
func TestDBProberTargetForAppliesNetworkPolicy(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	credSvc := NewSNMPCredentialService(db)
	cred, err := credSvc.Create(ctx, models.CredentialInput{Name: "walk-cred", Version: "2c", Community: str("public")}, uuid.Nil)
	testdb.Must(t, err)

	d := models.Device{CredentialID: cred.ID, Host: "metadata", Port: 161, TimeoutMs: 1000, Retries: 1}
	p := NewProber(credSvc, &fakeSNMP{})
	p.resolve = func(context.Context, string) (net.IP, error) { return net.ParseIP("169.254.169.254"), nil }
	p.blocked = func(net.IP) bool { return true }

	if _, err := p.TargetFor(ctx, d); !errors.Is(err, ErrTargetBlocked) {
		t.Fatalf("err %v, want ErrTargetBlocked", err)
	}

	p.blocked = func(net.IP) bool { return false }
	target, err := p.TargetFor(ctx, d)
	testdb.Must(t, err)
	if target.Host != "169.254.169.254" || target.Port != 161 || target.Credential.Community != "public" {
		t.Errorf("target %+v", target)
	}
}
