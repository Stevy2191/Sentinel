# UX Reorganization, Piece 3: Site Profiles — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Each site keeps its networks, ISPs and circuits (optionally tied to a device port, with live usage) and notes, shown on a two-column site page beside the site's devices, servers and uptime checks; the sites list shows what each site holds.

**Architecture:** Migration 061 adds `sites.notes` and the `site_networks` and `site_circuits` tables. Pure normalizers in `models/site_profile.go` validate input; `SiteProfileService` stores it and reads each linked port live through a new `PortService.LivePorts`; `api/site_profile_handler.go` serves `/sites/:id/profile`, `/notes`, `/networks`, `/circuits` with the existing site access levels and audit log. The frontend adds a profile hook, pure helpers, three profile cards with their forms, two compact lists, and rearranges `SiteDetail` into two columns.

**Tech Stack:** Go 1.26, Gin, GORM, PostgreSQL 16 (CIDR/INET types); React 18 + TypeScript + Vite + Tailwind + react-router-dom v6.

**Spec:** `docs/superpowers/specs/2026-10-08-ux-piece3-site-profiles-design.md`

## Global Constraints

- Work on `feature/ux-piece3` in a worktree cut from `dev`.
- Migration is `backend/migrations/061_site_profiles.sql`. Confirm first: `ls backend/migrations | tail -1` prints `060_monitor_agent_sites.sql`; otherwise use the next free number everywhere this plan says 061.
- No CHECK constraint ties two columns. A CHECK on one column is written `col IS NOT NULL AND col IN (...)`.
- Exact validation messages (400): `name is required`; `name must be 100 characters or fewer`; `subnet is required`; `subnet "<raw>" is not a network like 10.20.0.0/24`; `VLAN must be between 1 and 4094`; `gateway "<raw>" is not an IP address`; `<gateway> is outside <subnet>`; `note must be 500 characters or fewer`; `provider is required`; `provider must be 100 characters or fewer`; `type must be one of fiber, cable, dsl, fixed_wireless, cellular, copper, other`; `download speed must be more than 0 and at most 100000 Mbps` (and `upload …`); `circuit ID must be 100 characters or fewer`; `account number must be 100 characters or fewer`; `support phone must be 50 characters or fewer`; `notes must be 1000 characters or fewer` (circuit); `notes must be 10000 characters or fewer` (site); `<site name> already has <subnet>`; `that port is not on a device at this site`.
- Access: read the profile with site access readonly or above; change networks, circuits and notes with editable or above. A read-only sharer gets 403 `you need edit access to this site` (the existing `requireSiteLevel` wording); a user without access gets 404 `site not found`. A network or circuit id that belongs to another site is 404.
- Audit actions (resource type `site`, resource id the site's id): `site_notes_updated`, `site_network_created`, `site_network_updated`, `site_network_deleted`, `site_circuit_created`, `site_circuit_updated`, `site_circuit_deleted`. Account numbers, support phones and circuit notes are not written to the audit log.
- Networks are ordered by subnet (IPv4 before IPv6); circuits by provider (case-insensitive), then creation.
- A circuit's port counts **in as download, out as upload**. A port link whose device is no longer at the site is returned as `port: null`.
- Scan subnet offers saved networks that are IPv4 with a prefix of /22 to /32.
- The profile refreshes every 30 seconds; a late response never replaces a newer one, nor shows one site's profile on another site's page.
- Frontend rules: colours from existing slate/Tailwind classes (no new hex, no `dark:` variants); component files export only components (helpers in `src/utils/` or hooks); no `window.confirm`/`alert` (confirm on the page); modules checked by throwaway scripts use only `import type` from `@/…` and relative value imports.
- Backend commands from `backend/`: `go vet ./...`, `go test ./...`, `./scripts/test-db.sh -run <Name> -v` (database tests; needs Docker). The restore tests run pg_dump/psql through `docker exec` and need `SENTINEL_TEST_DB_CONTAINER`, which `./scripts/test-db.sh` sets.
- Frontend gate (repo root): `docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"`. Never run npm as root.
- Throwaway frontend check: `check.ts` in a temp dir outside the repo (`CHECK=$(mktemp -d)`), run from the repo root with `docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -v "$CHECK":/check -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx esbuild /check/check.ts --bundle --platform=node --outfile=/tmp/check.cjs --log-level=warning && node /tmp/check.cjs"`.
- Commits end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **A circuit tied to a port whose device moved to another site** shows no port, and saving that circuit's form (which then sends no port) must succeed rather than fail with "that port is not on a device at this site". Pinned by Task 2's `TestDBSiteProfileCarriesLivePort`.
2. **The same subnet typed two ways** (10.20.0.5/24 and 10.20.0.0/24) is one subnet and must be caught as a duplicate. Pinned by Task 2's `TestDBSiteNetworksOrderAndDuplicates`.
3. **Sensitive circuit fields must not leak into the audit log**, which admins browse and export. Pinned by Task 3's `TestDBSiteProfileAccess`.
4. **Deleting a site**: one with networks, circuits and notes but no devices is deleted with its profile; one with devices is still refused. Pinned by Task 2's `TestDBSiteProfileGoesWithTheSite`.
5. **Restoring a backup** taken with a profile in place keeps the networks, the circuit's port link and the notes. Pinned by Task 2's `TestDBRestoreKeepsSiteProfiles`.

## File structure

Backend:
- Create `backend/migrations/061_site_profiles.sql`.
- Create `backend/internal/models/site_profile.go` (+ `site_profile_test.go`).
- Create `backend/internal/services/port_live.go` (`LivePort`, `PortService.LivePorts`).
- Create `backend/internal/services/site_profile_service.go` (+ `site_profile_db_test.go`).
- Create `backend/internal/api/site_profile_handler.go` (+ `site_profile_db_test.go`).
- Modify `backend/internal/models/audit.go`, `backend/cmd/sentinel/main.go`.

Frontend:
- Create `frontend/src/hooks/useSiteProfile.ts`, `frontend/src/utils/siteProfile.ts`.
- Create `frontend/src/components/sites/{CircuitsCard,CircuitFormModal,NetworksCard,NetworkFormModal,NotesCard,SiteServersList,SiteChecksList}.tsx`.
- Modify `frontend/src/pages/network/SiteDetail.tsx`, `frontend/src/pages/network/Sites.tsx`, `frontend/src/components/network/ScanModal.tsx`, `docs/superpowers/STATUS.md`.

## Plan rulings

1. **Networks show as a compact list, not a wide table**: the left column is narrow, so each network is one row of name, then subnet · VLAN · gateway, then its note. Same fields as the spec's table.
2. **The circuit form seeds its port from the live port, not the stored id**, so a circuit whose port's device left the site opens with no port and saves cleanly (Review Focus 1).
3. **The duplicate-subnet check runs before the insert**; the unique index is the backstop, and a race between two saves of the same subnet surfaces as a 500 rather than the friendly message.
4. **Notes changes update `sites.updated_at`**; network and circuit changes do not.
5. **The circuit card shows "Port down" whenever the port's status is known and not `up`**; an unknown (empty) status shows the rates as usual.

---

### Task 1: Profile data and validation

**Files:**
- Create: `backend/migrations/061_site_profiles.sql`, `backend/internal/models/site_profile.go`
- Test: `backend/internal/models/site_profile_test.go`

**Interfaces:**
- Produces: `models.SiteNetwork` (table `site_networks`), `models.SiteNetworkInput`, `models.NormalizeSiteNetworkInput(SiteNetworkInput) (SiteNetworkInput, error)`; `models.SiteCircuit` (table `site_circuits`), `models.SiteCircuitInput`, `models.NormalizeSiteCircuitInput(SiteCircuitInput) (SiteCircuitInput, error)`, `models.CircuitKinds map[string]bool`; `models.NormalizeSiteNotes(string) (*string, error)`. Uses the package's existing `trimOptional(*string) *string`.

- [ ] **Step 1: Write the failing tests**

`backend/internal/models/site_profile_test.go`:

```go
package models

import (
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// strp is defined in site_test.go.
func intp(v int) *int         { return &v }
func f64p(v float64) *float64 { return &v }

func TestNormalizeSiteNetworkInput(t *testing.T) {
	cases := []struct {
		name string
		in   SiteNetworkInput
		want SiteNetworkInput
		err  string
	}{
		{"host address becomes the network",
			SiteNetworkInput{Name: " Staff ", CIDR: " 10.20.0.5/24 ", VLAN: intp(10), Gateway: strp(" 10.20.0.1 "), Note: strp(" front desk ")},
			SiteNetworkInput{Name: "Staff", CIDR: "10.20.0.0/24", VLAN: intp(10), Gateway: strp("10.20.0.1"), Note: strp("front desk")}, ""},
		{"ipv6", SiteNetworkInput{Name: "v6", CIDR: "2001:db8::1/64"}, SiteNetworkInput{Name: "v6", CIDR: "2001:db8::/64"}, ""},
		{"blank optionals are nil", SiteNetworkInput{Name: "x", CIDR: "10.0.0.0/8", Gateway: strp("  "), Note: strp("")},
			SiteNetworkInput{Name: "x", CIDR: "10.0.0.0/8"}, ""},
		{"name required", SiteNetworkInput{Name: " ", CIDR: "10.0.0.0/8"}, SiteNetworkInput{}, "name is required"},
		{"name too long", SiteNetworkInput{Name: strings.Repeat("n", 101), CIDR: "10.0.0.0/8"}, SiteNetworkInput{}, "name must be 100 characters or fewer"},
		{"subnet required", SiteNetworkInput{Name: "x"}, SiteNetworkInput{}, "subnet is required"},
		{"not a subnet", SiteNetworkInput{Name: "x", CIDR: "10.20.0.0"}, SiteNetworkInput{}, `subnet "10.20.0.0" is not a network like 10.20.0.0/24`},
		{"vlan zero", SiteNetworkInput{Name: "x", CIDR: "10.0.0.0/8", VLAN: intp(0)}, SiteNetworkInput{}, "VLAN must be between 1 and 4094"},
		{"vlan too big", SiteNetworkInput{Name: "x", CIDR: "10.0.0.0/8", VLAN: intp(4095)}, SiteNetworkInput{}, "VLAN must be between 1 and 4094"},
		{"gateway not an address", SiteNetworkInput{Name: "x", CIDR: "10.0.0.0/8", Gateway: strp("router")}, SiteNetworkInput{}, `gateway "router" is not an IP address`},
		{"gateway outside", SiteNetworkInput{Name: "x", CIDR: "10.20.0.0/24", Gateway: strp("10.30.0.1")}, SiteNetworkInput{}, "10.30.0.1 is outside 10.20.0.0/24"},
		{"gateway other family", SiteNetworkInput{Name: "x", CIDR: "10.20.0.0/24", Gateway: strp("2001:db8::1")}, SiteNetworkInput{}, "2001:db8::1 is outside 10.20.0.0/24"},
		{"note too long", SiteNetworkInput{Name: "x", CIDR: "10.0.0.0/8", Note: strp(strings.Repeat("a", 501))}, SiteNetworkInput{}, "note must be 500 characters or fewer"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := NormalizeSiteNetworkInput(c.in)
			if c.err != "" {
				if err == nil || err.Error() != c.err {
					t.Fatalf("err = %v, want %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestNormalizeSiteCircuitInput(t *testing.T) {
	port := uuid.New()
	cases := []struct {
		name string
		in   SiteCircuitInput
		want SiteCircuitInput
		err  string
	}{
		{"trimmed, kind lowered, decimals kept",
			SiteCircuitInput{Provider: " Spectrum ", Kind: " Fiber ", DownloadMbps: f64p(500), UploadMbps: f64p(1.5),
				CircuitRef: strp(" 12/KFGN/0391 "), SupportPhone: strp(" 1-800-892-4357 "), AccountNumber: strp(" 8347 "), Notes: strp(" static IP "), InterfaceID: &port},
			SiteCircuitInput{Provider: "Spectrum", Kind: "fiber", DownloadMbps: f64p(500), UploadMbps: f64p(1.5),
				CircuitRef: strp("12/KFGN/0391"), SupportPhone: strp("1-800-892-4357"), AccountNumber: strp("8347"), Notes: strp("static IP"), InterfaceID: &port}, ""},
		{"blank kind is other", SiteCircuitInput{Provider: "AT&T"}, SiteCircuitInput{Provider: "AT&T", Kind: "other"}, ""},
		{"provider required", SiteCircuitInput{Provider: "  "}, SiteCircuitInput{}, "provider is required"},
		{"provider too long", SiteCircuitInput{Provider: strings.Repeat("p", 101)}, SiteCircuitInput{}, "provider must be 100 characters or fewer"},
		{"unknown kind", SiteCircuitInput{Provider: "x", Kind: "satellite"}, SiteCircuitInput{}, "type must be one of fiber, cable, dsl, fixed_wireless, cellular, copper, other"},
		{"zero download", SiteCircuitInput{Provider: "x", DownloadMbps: f64p(0)}, SiteCircuitInput{}, "download speed must be more than 0 and at most 100000 Mbps"},
		{"huge upload", SiteCircuitInput{Provider: "x", UploadMbps: f64p(100001)}, SiteCircuitInput{}, "upload speed must be more than 0 and at most 100000 Mbps"},
		{"circuit id too long", SiteCircuitInput{Provider: "x", CircuitRef: strp(strings.Repeat("c", 101))}, SiteCircuitInput{}, "circuit ID must be 100 characters or fewer"},
		{"account too long", SiteCircuitInput{Provider: "x", AccountNumber: strp(strings.Repeat("a", 101))}, SiteCircuitInput{}, "account number must be 100 characters or fewer"},
		{"phone too long", SiteCircuitInput{Provider: "x", SupportPhone: strp(strings.Repeat("9", 51))}, SiteCircuitInput{}, "support phone must be 50 characters or fewer"},
		{"notes too long", SiteCircuitInput{Provider: "x", Notes: strp(strings.Repeat("n", 1001))}, SiteCircuitInput{}, "notes must be 1000 characters or fewer"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := NormalizeSiteCircuitInput(c.in)
			if c.err != "" {
				if err == nil || err.Error() != c.err {
					t.Fatalf("err = %v, want %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestNormalizeSiteNotes(t *testing.T) {
	got, err := NormalizeSiteNotes("  Closet: room 104\nKey at the front desk  ")
	if err != nil || got == nil || *got != "Closet: room 104\nKey at the front desk" {
		t.Errorf("got %v, %v", got, err)
	}
	if got, err := NormalizeSiteNotes(" \n "); err != nil || got != nil {
		t.Errorf("blank: got %v, %v; want nil", got, err)
	}
	if _, err := NormalizeSiteNotes(strings.Repeat("n", 10001)); err == nil || err.Error() != "notes must be 10000 characters or fewer" {
		t.Errorf("long: err = %v", err)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/models -run 'TestNormalizeSite(Network|Circuit)Input|TestNormalizeSiteNotes'`
Expected: build failure — `undefined: SiteNetworkInput`.

- [ ] **Step 3: Write the migration**

`backend/migrations/061_site_profiles.sql`:

```sql
-- 061_site_profiles.sql
-- UX piece 3 (spec 2026-10-08-ux-piece3-site-profiles-design.md): each site
-- keeps its networks, its ISPs and circuits, and free-form notes.

ALTER TABLE sites ADD COLUMN IF NOT EXISTS notes TEXT;

-- One subnet at a site. The unique key also serves lookups by site.
CREATE TABLE IF NOT EXISTS site_networks (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    site_id    UUID NOT NULL REFERENCES sites (id) ON DELETE CASCADE,
    name       VARCHAR(100) NOT NULL,
    cidr       CIDR NOT NULL,
    vlan       INTEGER,
    gateway    INET,
    note       VARCHAR(500),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (site_id, cidr)
);

-- One ISP circuit at a site, optionally tied to the device port it plugs
-- into. Deleting that port (or its device) clears the link, not the circuit.
CREATE TABLE IF NOT EXISTS site_circuits (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    site_id        UUID NOT NULL REFERENCES sites (id) ON DELETE CASCADE,
    provider       VARCHAR(100) NOT NULL,
    circuit_ref    VARCHAR(100),
    kind           VARCHAR(20) NOT NULL DEFAULT 'other'
        CHECK (kind IS NOT NULL AND kind IN ('fiber', 'cable', 'dsl', 'fixed_wireless', 'cellular', 'copper', 'other')),
    download_mbps  DOUBLE PRECISION,
    upload_mbps    DOUBLE PRECISION,
    support_phone  VARCHAR(50),
    account_number VARCHAR(100),
    notes          VARCHAR(1000),
    interface_id   UUID REFERENCES device_interfaces (id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_site_circuits_site_id ON site_circuits (site_id);
CREATE INDEX IF NOT EXISTS idx_site_circuits_interface_id ON site_circuits (interface_id);
```

- [ ] **Step 4: Write the models**

`backend/internal/models/site_profile.go`:

```go
package models

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Limits on a site profile's fields; the migration's column sizes match.
const (
	maxNetworkNameLen   = 100
	maxNetworkNoteLen   = 500
	maxProviderLen      = 100
	maxCircuitRefLen    = 100
	maxAccountNumberLen = 100
	maxSupportPhoneLen  = 50
	maxCircuitNotesLen  = 1000
	maxSiteNotesLen     = 10000
	maxCircuitMbps      = 100000
)

// SiteNetwork is one subnet at a site: what it is for, its VLAN and gateway.
type SiteNetwork struct {
	ID        uuid.UUID `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	SiteID    uuid.UUID `json:"site_id" gorm:"column:site_id;type:uuid;not null"`
	Name      string    `json:"name" gorm:"column:name;not null"`
	CIDR      string    `json:"cidr" gorm:"column:cidr;type:cidr;not null"`
	VLAN      *int      `json:"vlan" gorm:"column:vlan"`
	Gateway   *string   `json:"gateway" gorm:"column:gateway;type:inet"`
	Note      *string   `json:"note" gorm:"column:note"`
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName pins the table.
func (SiteNetwork) TableName() string { return "site_networks" }

// SiteNetworkInput is a network as a client sends it.
type SiteNetworkInput struct {
	Name    string  `json:"name"`
	CIDR    string  `json:"cidr"`
	VLAN    *int    `json:"vlan"`
	Gateway *string `json:"gateway"`
	Note    *string `json:"note"`
}

// NormalizeSiteNetworkInput trims and checks a network. The subnet is stored
// as its network address, so 10.20.0.5/24 becomes 10.20.0.0/24 and the two
// spellings collide as the duplicate they are; a gateway must be an address
// inside the subnet.
func NormalizeSiteNetworkInput(in SiteNetworkInput) (SiteNetworkInput, error) {
	out := SiteNetworkInput{Name: strings.TrimSpace(in.Name), VLAN: in.VLAN}
	if out.Name == "" {
		return SiteNetworkInput{}, errors.New("name is required")
	}
	if utf8.RuneCountInString(out.Name) > maxNetworkNameLen {
		return SiteNetworkInput{}, fmt.Errorf("name must be %d characters or fewer", maxNetworkNameLen)
	}
	raw := strings.TrimSpace(in.CIDR)
	if raw == "" {
		return SiteNetworkInput{}, errors.New("subnet is required")
	}
	_, subnet, err := net.ParseCIDR(raw)
	if err != nil {
		return SiteNetworkInput{}, fmt.Errorf("subnet %q is not a network like 10.20.0.0/24", raw)
	}
	out.CIDR = subnet.String()
	if out.VLAN != nil && (*out.VLAN < 1 || *out.VLAN > 4094) {
		return SiteNetworkInput{}, errors.New("VLAN must be between 1 and 4094")
	}
	if gw := trimOptional(in.Gateway); gw != nil {
		ip := net.ParseIP(*gw)
		if ip == nil {
			return SiteNetworkInput{}, fmt.Errorf("gateway %q is not an IP address", *gw)
		}
		if !subnet.Contains(ip) {
			return SiteNetworkInput{}, fmt.Errorf("%s is outside %s", ip, out.CIDR)
		}
		s := ip.String()
		out.Gateway = &s
	}
	if out.Note, err = optionalText(in.Note, maxNetworkNoteLen, "note"); err != nil {
		return SiteNetworkInput{}, err
	}
	return out, nil
}

// CircuitKinds are the kinds of circuit a site can record.
var CircuitKinds = map[string]bool{
	"fiber": true, "cable": true, "dsl": true, "fixed_wireless": true, "cellular": true, "copper": true, "other": true,
}

// SiteCircuit is one ISP circuit at a site, optionally tied to the device
// port it plugs into.
type SiteCircuit struct {
	ID            uuid.UUID  `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	SiteID        uuid.UUID  `json:"site_id" gorm:"column:site_id;type:uuid;not null"`
	Provider      string     `json:"provider" gorm:"column:provider;not null"`
	CircuitRef    *string    `json:"circuit_ref" gorm:"column:circuit_ref"`
	Kind          string     `json:"kind" gorm:"column:kind;not null"`
	DownloadMbps  *float64   `json:"download_mbps" gorm:"column:download_mbps"`
	UploadMbps    *float64   `json:"upload_mbps" gorm:"column:upload_mbps"`
	SupportPhone  *string    `json:"support_phone" gorm:"column:support_phone"`
	AccountNumber *string    `json:"account_number" gorm:"column:account_number"`
	Notes         *string    `json:"notes" gorm:"column:notes"`
	InterfaceID   *uuid.UUID `json:"interface_id" gorm:"column:interface_id;type:uuid"`
	CreatedAt     time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt     time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName pins the table.
func (SiteCircuit) TableName() string { return "site_circuits" }

// SiteCircuitInput is a circuit as a client sends it.
type SiteCircuitInput struct {
	Provider      string     `json:"provider"`
	CircuitRef    *string    `json:"circuit_ref"`
	Kind          string     `json:"kind"`
	DownloadMbps  *float64   `json:"download_mbps"`
	UploadMbps    *float64   `json:"upload_mbps"`
	SupportPhone  *string    `json:"support_phone"`
	AccountNumber *string    `json:"account_number"`
	Notes         *string    `json:"notes"`
	InterfaceID   *uuid.UUID `json:"interface_id"`
}

// NormalizeSiteCircuitInput trims and checks a circuit. A blank type means
// "other". Whether the port is at the site is checked by the service, which
// can see the database.
func NormalizeSiteCircuitInput(in SiteCircuitInput) (SiteCircuitInput, error) {
	out := SiteCircuitInput{Provider: strings.TrimSpace(in.Provider), InterfaceID: in.InterfaceID,
		DownloadMbps: in.DownloadMbps, UploadMbps: in.UploadMbps}
	if out.Provider == "" {
		return SiteCircuitInput{}, errors.New("provider is required")
	}
	if utf8.RuneCountInString(out.Provider) > maxProviderLen {
		return SiteCircuitInput{}, fmt.Errorf("provider must be %d characters or fewer", maxProviderLen)
	}
	out.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	if out.Kind == "" {
		out.Kind = "other"
	}
	if !CircuitKinds[out.Kind] {
		return SiteCircuitInput{}, errors.New("type must be one of fiber, cable, dsl, fixed_wireless, cellular, copper, other")
	}
	for _, sp := range []struct {
		label string
		v     *float64
	}{{"download", out.DownloadMbps}, {"upload", out.UploadMbps}} {
		if sp.v != nil && !(*sp.v > 0 && *sp.v <= maxCircuitMbps) {
			return SiteCircuitInput{}, fmt.Errorf("%s speed must be more than 0 and at most %d Mbps", sp.label, maxCircuitMbps)
		}
	}
	var err error
	if out.CircuitRef, err = optionalText(in.CircuitRef, maxCircuitRefLen, "circuit ID"); err != nil {
		return SiteCircuitInput{}, err
	}
	if out.SupportPhone, err = optionalText(in.SupportPhone, maxSupportPhoneLen, "support phone"); err != nil {
		return SiteCircuitInput{}, err
	}
	if out.AccountNumber, err = optionalText(in.AccountNumber, maxAccountNumberLen, "account number"); err != nil {
		return SiteCircuitInput{}, err
	}
	if out.Notes, err = optionalText(in.Notes, maxCircuitNotesLen, "notes"); err != nil {
		return SiteCircuitInput{}, err
	}
	return out, nil
}

// NormalizeSiteNotes trims a site's notes; blank clears them. Line breaks
// inside are kept.
func NormalizeSiteNotes(s string) (*string, error) {
	return optionalText(&s, maxSiteNotesLen, "notes")
}

// optionalText trims an optional field (blank becomes nil) and checks its
// length; label names the field in the error.
func optionalText(s *string, max int, label string) (*string, error) {
	t := trimOptional(s)
	if t != nil && utf8.RuneCountInString(*t) > max {
		return nil, fmt.Errorf("%s must be %d characters or fewer", label, max)
	}
	return t, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/models -run 'TestNormalizeSite(Network|Circuit)Input|TestNormalizeSiteNotes' -v`
Expected: PASS (all subtests). Then `go vet ./... && go test ./...` — PASS. (The migration is exercised by Task 2's database tests.)

- [ ] **Step 6: Commit**

```bash
git add backend/migrations/061_site_profiles.sql backend/internal/models/site_profile.go backend/internal/models/site_profile_test.go
git commit -m "feat(sites): site profile tables and validation

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Site profile service and live ports

**Files:**
- Create: `backend/internal/services/port_live.go`, `backend/internal/services/site_profile_service.go`
- Test: `backend/internal/services/site_profile_db_test.go`

**Interfaces:**
- Consumes: Task 1's models and normalizers; existing `PortService` internals `s.db`, `s.metrics.LatestMany(ctx, []uuid.UUID, []string, time.Time) (map[uuid.UUID]map[string]map[string]float64, error)`, `liveMetrics`, `liveSince(time.Time, int) time.Time`, `toPortView(models.DeviceInterface, map[string]map[string]float64) PortView`; `ErrSiteNotFound`; test helpers `seedDevice(t, db, siteName, host) seeded` (fields `SiteID`, `DeviceID`), `seedPort(t, db, deviceID, ifIndex, name, alias) uuid.UUID`, `NewMetricsStore`, `MetricsStore.Write`, `SamplePoint`, `MetricIfInBps`, `MetricIfOutBps`; `BackupService.dumpArgs()`/`restoreArgs()`.
- Produces (Task 3 uses): `type LivePort struct{ InterfaceID, DeviceID uuid.UUID; DeviceName string; SiteID uuid.UUID (json "-"); IfIndex, Number int; Label, Name, Alias string; StackUnit int; OperStatus string; InBps, OutBps *float64 }`; `func (s *PortService) LivePorts(ctx, []uuid.UUID) (map[uuid.UUID]LivePort, error)`; `NewSiteProfileService(db *gorm.DB, ports *PortService) *SiteProfileService`; `SiteProfile{ Notes *string; Networks []models.SiteNetwork; Circuits []SiteCircuitView }`; `SiteCircuitView{ models.SiteCircuit; Port *LivePort }`; methods `Profile(ctx, siteID) (*SiteProfile, error)`, `SetNotes(ctx, siteID, *string) (before, after *string, err error)`, `CreateNetwork(ctx, siteID, models.SiteNetworkInput) (*models.SiteNetwork, error)`, `UpdateNetwork(ctx, siteID, networkID, models.SiteNetworkInput) (before, after *models.SiteNetwork, err error)`, `DeleteNetwork(ctx, siteID, networkID) (*models.SiteNetwork, error)`, `CreateCircuit`, `UpdateCircuit`, `DeleteCircuit` (same shapes with `models.SiteCircuit`/`models.SiteCircuitInput`); errors `ErrSiteNetworkNotFound`, `ErrSiteCircuitNotFound`, `ErrCircuitPortNotAtSite`, `*SubnetTakenError{Site, CIDR string}`.

- [ ] **Step 1: Write the failing database tests**

`backend/internal/services/site_profile_db_test.go`:

```go
package services

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func profileFixture(t *testing.T) (*gorm.DB, *SiteProfileService, *MetricsStore) {
	t.Helper()
	db := testdb.Open(t)
	metrics := NewMetricsStore(db)
	ports := NewPortService(db, metrics, NewIncidentService(db), NewSettingsService(db))
	return db, NewSiteProfileService(db, ports), metrics
}

func newProfileSite(t *testing.T, db *gorm.DB, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, ?)`, id, name)
	return id
}

func network(t *testing.T, name, cidr string) models.SiteNetworkInput {
	t.Helper()
	in, err := models.NormalizeSiteNetworkInput(models.SiteNetworkInput{Name: name, CIDR: cidr})
	testdb.Must(t, err)
	return in
}

func circuit(t *testing.T, in models.SiteCircuitInput) models.SiteCircuitInput {
	t.Helper()
	out, err := models.NormalizeSiteCircuitInput(in)
	testdb.Must(t, err)
	return out
}

// Networks come back by subnet with IPv4 first; the same subnet typed as a
// host address is still a duplicate; another site may reuse it; an edit may
// keep its own subnet but not take another network's; another site's id is
// not found.
func TestDBSiteNetworksOrderAndDuplicates(t *testing.T) {
	db, svc, _ := profileFixture(t)
	ctx := context.Background()
	court, annex := newProfileSite(t, db, "Courthouse"), newProfileSite(t, db, "Annex")
	for _, cidr := range []string{"2001:db8::/64", "10.20.8.0/24", "10.20.0.0/24"} {
		_, err := svc.CreateNetwork(ctx, court, network(t, cidr, cidr))
		testdb.Must(t, err)
	}
	p, err := svc.Profile(ctx, court)
	testdb.Must(t, err)
	var got []string
	for _, n := range p.Networks {
		got = append(got, n.CIDR)
	}
	if len(got) != 3 || got[0] != "10.20.0.0/24" || got[1] != "10.20.8.0/24" || got[2] != "2001:db8::/64" {
		t.Fatalf("order = %v", got)
	}

	_, err = svc.CreateNetwork(ctx, court, network(t, "Again", "10.20.0.5/24"))
	var taken *SubnetTakenError
	if !errors.As(err, &taken) || err.Error() != "Courthouse already has 10.20.0.0/24" {
		t.Errorf("duplicate typed as a host address: err = %v", err)
	}
	if _, err := svc.CreateNetwork(ctx, annex, network(t, "Staff", "10.20.0.0/24")); err != nil {
		t.Errorf("another site reusing the subnet: %v", err)
	}

	first := p.Networks[0]
	if _, _, err := svc.UpdateNetwork(ctx, court, first.ID, network(t, "Staff", "10.20.0.0/24")); err != nil {
		t.Errorf("keeping its own subnet: %v", err)
	}
	if _, _, err := svc.UpdateNetwork(ctx, court, first.ID, network(t, "Staff", "10.20.8.0/24")); !errors.As(err, &taken) {
		t.Errorf("taking another network's subnet: err = %v", err)
	}
	if _, _, err := svc.UpdateNetwork(ctx, annex, first.ID, network(t, "Staff", "10.30.0.0/24")); !errors.Is(err, ErrSiteNetworkNotFound) {
		t.Errorf("another site's network id: err = %v", err)
	}
	if _, err := svc.DeleteNetwork(ctx, annex, first.ID); !errors.Is(err, ErrSiteNetworkNotFound) {
		t.Errorf("deleting another site's network: err = %v", err)
	}
}

// A circuit can only be tied to a port of a device at the same site, and
// another site's circuit id is not found.
func TestDBSiteCircuitPortMustBeAtSite(t *testing.T) {
	db, svc, _ := profileFixture(t)
	ctx := context.Background()
	s := seedDevice(t, db, "Courthouse", "10.0.0.2")
	port := seedPort(t, db, s.DeviceID, 1, "ge-0/0/1", "WAN")
	annex := newProfileSite(t, db, "Annex")

	if _, err := svc.CreateCircuit(ctx, annex, circuit(t, models.SiteCircuitInput{Provider: "Spectrum", InterfaceID: &port})); !errors.Is(err, ErrCircuitPortNotAtSite) {
		t.Errorf("port at another site: err = %v", err)
	}
	c, err := svc.CreateCircuit(ctx, s.SiteID, circuit(t, models.SiteCircuitInput{Provider: "Spectrum", InterfaceID: &port}))
	testdb.Must(t, err)
	if c.InterfaceID == nil || *c.InterfaceID != port || c.Kind != "other" {
		t.Errorf("created %+v", c)
	}
	if _, _, err := svc.UpdateCircuit(ctx, annex, c.ID, circuit(t, models.SiteCircuitInput{Provider: "Spectrum"})); !errors.Is(err, ErrSiteCircuitNotFound) {
		t.Errorf("another site's circuit id: err = %v", err)
	}
	if _, err := svc.DeleteCircuit(ctx, annex, c.ID); !errors.Is(err, ErrSiteCircuitNotFound) {
		t.Errorf("deleting another site's circuit: err = %v", err)
	}
}

// Deleting the port clears the circuit's link; the circuit stays.
func TestDBSiteCircuitLinkClearsWhenPortDeleted(t *testing.T) {
	db, svc, _ := profileFixture(t)
	ctx := context.Background()
	s := seedDevice(t, db, "Courthouse", "10.0.0.2")
	port := seedPort(t, db, s.DeviceID, 1, "ge-0/0/1", "WAN")
	_, err := svc.CreateCircuit(ctx, s.SiteID, circuit(t, models.SiteCircuitInput{Provider: "Spectrum", InterfaceID: &port}))
	testdb.Must(t, err)

	testdb.Exec(t, db, `DELETE FROM device_interfaces WHERE id = ?`, port)

	p, err := svc.Profile(ctx, s.SiteID)
	testdb.Must(t, err)
	if len(p.Circuits) != 1 || p.Circuits[0].InterfaceID != nil || p.Circuits[0].Port != nil {
		t.Errorf("after the port was deleted: %+v", p.Circuits)
	}
}

// The profile carries the linked port's device, status and current rates. A
// port whose device moved to another site is not shown, and the circuit can
// then be saved without a port (what the form sends).
func TestDBSiteProfileCarriesLivePort(t *testing.T) {
	db, svc, metrics := profileFixture(t)
	ctx := context.Background()
	s := seedDevice(t, db, "Courthouse", "10.0.0.2")
	port := seedPort(t, db, s.DeviceID, 7, "ge-0/0/7", "WAN")
	testdb.Exec(t, db, `UPDATE device_interfaces SET oper_status = 'up' WHERE id = ?`, port)
	testdb.Must(t, metrics.Write(ctx, s.DeviceID, time.Now().UTC(), []SamplePoint{
		{Metric: MetricIfInBps, Instance: "7", Value: 2.12e8},
		{Metric: MetricIfOutBps, Instance: "7", Value: 3e7},
	}))
	down := 500.0
	c, err := svc.CreateCircuit(ctx, s.SiteID, circuit(t, models.SiteCircuitInput{Provider: "Spectrum", Kind: "fiber", DownloadMbps: &down, InterfaceID: &port}))
	testdb.Must(t, err)

	p, err := svc.Profile(ctx, s.SiteID)
	testdb.Must(t, err)
	lp := p.Circuits[0].Port
	if lp == nil || lp.DeviceID != s.DeviceID || lp.DeviceName != "dev-10.0.0.2" || lp.IfIndex != 7 || lp.Alias != "WAN" ||
		lp.OperStatus != "up" || lp.InBps == nil || *lp.InBps != 2.12e8 || lp.OutBps == nil || *lp.OutBps != 3e7 {
		t.Fatalf("port = %+v", lp)
	}

	elsewhere := newProfileSite(t, db, "Annex")
	testdb.Exec(t, db, `UPDATE devices SET site_id = ? WHERE id = ?`, elsewhere, s.DeviceID)
	p, err = svc.Profile(ctx, s.SiteID)
	testdb.Must(t, err)
	if p.Circuits[0].Port != nil {
		t.Errorf("port of a device now at another site: %+v", p.Circuits[0].Port)
	}
	if _, _, err := svc.UpdateCircuit(ctx, s.SiteID, c.ID, circuit(t, models.SiteCircuitInput{Provider: "Spectrum", Kind: "fiber"})); err != nil {
		t.Errorf("saving the circuit without its moved port: %v", err)
	}
}

func TestDBSiteNotesSetAndClear(t *testing.T) {
	db, svc, _ := profileFixture(t)
	ctx := context.Background()
	site := newProfileSite(t, db, "Courthouse")
	text := "Closet: room 104\nKey at the front desk"

	before, after, err := svc.SetNotes(ctx, site, &text)
	testdb.Must(t, err)
	if before != nil || after == nil || *after != text {
		t.Errorf("set: before %v after %v", before, after)
	}
	p, err := svc.Profile(ctx, site)
	testdb.Must(t, err)
	if p.Notes == nil || *p.Notes != text {
		t.Errorf("profile notes = %v", p.Notes)
	}

	before, _, err = svc.SetNotes(ctx, site, nil)
	testdb.Must(t, err)
	if before == nil || *before != text {
		t.Errorf("clear: before %v", before)
	}
	if p, _ = svc.Profile(ctx, site); p.Notes != nil {
		t.Errorf("after clearing: %v", p.Notes)
	}
	if _, _, err := svc.SetNotes(ctx, uuid.New(), nil); !errors.Is(err, ErrSiteNotFound) {
		t.Errorf("unknown site: err = %v", err)
	}
}

// A site with a profile but no devices is deleted with its profile; one with
// devices is still refused.
func TestDBSiteProfileGoesWithTheSite(t *testing.T) {
	db, svc, _ := profileFixture(t)
	ctx := context.Background()
	site := newProfileSite(t, db, "Annex")
	text := "notes"
	_, err := svc.CreateNetwork(ctx, site, network(t, "Staff", "10.20.0.0/24"))
	testdb.Must(t, err)
	_, err = svc.CreateCircuit(ctx, site, circuit(t, models.SiteCircuitInput{Provider: "Spectrum"}))
	testdb.Must(t, err)
	_, _, err = svc.SetNotes(ctx, site, &text)
	testdb.Must(t, err)

	if _, err := NewSiteService(db).Delete(ctx, site); err != nil {
		t.Fatalf("deleting a site with only a profile: %v", err)
	}
	var left int64
	testdb.Must(t, db.Raw(`SELECT (SELECT count(*) FROM site_networks WHERE site_id = ?) + (SELECT count(*) FROM site_circuits WHERE site_id = ?)`, site, site).Scan(&left).Error)
	if left != 0 {
		t.Errorf("%d profile rows outlived their site", left)
	}

	s := seedDevice(t, db, "Courthouse", "10.0.0.2")
	_, err = svc.CreateNetwork(ctx, s.SiteID, network(t, "Staff", "10.20.0.0/24"))
	testdb.Must(t, err)
	if _, err := NewSiteService(db).Delete(ctx, s.SiteID); !errors.Is(err, ErrSiteNotEmpty) {
		t.Errorf("site with a device: err = %v, want ErrSiteNotEmpty", err)
	}
}

// A backup restored over a database holding a site profile keeps its
// networks, its circuit's port link and its notes. Runs pg_dump and psql
// inside the test container with the backup's own arguments.
func TestDBRestoreKeepsSiteProfiles(t *testing.T) {
	container := os.Getenv("SENTINEL_TEST_DB_CONTAINER")
	if container == "" {
		t.Skip("SENTINEL_TEST_DB_CONTAINER not set; run `make test-db`")
	}
	db, svc, _ := profileFixture(t)
	ctx := context.Background()
	s := seedDevice(t, db, "Courthouse", "10.0.0.2")
	port := seedPort(t, db, s.DeviceID, 1, "ge-0/0/1", "WAN")
	text := "Closet: room 104"
	_, err := svc.CreateNetwork(ctx, s.SiteID, network(t, "Staff", "10.20.0.0/24"))
	testdb.Must(t, err)
	c, err := svc.CreateCircuit(ctx, s.SiteID, circuit(t, models.SiteCircuitInput{Provider: "Spectrum", InterfaceID: &port}))
	testdb.Must(t, err)
	_, _, err = svc.SetNotes(ctx, s.SiteID, &text)
	testdb.Must(t, err)

	var name string
	testdb.Must(t, db.Raw("SELECT current_database()").Scan(&name).Error)
	b := &BackupService{db: DBConfig{Host: "127.0.0.1", Port: "5432", User: "sentinel", Name: name}}
	dump, err := exec.Command("docker", append([]string{"exec", "-e", "PGPASSWORD=test", container, "pg_dump"}, b.dumpArgs()...)...).Output()
	if err != nil {
		t.Fatalf("pg_dump: %v", err)
	}
	restore := exec.Command("docker", append([]string{"exec", "-i", "-e", "PGPASSWORD=test", container, "psql"}, b.restoreArgs()...)...)
	restore.Stdin = bytes.NewReader(dump)
	if out, err := restore.CombinedOutput(); err != nil {
		t.Fatalf("restore failed: %v\n%s", err, out)
	}

	p, err := svc.Profile(ctx, s.SiteID)
	testdb.Must(t, err)
	if len(p.Networks) != 1 || p.Networks[0].CIDR != "10.20.0.0/24" || p.Notes == nil || *p.Notes != text ||
		len(p.Circuits) != 1 || p.Circuits[0].ID != c.ID || p.Circuits[0].InterfaceID == nil || *p.Circuits[0].InterfaceID != port {
		t.Errorf("after restore: %+v", p)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `./scripts/test-db.sh -run 'TestDBSite(Networks|Circuit|Profile|Notes)|TestDBRestoreKeepsSiteProfiles' -v`
Expected: build failure — `undefined: SiteProfileService`.

- [ ] **Step 3: Live ports**

`backend/internal/services/port_live.go`:

```go
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// LivePort is one port as a circuit card shows it: its device, its name, its
// link state and its latest rates (nil when there is no recent sample).
// SiteID is the device's site, so a caller can drop a port whose device has
// moved elsewhere.
type LivePort struct {
	InterfaceID uuid.UUID `json:"interface_id"`
	DeviceID    uuid.UUID `json:"device_id"`
	DeviceName  string    `json:"device_name"`
	SiteID      uuid.UUID `json:"-"`
	IfIndex     int       `json:"if_index"`
	Number      int       `json:"number"`
	Label       string    `json:"label"`
	Name        string    `json:"name"`
	Alias       string    `json:"alias"`
	StackUnit   int       `json:"stack_unit"`
	OperStatus  string    `json:"oper_status"`
	InBps       *float64  `json:"in_bps"`
	OutBps      *float64  `json:"out_bps"`
}

// LivePorts reads the given ports with their devices and latest rates, keyed
// by interface id. Ports that no longer exist are left out. Rates use each
// device's own freshness window, as the port pages do.
func (s *PortService) LivePorts(ctx context.Context, interfaceIDs []uuid.UUID) (map[uuid.UUID]LivePort, error) {
	out := map[uuid.UUID]LivePort{}
	if len(interfaceIDs) == 0 {
		return out, nil
	}
	var rows []models.DeviceInterface
	if err := s.db.WithContext(ctx).Where("id IN ?", interfaceIDs).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("loading ports: %w", err)
	}
	if len(rows) == 0 {
		return out, nil
	}
	seen := map[uuid.UUID]bool{}
	var deviceIDs []uuid.UUID
	for _, r := range rows {
		if !seen[r.DeviceID] {
			seen[r.DeviceID] = true
			deviceIDs = append(deviceIDs, r.DeviceID)
		}
	}
	var devices []struct {
		ID           uuid.UUID
		Name         string
		SiteID       uuid.UUID
		PollInterval int
	}
	if err := s.db.WithContext(ctx).Table("devices").Select("id, name, site_id, poll_interval").
		Where("id IN ?", deviceIDs).Scan(&devices).Error; err != nil {
		return nil, fmt.Errorf("loading port devices: %w", err)
	}
	type deviceLive struct {
		name string
		site uuid.UUID
		live map[string]map[string]float64
	}
	now := time.Now()
	byDevice := make(map[uuid.UUID]deviceLive, len(devices))
	for _, d := range devices {
		latest, err := s.metrics.LatestMany(ctx, []uuid.UUID{d.ID}, liveMetrics, liveSince(now, d.PollInterval))
		if err != nil {
			return nil, err
		}
		byDevice[d.ID] = deviceLive{name: d.Name, site: d.SiteID, live: latest[d.ID]}
	}
	for _, r := range rows {
		d, ok := byDevice[r.DeviceID]
		if !ok {
			continue
		}
		v := toPortView(r, d.live)
		out[r.ID] = LivePort{InterfaceID: r.ID, DeviceID: r.DeviceID, DeviceName: d.name, SiteID: d.site,
			IfIndex: r.IfIndex, Number: v.Number, Label: v.Label, Name: r.Name, Alias: r.Alias, StackUnit: r.StackUnit,
			OperStatus: r.OperStatus, InBps: v.InBps, OutBps: v.OutBps}
	}
	return out, nil
}
```

- [ ] **Step 4: The profile service**

`backend/internal/services/site_profile_service.go`:

```go
package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// Site profile errors. A network or circuit of another site is not found, the
// same as one that does not exist.
var (
	ErrSiteNetworkNotFound  = errors.New("network not found")
	ErrSiteCircuitNotFound  = errors.New("circuit not found")
	ErrCircuitPortNotAtSite = errors.New("that port is not on a device at this site")
)

// SubnetTakenError is a subnet the site already records.
type SubnetTakenError struct{ Site, CIDR string }

func (e *SubnetTakenError) Error() string { return fmt.Sprintf("%s already has %s", e.Site, e.CIDR) }

// SiteProfileService keeps each site's networks, circuits and notes.
type SiteProfileService struct {
	db    *gorm.DB
	ports *PortService
}

// NewSiteProfileService builds the service; ports reads circuits' ports live.
func NewSiteProfileService(db *gorm.DB, ports *PortService) *SiteProfileService {
	return &SiteProfileService{db: db, ports: ports}
}

// SiteCircuitView is a circuit plus the live state of the port it is tied
// to: nil when it is not tied to one, or that port's device is no longer at
// the site.
type SiteCircuitView struct {
	models.SiteCircuit
	Port *LivePort `json:"port"`
}

// SiteProfile is what the site page shows about the place itself.
type SiteProfile struct {
	Notes    *string              `json:"notes"`
	Networks []models.SiteNetwork `json:"networks"`
	Circuits []SiteCircuitView    `json:"circuits"`
}

// siteNotes reads a site's notes, or ErrSiteNotFound.
func (s *SiteProfileService) siteNotes(ctx context.Context, siteID uuid.UUID) (*string, error) {
	var row struct{ Notes *string }
	res := s.db.WithContext(ctx).Table("sites").Select("notes").Where("id = ?", siteID).Scan(&row)
	if res.Error != nil {
		return nil, fmt.Errorf("loading site notes: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, ErrSiteNotFound
	}
	return row.Notes, nil
}

// Profile loads a site's notes, networks (by subnet, IPv4 first) and
// circuits (by provider, then age), each circuit's port read live.
func (s *SiteProfileService) Profile(ctx context.Context, siteID uuid.UUID) (*SiteProfile, error) {
	notes, err := s.siteNotes(ctx, siteID)
	if err != nil {
		return nil, err
	}
	out := &SiteProfile{Notes: notes, Networks: []models.SiteNetwork{}, Circuits: []SiteCircuitView{}}
	if err := s.db.WithContext(ctx).Where("site_id = ?", siteID).Order("family(cidr), cidr").Find(&out.Networks).Error; err != nil {
		return nil, fmt.Errorf("listing networks: %w", err)
	}
	if out.Networks == nil {
		out.Networks = []models.SiteNetwork{}
	}
	var circuits []models.SiteCircuit
	if err := s.db.WithContext(ctx).Where("site_id = ?", siteID).Order("lower(provider), created_at").Find(&circuits).Error; err != nil {
		return nil, fmt.Errorf("listing circuits: %w", err)
	}
	var ids []uuid.UUID
	for _, c := range circuits {
		if c.InterfaceID != nil {
			ids = append(ids, *c.InterfaceID)
		}
	}
	live, err := s.ports.LivePorts(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, c := range circuits {
		v := SiteCircuitView{SiteCircuit: c}
		if c.InterfaceID != nil {
			if p, ok := live[*c.InterfaceID]; ok && p.SiteID == siteID {
				v.Port = &p
			}
		}
		out.Circuits = append(out.Circuits, v)
	}
	return out, nil
}

// SetNotes replaces a site's notes (nil clears them) and returns them before
// and after, for the audit log.
func (s *SiteProfileService) SetNotes(ctx context.Context, siteID uuid.UUID, notes *string) (*string, *string, error) {
	before, err := s.siteNotes(ctx, siteID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.db.WithContext(ctx).Table("sites").Where("id = ?", siteID).
		Updates(map[string]any{"notes": notes, "updated_at": time.Now().UTC()}).Error; err != nil {
		return nil, nil, fmt.Errorf("saving site notes: %w", err)
	}
	return before, notes, nil
}

// network loads one of a site's networks.
func (s *SiteProfileService) network(ctx context.Context, siteID, id uuid.UUID) (*models.SiteNetwork, error) {
	var n models.SiteNetwork
	err := s.db.WithContext(ctx).Where("id = ? AND site_id = ?", id, siteID).First(&n).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSiteNetworkNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("loading network: %w", err)
	}
	return &n, nil
}

// checkSubnetFree refuses a subnet the site already records under another
// network than except (the one being edited, or uuid.Nil).
func (s *SiteProfileService) checkSubnetFree(ctx context.Context, siteID uuid.UUID, cidr string, except uuid.UUID) error {
	var n int64
	if err := s.db.WithContext(ctx).Model(&models.SiteNetwork{}).
		Where("site_id = ? AND cidr = ? AND id <> ?", siteID, cidr, except).Count(&n).Error; err != nil {
		return fmt.Errorf("checking subnet: %w", err)
	}
	if n == 0 {
		return nil
	}
	var site struct{ Name string }
	if err := s.db.WithContext(ctx).Table("sites").Select("name").Where("id = ?", siteID).Scan(&site).Error; err != nil {
		return fmt.Errorf("loading site name: %w", err)
	}
	return &SubnetTakenError{Site: site.Name, CIDR: cidr}
}

// CreateNetwork records a network; in must already be normalized.
func (s *SiteProfileService) CreateNetwork(ctx context.Context, siteID uuid.UUID, in models.SiteNetworkInput) (*models.SiteNetwork, error) {
	if err := s.checkSubnetFree(ctx, siteID, in.CIDR, uuid.Nil); err != nil {
		return nil, err
	}
	n := models.SiteNetwork{SiteID: siteID, Name: in.Name, CIDR: in.CIDR, VLAN: in.VLAN, Gateway: in.Gateway, Note: in.Note}
	if err := s.db.WithContext(ctx).Create(&n).Error; err != nil {
		return nil, fmt.Errorf("saving network: %w", err)
	}
	return s.network(ctx, siteID, n.ID)
}

// UpdateNetwork replaces a network's fields; in must already be normalized.
func (s *SiteProfileService) UpdateNetwork(ctx context.Context, siteID, id uuid.UUID, in models.SiteNetworkInput) (*models.SiteNetwork, *models.SiteNetwork, error) {
	before, err := s.network(ctx, siteID, id)
	if err != nil {
		return nil, nil, err
	}
	if err := s.checkSubnetFree(ctx, siteID, in.CIDR, id); err != nil {
		return nil, nil, err
	}
	if err := s.db.WithContext(ctx).Model(&models.SiteNetwork{}).Where("id = ?", id).Updates(map[string]any{
		"name": in.Name, "cidr": in.CIDR, "vlan": in.VLAN, "gateway": in.Gateway, "note": in.Note, "updated_at": time.Now().UTC(),
	}).Error; err != nil {
		return nil, nil, fmt.Errorf("updating network: %w", err)
	}
	after, err := s.network(ctx, siteID, id)
	return before, after, err
}

// DeleteNetwork removes a network and returns what it was.
func (s *SiteProfileService) DeleteNetwork(ctx context.Context, siteID, id uuid.UUID) (*models.SiteNetwork, error) {
	n, err := s.network(ctx, siteID, id)
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Delete(&models.SiteNetwork{}, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("deleting network: %w", err)
	}
	return n, nil
}

// circuit loads one of a site's circuits.
func (s *SiteProfileService) circuit(ctx context.Context, siteID, id uuid.UUID) (*models.SiteCircuit, error) {
	var c models.SiteCircuit
	err := s.db.WithContext(ctx).Where("id = ? AND site_id = ?", id, siteID).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSiteCircuitNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("loading circuit: %w", err)
	}
	return &c, nil
}

// checkPortAtSite refuses a port that is not on a device at this site.
func (s *SiteProfileService) checkPortAtSite(ctx context.Context, siteID uuid.UUID, interfaceID *uuid.UUID) error {
	if interfaceID == nil {
		return nil
	}
	var n int64
	if err := s.db.WithContext(ctx).Table("device_interfaces AS di").Joins("JOIN devices AS d ON d.id = di.device_id").
		Where("di.id = ? AND d.site_id = ?", *interfaceID, siteID).Count(&n).Error; err != nil {
		return fmt.Errorf("checking port: %w", err)
	}
	if n == 0 {
		return ErrCircuitPortNotAtSite
	}
	return nil
}

// CreateCircuit records a circuit; in must already be normalized.
func (s *SiteProfileService) CreateCircuit(ctx context.Context, siteID uuid.UUID, in models.SiteCircuitInput) (*models.SiteCircuit, error) {
	if err := s.checkPortAtSite(ctx, siteID, in.InterfaceID); err != nil {
		return nil, err
	}
	c := models.SiteCircuit{SiteID: siteID, Provider: in.Provider, CircuitRef: in.CircuitRef, Kind: in.Kind,
		DownloadMbps: in.DownloadMbps, UploadMbps: in.UploadMbps, SupportPhone: in.SupportPhone,
		AccountNumber: in.AccountNumber, Notes: in.Notes, InterfaceID: in.InterfaceID}
	if err := s.db.WithContext(ctx).Create(&c).Error; err != nil {
		return nil, fmt.Errorf("saving circuit: %w", err)
	}
	return s.circuit(ctx, siteID, c.ID)
}

// UpdateCircuit replaces a circuit's fields; in must already be normalized.
func (s *SiteProfileService) UpdateCircuit(ctx context.Context, siteID, id uuid.UUID, in models.SiteCircuitInput) (*models.SiteCircuit, *models.SiteCircuit, error) {
	before, err := s.circuit(ctx, siteID, id)
	if err != nil {
		return nil, nil, err
	}
	if err := s.checkPortAtSite(ctx, siteID, in.InterfaceID); err != nil {
		return nil, nil, err
	}
	if err := s.db.WithContext(ctx).Model(&models.SiteCircuit{}).Where("id = ?", id).Updates(map[string]any{
		"provider": in.Provider, "circuit_ref": in.CircuitRef, "kind": in.Kind,
		"download_mbps": in.DownloadMbps, "upload_mbps": in.UploadMbps, "support_phone": in.SupportPhone,
		"account_number": in.AccountNumber, "notes": in.Notes, "interface_id": in.InterfaceID,
		"updated_at": time.Now().UTC(),
	}).Error; err != nil {
		return nil, nil, fmt.Errorf("updating circuit: %w", err)
	}
	after, err := s.circuit(ctx, siteID, id)
	return before, after, err
}

// DeleteCircuit removes a circuit and returns what it was.
func (s *SiteProfileService) DeleteCircuit(ctx context.Context, siteID, id uuid.UUID) (*models.SiteCircuit, error) {
	c, err := s.circuit(ctx, siteID, id)
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Delete(&models.SiteCircuit{}, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("deleting circuit: %w", err)
	}
	return c, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `./scripts/test-db.sh -run 'TestDBSite(Networks|Circuit|Profile|Notes)|TestDBRestoreKeepsSiteProfiles' -v`
Expected: all seven PASS (`TestDBRestoreKeepsSiteProfiles` must say PASS, not SKIP). Then `go vet ./...`, `go test ./...` and the full `./scripts/test-db.sh` — PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/services/port_live.go backend/internal/services/site_profile_service.go backend/internal/services/site_profile_db_test.go
git commit -m "feat(sites): site profile service with live circuit ports

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Site profile API

**Files:**
- Create: `backend/internal/api/site_profile_handler.go`
- Modify: `backend/internal/models/audit.go` (site actions block), `backend/cmd/sentinel/main.go` (beside `api.RegisterSiteRoutes`)
- Test: `backend/internal/api/site_profile_db_test.go`

**Interfaces:**
- Consumes: Task 2's service, `SiteProfile`, errors; Task 1's input types and normalizers; existing `parseSiteID`, `requireSiteLevel(c, sites siteAccessChecker, id, level) bool`, `siteAccessChecker`, `auditRecorder`, `actorFrom(c)`, `respondSuccess`, `respondError`, `respondInternal`, `models.ResourceSite`, `models.AuditChanges{Summary, Before, After}`; test helpers in package `api`: `siteTestGroup`, `dataOf`, `newSite`, `toolRequest`, `recordingAudit` (fields `calls []auditCall{action, resourceType string; resourceID *uuid.UUID; changes models.AuditChanges}`).
- Produces: routes `GET /sites/:id/profile`, `PUT /sites/:id/notes`, `POST /sites/:id/networks`, `PUT|DELETE /sites/:id/networks/:networkId`, `POST /sites/:id/circuits`, `PUT|DELETE /sites/:id/circuits/:circuitId`; `RegisterSiteProfileRoutes(rg, profiles siteProfileStore, sites siteAccessChecker, audit auditRecorder)`; audit constants listed in Global Constraints.

- [ ] **Step 1: Write the failing database tests**

`backend/internal/api/site_profile_db_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func siteProfileRouter(t *testing.T, db *gorm.DB, caller uuid.UUID, audit *recordingAudit) *gin.Engine {
	t.Helper()
	r, v1, _ := siteTestGroup(t, db, caller)
	metrics := services.NewMetricsStore(db)
	ports := services.NewPortService(db, metrics, services.NewIncidentService(db), services.NewSettingsService(db))
	RegisterSiteProfileRoutes(v1, services.NewSiteProfileService(db, ports), services.NewSiteService(db), audit)
	return r
}

func shareSiteWith(t *testing.T, db *gorm.DB, site, user uuid.UUID, permission string) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO site_sharing (site_id, shared_with_user_id, permission) VALUES (?, ?, ?)`, site, user, permission)
}

// A read-only sharer reads the profile and is refused every change; someone
// the site is not shared with gets "not found"; an editable sharer changes
// it, each change audited against the site without sensitive circuit fields.
func TestDBSiteProfileAccess(t *testing.T) {
	db := testdb.Open(t)
	site := newSite(t, db, "Courthouse")
	readonly, editable, stranger := testdb.NewUser(t, db, false), testdb.NewUser(t, db, false), testdb.NewUser(t, db, false)
	shareSiteWith(t, db, site, readonly, "readonly")
	shareSiteWith(t, db, site, editable, "editable")
	base := "/api/v1/sites/" + site.String()
	changes := []struct{ method, path, body string }{
		{http.MethodPost, "/networks", `{"name":"Staff","cidr":"10.20.0.0/24"}`},
		{http.MethodPut, "/notes", `{"notes":"Closet: room 104"}`},
		{http.MethodPost, "/circuits", `{"provider":"Spectrum","kind":"fiber","account_number":"8347-SECRET","support_phone":"1-800-555-0100","notes":"PIN 4411"}`},
	}
	audit := &recordingAudit{}

	r := siteProfileRouter(t, db, readonly, audit)
	if w := toolRequest(r, http.MethodGet, base+"/profile", ""); w.Code != http.StatusOK {
		t.Errorf("read-only reading the profile: %d %s", w.Code, w.Body.String())
	}
	for _, c := range changes {
		if w := toolRequest(r, c.method, base+c.path, c.body); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "you need edit access to this site") {
			t.Errorf("read-only %s %s: %d %s, want 403", c.method, c.path, w.Code, w.Body.String())
		}
	}

	r = siteProfileRouter(t, db, stranger, audit)
	if w := toolRequest(r, http.MethodGet, base+"/profile", ""); w.Code != http.StatusNotFound {
		t.Errorf("stranger reading: %d, want 404", w.Code)
	}
	if w := toolRequest(r, http.MethodPost, base+"/networks", changes[0].body); w.Code != http.StatusNotFound {
		t.Errorf("stranger changing: %d, want 404", w.Code)
	}
	if len(audit.calls) != 0 {
		t.Fatalf("refused requests were audited: %+v", audit.calls)
	}

	r = siteProfileRouter(t, db, editable, audit)
	for _, c := range changes {
		if w := toolRequest(r, c.method, base+c.path, c.body); w.Code >= 300 {
			t.Fatalf("editable %s %s: %d %s", c.method, c.path, w.Code, w.Body.String())
		}
	}
	want := []string{"site_network_created", "site_notes_updated", "site_circuit_created"}
	if len(audit.calls) != len(want) {
		t.Fatalf("audit calls = %+v", audit.calls)
	}
	for i, call := range audit.calls {
		if call.action != want[i] || call.resourceType != "site" || call.resourceID == nil || *call.resourceID != site {
			t.Errorf("audit %d = %+v, want %s on the site", i, call, want[i])
		}
		logged, _ := json.Marshal(call.changes)
		for _, secret := range []string{"8347-SECRET", "1-800-555-0100", "PIN 4411"} {
			if strings.Contains(string(logged), secret) {
				t.Errorf("audit %s carries %q: %s", call.action, secret, logged)
			}
		}
	}
}

// Through the API: a host address is saved as its network, a duplicate and
// an outside gateway are refused with their messages, an unknown circuit
// type is refused, and another site's ids are not found.
func TestDBSiteProfileThroughAPI(t *testing.T) {
	db := testdb.Open(t)
	court, annex := newSite(t, db, "Courthouse"), newSite(t, db, "Annex")
	r := siteProfileRouter(t, db, testdb.NewUser(t, db, true), &recordingAudit{})
	base := "/api/v1/sites/" + court.String()

	w := toolRequest(r, http.MethodPost, base+"/networks", `{"name":"Staff","cidr":"10.20.0.5/24","vlan":10,"gateway":"10.20.0.1"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create network: %d %s", w.Code, w.Body.String())
	}
	net := dataOf[struct {
		ID   string `json:"id"`
		CIDR string `json:"cidr"`
	}](t, w)
	if net.CIDR != "10.20.0.0/24" {
		t.Errorf("cidr = %q, want 10.20.0.0/24", net.CIDR)
	}
	for _, c := range []struct{ body, want string }{
		{`{"name":"Again","cidr":"10.20.0.0/24"}`, "Courthouse already has 10.20.0.0/24"},
		{`{"name":"Bad","cidr":"10.30.0.0/24","gateway":"10.40.0.1"}`, "10.40.0.1 is outside 10.30.0.0/24"},
	} {
		if w := toolRequest(r, http.MethodPost, base+"/networks", c.body); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), c.want) {
			t.Errorf("%s: %d %s, want 400 %q", c.body, w.Code, w.Body.String(), c.want)
		}
	}
	if w := toolRequest(r, http.MethodPost, base+"/circuits", `{"provider":"Spectrum","kind":"satellite"}`); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "type must be one of") {
		t.Errorf("unknown circuit type: %d %s", w.Code, w.Body.String())
	}
	w = toolRequest(r, http.MethodPost, base+"/circuits", `{"provider":"Spectrum","kind":"fiber","download_mbps":500}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create circuit: %d %s", w.Code, w.Body.String())
	}
	ckt := dataOf[struct {
		ID string `json:"id"`
	}](t, w)

	other := "/api/v1/sites/" + annex.String()
	if w := toolRequest(r, http.MethodPut, other+"/networks/"+net.ID, `{"name":"x","cidr":"10.9.0.0/24"}`); w.Code != http.StatusNotFound {
		t.Errorf("another site's network: %d, want 404", w.Code)
	}
	if w := toolRequest(r, http.MethodDelete, other+"/circuits/"+ckt.ID, ""); w.Code != http.StatusNotFound {
		t.Errorf("another site's circuit: %d, want 404", w.Code)
	}

	if w := toolRequest(r, http.MethodDelete, base+"/networks/"+net.ID, ""); w.Code != http.StatusOK {
		t.Errorf("delete network: %d %s", w.Code, w.Body.String())
	}
	profile := dataOf[struct {
		Networks []json.RawMessage `json:"networks"`
		Circuits []json.RawMessage `json:"circuits"`
	}](t, toolRequest(r, http.MethodGet, base+"/profile", ""))
	if len(profile.Networks) != 0 || len(profile.Circuits) != 1 {
		t.Errorf("profile after delete: %d networks, %d circuits", len(profile.Networks), len(profile.Circuits))
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `./scripts/test-db.sh -run 'TestDBSiteProfile(Access|ThroughAPI)' -v`
Expected: build failure — `undefined: RegisterSiteProfileRoutes`.

- [ ] **Step 3: Audit actions**

In `backend/internal/models/audit.go`, after `ActionSiteUnshared`:

```go
	ActionSiteNotesUpdated   = "site_notes_updated"
	ActionSiteNetworkCreated = "site_network_created"
	ActionSiteNetworkUpdated = "site_network_updated"
	ActionSiteNetworkDeleted = "site_network_deleted"
	ActionSiteCircuitCreated = "site_circuit_created"
	ActionSiteCircuitUpdated = "site_circuit_updated"
	ActionSiteCircuitDeleted = "site_circuit_deleted"
```

- [ ] **Step 4: The handlers**

`backend/internal/api/site_profile_handler.go`:

```go
package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// siteProfileStore is what the site profile handlers need from
// SiteProfileService.
type siteProfileStore interface {
	Profile(ctx context.Context, siteID uuid.UUID) (*services.SiteProfile, error)
	SetNotes(ctx context.Context, siteID uuid.UUID, notes *string) (*string, *string, error)
	CreateNetwork(ctx context.Context, siteID uuid.UUID, in models.SiteNetworkInput) (*models.SiteNetwork, error)
	UpdateNetwork(ctx context.Context, siteID, id uuid.UUID, in models.SiteNetworkInput) (*models.SiteNetwork, *models.SiteNetwork, error)
	DeleteNetwork(ctx context.Context, siteID, id uuid.UUID) (*models.SiteNetwork, error)
	CreateCircuit(ctx context.Context, siteID uuid.UUID, in models.SiteCircuitInput) (*models.SiteCircuit, error)
	UpdateCircuit(ctx context.Context, siteID, id uuid.UUID, in models.SiteCircuitInput) (*models.SiteCircuit, *models.SiteCircuit, error)
	DeleteCircuit(ctx context.Context, siteID, id uuid.UUID) (*models.SiteCircuit, error)
}

// RegisterSiteProfileRoutes mounts a site's profile. Anyone who can see the
// site reads it; editable sharers and admins change it.
func RegisterSiteProfileRoutes(rg *gin.RouterGroup, profiles siteProfileStore, sites siteAccessChecker, audit auditRecorder) {
	g := rg.Group("/sites/:id")
	g.GET("/profile", getSiteProfileHandler(profiles, sites))
	g.PUT("/notes", putSiteNotesHandler(profiles, sites, audit))
	g.POST("/networks", createSiteNetworkHandler(profiles, sites, audit))
	g.PUT("/networks/:networkId", updateSiteNetworkHandler(profiles, sites, audit))
	g.DELETE("/networks/:networkId", deleteSiteNetworkHandler(profiles, sites, audit))
	g.POST("/circuits", createSiteCircuitHandler(profiles, sites, audit))
	g.PUT("/circuits/:circuitId", updateSiteCircuitHandler(profiles, sites, audit))
	g.DELETE("/circuits/:circuitId", deleteSiteCircuitHandler(profiles, sites, audit))
}

// siteForChange parses :id and requires edit access to the site, answering
// 404 or 403 otherwise.
func siteForChange(c *gin.Context, sites siteAccessChecker) (uuid.UUID, bool) {
	id, ok := parseSiteID(c)
	if !ok {
		return uuid.Nil, false
	}
	return id, requireSiteLevel(c, sites, id, services.SiteAccessEditable)
}

// parseChildID reads a network or circuit id from the path.
func parseChildID(c *gin.Context, param, what string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(param))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid "+what+" id")
		return uuid.Nil, false
	}
	return id, true
}

// respondProfileError maps SiteProfileService errors to responses.
func respondProfileError(c *gin.Context, op string, err error) {
	var taken *services.SubnetTakenError
	switch {
	case errors.As(err, &taken), errors.Is(err, services.ErrCircuitPortNotAtSite):
		respondError(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrSiteNetworkNotFound), errors.Is(err, services.ErrSiteCircuitNotFound):
		respondError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrSiteNotFound):
		respondError(c, http.StatusNotFound, "site not found")
	default:
		respondInternal(c, op, err)
	}
}

func networkSummary(n *models.SiteNetwork) map[string]any {
	return map[string]any{"name": n.Name, "cidr": n.CIDR, "vlan": n.VLAN, "gateway": n.Gateway, "note": n.Note}
}

// circuitSummary is what the audit log keeps of a circuit. Account number,
// support phone and notes are left out: they can hold things (PINs, account
// details) the audit log should not spread.
func circuitSummary(c *models.SiteCircuit) map[string]any {
	return map[string]any{"provider": c.Provider, "circuit_ref": c.CircuitRef, "kind": c.Kind,
		"download_mbps": c.DownloadMbps, "upload_mbps": c.UploadMbps, "interface_id": c.InterfaceID}
}

func getSiteProfileHandler(profiles siteProfileStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseSiteID(c)
		if !ok || !requireSiteLevel(c, sites, id, services.SiteAccessReadonly) {
			return
		}
		p, err := profiles.Profile(c.Request.Context(), id)
		if err != nil {
			respondProfileError(c, "getSiteProfile", err)
			return
		}
		respondSuccess(c, http.StatusOK, p)
	}
}

func putSiteNotesHandler(profiles siteProfileStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := siteForChange(c, sites)
		if !ok {
			return
		}
		var body struct {
			Notes string `json:"notes"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		notes, err := models.NormalizeSiteNotes(body.Notes)
		if err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		before, after, err := profiles.SetNotes(c.Request.Context(), id, notes)
		if err != nil {
			respondProfileError(c, "putSiteNotes", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteNotesUpdated, models.ResourceSite, &id,
			models.AuditChanges{Before: map[string]any{"notes": before}, After: map[string]any{"notes": after}})
		respondSuccess(c, http.StatusOK, gin.H{"notes": after})
	}
}

func bindNetwork(c *gin.Context) (models.SiteNetworkInput, bool) {
	var raw models.SiteNetworkInput
	if err := c.ShouldBindJSON(&raw); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return models.SiteNetworkInput{}, false
	}
	in, err := models.NormalizeSiteNetworkInput(raw)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return models.SiteNetworkInput{}, false
	}
	return in, true
}

func bindCircuit(c *gin.Context) (models.SiteCircuitInput, bool) {
	var raw models.SiteCircuitInput
	if err := c.ShouldBindJSON(&raw); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return models.SiteCircuitInput{}, false
	}
	in, err := models.NormalizeSiteCircuitInput(raw)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return models.SiteCircuitInput{}, false
	}
	return in, true
}

func createSiteNetworkHandler(profiles siteProfileStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := siteForChange(c, sites)
		if !ok {
			return
		}
		in, ok := bindNetwork(c)
		if !ok {
			return
		}
		n, err := profiles.CreateNetwork(c.Request.Context(), id, in)
		if err != nil {
			respondProfileError(c, "createSiteNetwork", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteNetworkCreated, models.ResourceSite, &id,
			models.AuditChanges{Summary: networkSummary(n)})
		respondSuccess(c, http.StatusCreated, n)
	}
}

func updateSiteNetworkHandler(profiles siteProfileStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := siteForChange(c, sites)
		if !ok {
			return
		}
		networkID, ok := parseChildID(c, "networkId", "network")
		if !ok {
			return
		}
		in, ok := bindNetwork(c)
		if !ok {
			return
		}
		before, after, err := profiles.UpdateNetwork(c.Request.Context(), id, networkID, in)
		if err != nil {
			respondProfileError(c, "updateSiteNetwork", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteNetworkUpdated, models.ResourceSite, &id,
			models.AuditChanges{Before: networkSummary(before), After: networkSummary(after)})
		respondSuccess(c, http.StatusOK, after)
	}
}

func deleteSiteNetworkHandler(profiles siteProfileStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := siteForChange(c, sites)
		if !ok {
			return
		}
		networkID, ok := parseChildID(c, "networkId", "network")
		if !ok {
			return
		}
		n, err := profiles.DeleteNetwork(c.Request.Context(), id, networkID)
		if err != nil {
			respondProfileError(c, "deleteSiteNetwork", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteNetworkDeleted, models.ResourceSite, &id,
			models.AuditChanges{Summary: networkSummary(n)})
		respondSuccess(c, http.StatusOK, gin.H{"deleted": true})
	}
}

func createSiteCircuitHandler(profiles siteProfileStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := siteForChange(c, sites)
		if !ok {
			return
		}
		in, ok := bindCircuit(c)
		if !ok {
			return
		}
		ckt, err := profiles.CreateCircuit(c.Request.Context(), id, in)
		if err != nil {
			respondProfileError(c, "createSiteCircuit", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteCircuitCreated, models.ResourceSite, &id,
			models.AuditChanges{Summary: circuitSummary(ckt)})
		respondSuccess(c, http.StatusCreated, ckt)
	}
}

func updateSiteCircuitHandler(profiles siteProfileStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := siteForChange(c, sites)
		if !ok {
			return
		}
		circuitID, ok := parseChildID(c, "circuitId", "circuit")
		if !ok {
			return
		}
		in, ok := bindCircuit(c)
		if !ok {
			return
		}
		before, after, err := profiles.UpdateCircuit(c.Request.Context(), id, circuitID, in)
		if err != nil {
			respondProfileError(c, "updateSiteCircuit", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteCircuitUpdated, models.ResourceSite, &id,
			models.AuditChanges{Before: circuitSummary(before), After: circuitSummary(after)})
		respondSuccess(c, http.StatusOK, after)
	}
}

func deleteSiteCircuitHandler(profiles siteProfileStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := siteForChange(c, sites)
		if !ok {
			return
		}
		circuitID, ok := parseChildID(c, "circuitId", "circuit")
		if !ok {
			return
		}
		ckt, err := profiles.DeleteCircuit(c.Request.Context(), id, circuitID)
		if err != nil {
			respondProfileError(c, "deleteSiteCircuit", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteCircuitDeleted, models.ResourceSite, &id,
			models.AuditChanges{Summary: circuitSummary(ckt)})
		respondSuccess(c, http.StatusOK, gin.H{"deleted": true})
	}
}
```

In `backend/cmd/sentinel/main.go`, after `portService` and `siteService` are created, add `siteProfileService := services.NewSiteProfileService(db, portService)`, and beside `api.RegisterSiteRoutes(...)` add `api.RegisterSiteProfileRoutes(v1, siteProfileService, siteService, auditService)`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `./scripts/test-db.sh -run 'TestDBSiteProfile(Access|ThroughAPI)' -v`
Expected: both PASS. Then `go vet ./...`, `go test ./...` and the full `./scripts/test-db.sh` — PASS. If gin panics at startup over a route conflict, the `/sites/:id/...` routes elsewhere use the same `:id` name; keep `:id`.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api/site_profile_handler.go backend/internal/api/site_profile_db_test.go backend/internal/models/audit.go backend/cmd/sentinel/main.go
git commit -m "feat(sites): site profile API (networks, circuits, notes)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Profile hook and helpers (frontend)

**Files:**
- Create: `frontend/src/hooks/useSiteProfile.ts`, `frontend/src/utils/siteProfile.ts`
- Test: throwaway `check.ts` + the frontend gate

**Interfaces:**
- Consumes: Task 3's API; `summaryCounts`, `ViewStatus` from `frontend/src/utils/monitoringView.ts` (piece 2).
- Produces (Tasks 5–6 use these exact names): types `CircuitKind`, `SiteNetwork`, `LivePort`, `SiteCircuit`, `SiteProfile`, `SiteNetworkInput`, `SiteCircuitInput`; `useSiteProfile(siteId: string | undefined) → { profile: SiteProfile | null, loading: boolean, error: string | null, refetch }`; `useSiteProfileActions(siteId: string) → { busy, saveNotes(notes), createNetwork(input), updateNetwork(id, input), deleteNetwork(id), createCircuit(input), updateCircuit(id, input), deleteCircuit(id) }`; helpers `CIRCUIT_KINDS`, `circuitKindLabel(kind)`, `circuitSpeed(c)`, `usage(bps, mbps): Usage | null`, `scanChoices(networks)`, `siteCounts(siteId, lists): SiteCounts`, `siteCountsText(c)`, `STATE_DOT: Record<ViewStatus, string>`.

- [ ] **Step 1: Write the failing check**

`$CHECK/check.ts`:

```ts
import {
  circuitKindLabel, circuitSpeed, scanChoices, siteCounts, siteCountsText, usage,
} from '/app/src/utils/siteProfile.ts'

let failures = 0
function expect(name: string, got: unknown, want: unknown) {
  if (JSON.stringify(got) !== JSON.stringify(want)) {
    failures++
    console.log(`FAIL ${name}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`)
  }
}
/* eslint-disable @typescript-eslint/no-explicit-any */
const mon = (o: any): any => ({ id: 'm', name: 'm', enabled: true, current_status: 'online', is_in_maintenance: false, site_id: null, ...o })
const srv = (o: any): any => ({ id: 's', status: 'active', site_id: null, ...o })
const dev = (o: any): any => ({ id: 'd', status: 'up', site_id: 'x', ...o })

expect('kind label', circuitKindLabel('copper'), 'T1/copper')
expect('unknown kind label', circuitKindLabel('weird'), 'Other')
expect('speed both', circuitSpeed({ download_mbps: 500, upload_mbps: 500 }), '500/500 Mb')
expect('speed decimals', circuitSpeed({ download_mbps: 1.5, upload_mbps: 0.75 }), '1.5/0.75 Mb')
expect('speed down only', circuitSpeed({ download_mbps: 300, upload_mbps: null }), '300 Mb down')
expect('speed up only', circuitSpeed({ download_mbps: null, upload_mbps: 20 }), '20 Mb up')
expect('no speed', circuitSpeed({ download_mbps: null, upload_mbps: null }), '')

expect('no rate', usage(null, 500), null)
expect('no speed', usage(2e8, null), { pct: null, bar: 0 })
expect('share', usage(2.1e8, 500), { pct: 42, bar: 42 })
expect('capped bar', usage(6e8, 500), { pct: 120, bar: 100 })

const nets = [
  { name: 'Staff', cidr: '10.20.0.0/24' },
  { name: 'Big', cidr: '10.0.0.0/21' },
  { name: 'Edge', cidr: '10.30.0.0/22' },
  { name: 'Host', cidr: '10.40.0.1/32' },
  { name: 'v6', cidr: '2001:db8::/64' },
]
expect('scan choices', scanChoices(nets).map((n) => n.name), ['Staff', 'Edge', 'Host'])

const lists = {
  monitors: [mon({ site_id: 'a' }), mon({ site_id: 'a', current_status: 'offline' }), mon({ site_id: 'b' })],
  agents: [srv({ site_id: 'a', status: 'offline' }), srv({ site_id: null })],
  devices: [dev({ site_id: 'a', status: 'error' }), dev({ site_id: 'a' }), dev({ site_id: 'b', status: 'down' })],
}
const a = siteCounts('a', lists)
expect('counts', a, { devices: 2, servers: 1, checks: 2, watched: 5, down: 2 })
expect('text', siteCountsText(a), '2 devices · 1 server · 2 checks')
const partial = siteCounts('a', { ...lists, agents: null })
expect('a list not loaded is left out', siteCountsText(partial), '2 devices · 2 checks')
expect('partial down', partial.down, 1)
expect('singulars', siteCountsText({ devices: 1, servers: 0, checks: 1, watched: 2, down: 0 }), '1 device · 0 servers · 1 check')

if (failures) {
  console.log(`${failures} check(s) failed`)
  process.exit(1)
}
console.log('siteProfile: all checks passed')
```

- [ ] **Step 2: Run it to verify it fails**

Run the throwaway check command.
Expected: esbuild error — `Could not resolve "/app/src/utils/siteProfile.ts"`.

- [ ] **Step 3: The hook**

`frontend/src/hooks/useSiteProfile.ts`:

```ts
import { useCallback, useEffect, useRef, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

export type CircuitKind = 'fiber' | 'cable' | 'dsl' | 'fixed_wireless' | 'cellular' | 'copper' | 'other'

export interface SiteNetwork {
  id: string
  site_id: string
  name: string
  cidr: string
  vlan: number | null
  gateway: string | null
  note: string | null
  created_at: string
  updated_at: string
}

/** The port a circuit is tied to, read live. Rates are null without a recent sample. */
export interface LivePort {
  interface_id: string
  device_id: string
  device_name: string
  if_index: number
  number: number
  label: string
  name: string
  alias: string
  stack_unit: number
  oper_status: string
  in_bps: number | null
  out_bps: number | null
}

export interface SiteCircuit {
  id: string
  site_id: string
  provider: string
  circuit_ref: string | null
  kind: CircuitKind
  download_mbps: number | null
  upload_mbps: number | null
  support_phone: string | null
  account_number: string | null
  notes: string | null
  interface_id: string | null
  /** null when not tied to a port, or its device has left the site. */
  port: LivePort | null
  created_at: string
  updated_at: string
}

export interface SiteProfile {
  notes: string | null
  networks: SiteNetwork[]
  circuits: SiteCircuit[]
}

export interface SiteNetworkInput {
  name: string
  cidr: string
  vlan: number | null
  gateway: string | null
  note: string | null
}

export interface SiteCircuitInput {
  provider: string
  kind: CircuitKind
  circuit_ref: string | null
  download_mbps: number | null
  upload_mbps: number | null
  support_phone: string | null
  account_number: string | null
  notes: string | null
  interface_id: string | null
}

const POLL_MS = 30_000

/** A site's notes, networks and circuits (with their ports live), refreshed
 *  every 30 seconds. The answer is kept with the site it belongs to and each
 *  load takes a number, so neither a slow load nor another site's answer can
 *  replace what this page shows. */
export function useSiteProfile(siteId: string | undefined) {
  const [answer, setAnswer] = useState<{ siteId: string; profile: SiteProfile | null; error: string | null } | null>(null)
  const latest = useRef(0)

  const refetch = useCallback(async () => {
    if (!siteId) return
    const mine = ++latest.current
    try {
      const { data } = await api.get<ApiResponse<SiteProfile>>(`/sites/${siteId}/profile`)
      if (mine === latest.current) setAnswer({ siteId, profile: data.data, error: null })
    } catch (err) {
      if (mine === latest.current) {
        setAnswer((prev) => ({
          siteId,
          // A failed refresh keeps showing what this site had.
          profile: prev && prev.siteId === siteId ? prev.profile : null,
          error: (err as ApiError).message || 'Could not load the site profile',
        }))
      }
    }
  }, [siteId])

  useEffect(() => {
    void refetch()
    const t = window.setInterval(() => void refetch(), POLL_MS)
    return () => window.clearInterval(t)
  }, [refetch])

  const current = answer && answer.siteId === siteId ? answer : null
  return {
    profile: current?.profile ?? null,
    loading: !current,
    error: current?.error ?? null,
    refetch,
  }
}

/** Changes to a site's profile. Each call rejects with the API's error. */
export function useSiteProfileActions(siteId: string) {
  const [busy, setBusy] = useState(false)
  const run = useCallback(async <T>(fn: () => Promise<T>): Promise<T> => {
    setBusy(true)
    try {
      return await fn()
    } finally {
      setBusy(false)
    }
  }, [])
  const base = `/sites/${siteId}`
  return {
    busy,
    saveNotes: (notes: string) => run(() => api.put(`${base}/notes`, { notes })),
    createNetwork: (input: SiteNetworkInput) => run(() => api.post(`${base}/networks`, input)),
    updateNetwork: (id: string, input: SiteNetworkInput) => run(() => api.put(`${base}/networks/${id}`, input)),
    deleteNetwork: (id: string) => run(() => api.delete(`${base}/networks/${id}`)),
    createCircuit: (input: SiteCircuitInput) => run(() => api.post(`${base}/circuits`, input)),
    updateCircuit: (id: string, input: SiteCircuitInput) => run(() => api.put(`${base}/circuits/${id}`, input)),
    deleteCircuit: (id: string) => run(() => api.delete(`${base}/circuits/${id}`)),
  }
}
```

- [ ] **Step 4: The helpers**

`frontend/src/utils/siteProfile.ts`:

```ts
// What the site page and the sites list compute from a site's profile and the
// lists the Monitoring page loads. Only type imports from `@/` and relative
// value imports, so it can be checked on its own.
import type { Monitor } from '@/types'
import type { Agent } from '@/hooks/useAgents'
import type { Device } from '@/hooks/useDevices'
import type { CircuitKind } from '@/hooks/useSiteProfile'
import { summaryCounts, type ViewStatus } from './monitoringView'

export const CIRCUIT_KINDS: { value: CircuitKind; label: string }[] = [
  { value: 'fiber', label: 'Fiber' },
  { value: 'cable', label: 'Cable' },
  { value: 'dsl', label: 'DSL' },
  { value: 'fixed_wireless', label: 'Fixed wireless' },
  { value: 'cellular', label: 'Cellular' },
  { value: 'copper', label: 'T1/copper' },
  { value: 'other', label: 'Other' },
]

export function circuitKindLabel(kind: string): string {
  return CIRCUIT_KINDS.find((k) => k.value === kind)?.label ?? 'Other'
}

const mb = (v: number) => `${+v.toFixed(2)}`

/** "500/500 Mb", "300 Mb down", "20 Mb up", or '' with no speeds. */
export function circuitSpeed(c: { download_mbps: number | null; upload_mbps: number | null }): string {
  const d = c.download_mbps
  const u = c.upload_mbps
  if (d != null && u != null) return `${mb(d)}/${mb(u)} Mb`
  if (d != null) return `${mb(d)} Mb down`
  if (u != null) return `${mb(u)} Mb up`
  return ''
}

export interface Usage {
  /** The rate as a share of the speed (may pass 100), or null with no speed. */
  pct: number | null
  /** The bar's width, 0-100. */
  bar: number
}

/** usage is a rate against a circuit speed; null when the rate is unknown. */
export function usage(bps: number | null | undefined, mbps: number | null | undefined): Usage | null {
  if (bps == null) return null
  if (!mbps || mbps <= 0) return { pct: null, bar: 0 }
  // Multiplied first so whole shares stay whole (0.42 * 100 is not 42).
  const pct = (bps * 100) / (mbps * 1e6)
  return { pct, bar: Math.max(0, Math.min(100, pct)) }
}

/** scanChoices are the saved networks Scan subnet can offer: IPv4, /22 to /32. */
export function scanChoices<T extends { cidr: string }>(networks: T[]): T[] {
  return networks.filter((n) => {
    const m = /^\d{1,3}(?:\.\d{1,3}){3}\/(\d{1,2})$/.exec(n.cidr)
    return !!m && Number(m[1]) >= 22 && Number(m[1]) <= 32
  })
}

export interface SiteCounts {
  /** null when that list has not loaded (or failed). */
  devices: number | null
  servers: number | null
  checks: number | null
  watched: number
  down: number
}

/** siteCounts counts what is at a site in each loaded list (null for one that
 *  has not loaded). Down uses the shared status scale. */
export function siteCounts(
  siteId: string,
  lists: { monitors: Monitor[] | null; agents: Agent[] | null; devices: Device[] | null }
): SiteCounts {
  const monitors = lists.monitors?.filter((m) => m.site_id === siteId) ?? null
  const agents = lists.agents?.filter((a) => a.site_id === siteId) ?? null
  const devices = lists.devices?.filter((d) => d.site_id === siteId) ?? null
  const s = summaryCounts(monitors ?? [], agents ?? [], devices ?? [])
  return {
    devices: devices?.length ?? null,
    servers: agents?.length ?? null,
    checks: monitors?.length ?? null,
    watched: s.watched,
    down: s.down,
  }
}

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`

/** "18 devices · 3 servers · 9 checks", leaving out lists that did not load. */
export function siteCountsText(c: SiteCounts): string {
  const parts: string[] = []
  if (c.devices != null) parts.push(plural(c.devices, 'device', 'devices'))
  if (c.servers != null) parts.push(plural(c.servers, 'server', 'servers'))
  if (c.checks != null) parts.push(plural(c.checks, 'check', 'checks'))
  return parts.join(' · ')
}

/** The dot colour for each state on the shared scale. */
export const STATE_DOT: Record<ViewStatus, string> = {
  up: 'bg-emerald-400',
  down: 'bg-red-400',
  pending: 'bg-amber-300',
  paused: 'bg-slate-500',
  maintenance: 'bg-sky-400',
  error: 'bg-orange-400',
}
```

- [ ] **Step 5: Run the check to verify it passes**

Run the throwaway check command.
Expected: `siteProfile: all checks passed`.

- [ ] **Step 6: Run the frontend gate**

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/hooks/useSiteProfile.ts frontend/src/utils/siteProfile.ts
git commit -m "feat(sites): site profile hook and helpers

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Profile cards and the two-column site page

**Files:**
- Create: `frontend/src/components/sites/{NotesCard,NetworksCard,NetworkFormModal,CircuitsCard,CircuitFormModal}.tsx`
- Modify: `frontend/src/pages/network/SiteDetail.tsx` (whole file below)
- Test: the frontend gate

**Interfaces:**
- Consumes: Task 4's hook, types and helpers; existing `useDevices({ siteId })`, `usePortChoices(deviceId)` (→ `{ ports: PortView[] | null, error }`), `portTitle(p)` and `formatBps(bps)` from `@/utils/network`, `SiteSharingPanel`, and everything `SiteDetail` imports today.
- Produces: `<NotesCard notes canEdit busy onSave />`, `<NetworksCard siteId networks canEdit onDelete onChanged />`, `<CircuitsCard siteId circuits canEdit onDelete onChanged />`, `<NetworkFormModal siteId initial? onClose onSaved />`, `<CircuitFormModal siteId initial? onClose onSaved />`; the two-column `SiteDetail` (Task 6 edits it at the anchors named there).

- [ ] **Step 1: Notes card**

`frontend/src/components/sites/NotesCard.tsx`:

```tsx
import { useState } from 'react'
import { Pencil } from 'lucide-react'
import type { ApiError } from '@/services/api'

const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

interface Props {
  notes: string | null
  canEdit: boolean
  busy: boolean
  onSave: (notes: string) => Promise<unknown>
}

/** A site's free-form notes, edited in place. Line breaks are kept. */
export default function NotesCard({ notes, canEdit, busy, onSave }: Props) {
  const [draft, setDraft] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const save = async () => {
    if (draft === null) return
    setError(null)
    try {
      await onSave(draft)
      setDraft(null)
    } catch (err) {
      setError((err as ApiError).message || 'Could not save the notes')
    }
  }

  return (
    <section className="card space-y-3 p-4">
      <div className="flex items-center justify-between gap-2">
        <h2 className="text-lg font-light text-white">Notes</h2>
        {canEdit && draft === null && (
          <button className="btn-secondary flex items-center gap-1.5 !py-1 text-sm" onClick={() => setDraft(notes ?? '')}>
            <Pencil className="h-3.5 w-3.5" /> Edit
          </button>
        )}
      </div>
      {draft !== null ? (
        <div className="space-y-2">
          <textarea
            className={inputCls}
            rows={6}
            maxLength={10000}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            aria-label="Site notes"
            autoFocus
          />
          {error && <p className="text-sm text-red-400">{error}</p>}
          <div className="flex justify-end gap-2">
            <button
              className="btn-secondary"
              disabled={busy}
              onClick={() => {
                setDraft(null)
                setError(null)
              }}
            >
              Cancel
            </button>
            <button className="btn-primary" disabled={busy} onClick={() => void save()}>
              {busy ? 'Saving…' : 'Save'}
            </button>
          </div>
        </div>
      ) : notes ? (
        <p className="whitespace-pre-wrap break-words text-sm text-slate-300">{notes}</p>
      ) : (
        <p className="text-sm text-slate-500">{canEdit ? 'No notes yet.' : 'No notes recorded.'}</p>
      )}
    </section>
  )
}
```

- [ ] **Step 2: Network form and card**

`frontend/src/components/sites/NetworkFormModal.tsx`:

```tsx
import { useState } from 'react'
import { X } from 'lucide-react'
import { useSiteProfileActions, type SiteNetwork } from '@/hooks/useSiteProfile'
import type { ApiError } from '@/services/api'

const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

interface Props {
  siteId: string
  /** The network being edited; omitted when adding. */
  initial?: SiteNetwork
  onClose: () => void
  onSaved: () => void
}

export default function NetworkFormModal({ siteId, initial, onClose, onSaved }: Props) {
  const { createNetwork, updateNetwork, busy } = useSiteProfileActions(siteId)
  const [name, setName] = useState(initial?.name ?? '')
  const [cidr, setCidr] = useState(initial?.cidr ?? '')
  const [vlan, setVlan] = useState(initial?.vlan != null ? String(initial.vlan) : '')
  const [gateway, setGateway] = useState(initial?.gateway ?? '')
  const [note, setNote] = useState(initial?.note ?? '')
  const [error, setError] = useState<string | null>(null)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    const vlanNum = vlan.trim() === '' ? null : Number(vlan.trim())
    if (vlanNum !== null && !Number.isInteger(vlanNum)) {
      setError('VLAN must be a whole number')
      return
    }
    // Blank optional fields are sent as null; the server trims and checks the rest.
    const input = { name, cidr, vlan: vlanNum, gateway: gateway.trim() || null, note: note.trim() || null }
    try {
      if (initial) await updateNetwork(initial.id, input)
      else await createNetwork(input)
      onSaved()
    } catch (err) {
      setError((err as ApiError).message || 'Could not save the network')
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <form className="card w-full max-w-md space-y-4 p-6" onClick={(e) => e.stopPropagation()} onSubmit={(e) => void submit(e)}>
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">{initial ? 'Edit network' : 'Add network'}</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>
        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Name</span>
          <input className={inputCls} value={name} onChange={(e) => setName(e.target.value)} maxLength={100} placeholder="Staff" required autoFocus />
        </label>
        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Subnet</span>
          <input className={inputCls} value={cidr} onChange={(e) => setCidr(e.target.value)} placeholder="10.20.0.0/24" required />
        </label>
        <div className="grid grid-cols-2 gap-3">
          <label className="block space-y-1">
            <span className="text-sm text-slate-300">VLAN <span className="text-slate-500">(optional)</span></span>
            <input className={inputCls} value={vlan} onChange={(e) => setVlan(e.target.value)} inputMode="numeric" placeholder="1–4094" />
          </label>
          <label className="block space-y-1">
            <span className="text-sm text-slate-300">Gateway <span className="text-slate-500">(optional)</span></span>
            <input className={inputCls} value={gateway} onChange={(e) => setGateway(e.target.value)} placeholder="10.20.0.1" />
          </label>
        </div>
        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Note <span className="text-slate-500">(optional)</span></span>
          <input className={inputCls} value={note} onChange={(e) => setNote(e.target.value)} maxLength={500} />
        </label>
        {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="btn-primary" disabled={busy || !name.trim() || !cidr.trim()}>
            {busy ? 'Saving…' : initial ? 'Save' : 'Add network'}
          </button>
        </div>
      </form>
    </div>
  )
}
```

`frontend/src/components/sites/NetworksCard.tsx`:

```tsx
import { useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import NetworkFormModal from '@/components/sites/NetworkFormModal'
import type { SiteNetwork } from '@/hooks/useSiteProfile'
import type { ApiError } from '@/services/api'

interface Props {
  siteId: string
  networks: SiteNetwork[]
  canEdit: boolean
  onDelete: (id: string) => Promise<unknown>
  onChanged: () => void
}

/** A site's subnets: name, then subnet · VLAN · gateway, then its note. */
export default function NetworksCard({ siteId, networks, canEdit, onDelete, onChanged }: Props) {
  const [editing, setEditing] = useState<SiteNetwork | 'new' | null>(null)
  const [confirming, setConfirming] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const remove = async (id: string) => {
    setError(null)
    try {
      await onDelete(id)
      setConfirming(null)
      onChanged()
    } catch (err) {
      setError((err as ApiError).message || 'Could not delete the network')
    }
  }

  return (
    <section className="card space-y-3 p-4">
      <div className="flex items-center justify-between gap-2">
        <h2 className="text-lg font-light text-white">Networks</h2>
        {canEdit && (
          <button className="btn-secondary flex items-center gap-1.5 !py-1 text-sm" onClick={() => setEditing('new')}>
            <Plus className="h-3.5 w-3.5" /> Add
          </button>
        )}
      </div>
      {error && <p className="text-sm text-red-400">{error}</p>}
      {networks.length === 0 ? (
        <p className="text-sm text-slate-500">{canEdit ? 'No networks yet.' : 'No networks recorded.'}</p>
      ) : (
        <ul className="divide-y divide-white/10">
          {networks.map((n) => (
            <li key={n.id} className="flex items-start justify-between gap-2 py-2 text-sm">
              <div className="min-w-0">
                <div className="font-medium text-slate-200">{n.name}</div>
                <div className="font-mono text-xs text-slate-400">
                  {n.cidr}
                  {n.vlan != null && ` · VLAN ${n.vlan}`}
                  {n.gateway && ` · gw ${n.gateway}`}
                </div>
                {n.note && <div className="mt-0.5 break-words text-xs text-slate-500">{n.note}</div>}
              </div>
              {canEdit &&
                (confirming === n.id ? (
                  <div className="flex shrink-0 items-center gap-1.5 text-xs">
                    <span className="text-amber-300">Delete?</span>
                    <button className="btn-secondary !px-2 !py-0.5 text-xs" onClick={() => setConfirming(null)}>
                      Cancel
                    </button>
                    <button className="btn bg-red-600 !px-2 !py-0.5 text-xs text-white hover:bg-red-700" onClick={() => void remove(n.id)}>
                      Delete
                    </button>
                  </div>
                ) : (
                  <div className="flex shrink-0 gap-1">
                    <button className="rounded p-1 text-slate-400 hover:bg-white/10 hover:text-white" aria-label={`Edit ${n.name}`} onClick={() => setEditing(n)}>
                      <Pencil className="h-3.5 w-3.5" />
                    </button>
                    <button className="rounded p-1 text-red-400 hover:bg-red-500/10" aria-label={`Delete ${n.name}`} onClick={() => setConfirming(n.id)}>
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                ))}
            </li>
          ))}
        </ul>
      )}
      {editing && (
        <NetworkFormModal
          siteId={siteId}
          initial={editing === 'new' ? undefined : editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            onChanged()
          }}
        />
      )}
    </section>
  )
}
```

- [ ] **Step 3: Circuit form and card**

`frontend/src/components/sites/CircuitFormModal.tsx`:

```tsx
import { useState } from 'react'
import { X } from 'lucide-react'
import { useDevices } from '@/hooks/useDevices'
import { usePortChoices } from '@/hooks/usePorts'
import { useSiteProfileActions, type CircuitKind, type SiteCircuit } from '@/hooks/useSiteProfile'
import { CIRCUIT_KINDS } from '@/utils/siteProfile'
import { portTitle } from '@/utils/network'
import type { ApiError } from '@/services/api'

const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

interface Props {
  siteId: string
  /** The circuit being edited; omitted when adding. */
  initial?: SiteCircuit
  onClose: () => void
  onSaved: () => void
}

// A speed box: blank is null, anything else must be a number.
function speedValue(s: string): number | null | 'bad' {
  const t = s.trim()
  if (t === '') return null
  const n = Number(t)
  return Number.isFinite(n) ? n : 'bad'
}

export default function CircuitFormModal({ siteId, initial, onClose, onSaved }: Props) {
  const { createCircuit, updateCircuit, busy } = useSiteProfileActions(siteId)
  const { devices } = useDevices({ siteId })
  const [provider, setProvider] = useState(initial?.provider ?? '')
  const [kind, setKind] = useState<CircuitKind>(initial?.kind ?? 'fiber')
  const [circuitRef, setCircuitRef] = useState(initial?.circuit_ref ?? '')
  const [down, setDown] = useState(initial?.download_mbps != null ? String(initial.download_mbps) : '')
  const [up, setUp] = useState(initial?.upload_mbps != null ? String(initial.upload_mbps) : '')
  const [phone, setPhone] = useState(initial?.support_phone ?? '')
  const [account, setAccount] = useState(initial?.account_number ?? '')
  const [notes, setNotes] = useState(initial?.notes ?? '')
  // Seeded from the live port, not the stored id: a port whose device has left
  // the site comes back as no port, so saving drops the stale link.
  const [deviceId, setDeviceId] = useState(initial?.port?.device_id ?? '')
  const [interfaceId, setInterfaceId] = useState(initial?.port?.interface_id ?? '')
  const { ports } = usePortChoices(deviceId || undefined)
  const [error, setError] = useState<string | null>(null)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    const d = speedValue(down)
    const u = speedValue(up)
    if (d === 'bad' || u === 'bad') {
      setError('Speeds must be numbers in Mbps')
      return
    }
    const input = {
      provider,
      kind,
      circuit_ref: circuitRef.trim() || null,
      download_mbps: d,
      upload_mbps: u,
      support_phone: phone.trim() || null,
      account_number: account.trim() || null,
      notes: notes.trim() || null,
      interface_id: deviceId && interfaceId ? interfaceId : null,
    }
    try {
      if (initial) await updateCircuit(initial.id, input)
      else await createCircuit(input)
      onSaved()
    } catch (err) {
      setError((err as ApiError).message || 'Could not save the circuit')
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <form
        className="card max-h-[90vh] w-full max-w-lg space-y-4 overflow-y-auto p-6"
        onClick={(e) => e.stopPropagation()}
        onSubmit={(e) => void submit(e)}
      >
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">{initial ? 'Edit circuit' : 'Add circuit'}</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>
        <div className="grid grid-cols-3 gap-3">
          <label className="col-span-2 block space-y-1">
            <span className="text-sm text-slate-300">Provider</span>
            <input className={inputCls} value={provider} onChange={(e) => setProvider(e.target.value)} maxLength={100} placeholder="Spectrum" required autoFocus />
          </label>
          <label className="block space-y-1">
            <span className="text-sm text-slate-300">Type</span>
            <select className={`${inputCls} cursor-pointer`} value={kind} onChange={(e) => setKind(e.target.value as CircuitKind)}>
              {CIRCUIT_KINDS.map((k) => (
                <option key={k.value} value={k.value}>
                  {k.label}
                </option>
              ))}
            </select>
          </label>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <label className="block space-y-1">
            <span className="text-sm text-slate-300">Download (Mbps)</span>
            <input className={inputCls} value={down} onChange={(e) => setDown(e.target.value)} inputMode="decimal" placeholder="500" />
          </label>
          <label className="block space-y-1">
            <span className="text-sm text-slate-300">Upload (Mbps)</span>
            <input className={inputCls} value={up} onChange={(e) => setUp(e.target.value)} inputMode="decimal" placeholder="500" />
          </label>
        </div>
        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Circuit ID</span>
          <input className={inputCls} value={circuitRef} onChange={(e) => setCircuitRef(e.target.value)} maxLength={100} />
        </label>
        <div className="grid grid-cols-2 gap-3">
          <label className="block space-y-1">
            <span className="text-sm text-slate-300">Support phone</span>
            <input className={inputCls} value={phone} onChange={(e) => setPhone(e.target.value)} maxLength={50} />
          </label>
          <label className="block space-y-1">
            <span className="text-sm text-slate-300">Account number</span>
            <input className={inputCls} value={account} onChange={(e) => setAccount(e.target.value)} maxLength={100} />
          </label>
        </div>
        <fieldset className="space-y-2">
          <legend className="mb-1 text-sm text-slate-300">Port it plugs into</legend>
          <select
            className={`${inputCls} cursor-pointer`}
            value={deviceId}
            onChange={(e) => {
              setDeviceId(e.target.value)
              setInterfaceId('')
            }}
            aria-label="Device"
          >
            <option value="">Not tied to a port</option>
            {devices.map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
              </option>
            ))}
          </select>
          {deviceId &&
            (ports === null ? (
              <p className="text-xs text-slate-500">Loading ports…</p>
            ) : (
              <select className={`${inputCls} cursor-pointer`} value={interfaceId} onChange={(e) => setInterfaceId(e.target.value)} aria-label="Port">
                <option value="">Choose a port…</option>
                {ports.map((p) => (
                  <option key={p.id} value={p.id}>
                    {portTitle(p)}
                  </option>
                ))}
              </select>
            ))}
        </fieldset>
        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Notes</span>
          <textarea className={inputCls} rows={2} value={notes} onChange={(e) => setNotes(e.target.value)} maxLength={1000} />
        </label>
        {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="btn-primary" disabled={busy || !provider.trim()}>
            {busy ? 'Saving…' : initial ? 'Save' : 'Add circuit'}
          </button>
        </div>
      </form>
    </div>
  )
}
```

`frontend/src/components/sites/CircuitsCard.tsx`:

```tsx
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import CircuitFormModal from '@/components/sites/CircuitFormModal'
import type { SiteCircuit } from '@/hooks/useSiteProfile'
import type { ApiError } from '@/services/api'
import { circuitKindLabel, circuitSpeed, usage } from '@/utils/siteProfile'
import { formatBps, portTitle } from '@/utils/network'

interface Props {
  siteId: string
  circuits: SiteCircuit[]
  canEdit: boolean
  onDelete: (id: string) => Promise<unknown>
  onChanged: () => void
}

function UsageRow({ label, bps, mbps }: { label: string; bps: number | null; mbps: number | null }) {
  const u = usage(bps, mbps)
  return (
    <div>
      <div className="flex justify-between gap-2 text-xs">
        <span className="text-slate-400">{label}</span>
        <span className="tabular-nums text-slate-300">
          {formatBps(bps)}
          {u?.pct != null && ` · ${Math.round(u.pct)}%`}
        </span>
      </div>
      {u?.pct != null && (
        <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-white/10">
          <div
            className={`h-full rounded-full ${u.bar >= 90 ? 'bg-red-500' : u.bar >= 75 ? 'bg-amber-500' : 'bg-emerald-500'}`}
            style={{ width: `${u.bar}%` }}
          />
        </div>
      )}
    </div>
  )
}

function Detail({ label, value }: { label: string; value: string | null }) {
  if (!value) return null
  return (
    <div className="flex justify-between gap-3 text-xs">
      <span className="shrink-0 text-slate-500">{label}</span>
      <span className="min-w-0 break-words text-right text-slate-300">{value}</span>
    </div>
  )
}

/** A site's ISP circuits. A circuit tied to a port shows that port's current
 *  in (download) and out (upload) against the circuit's speed. */
export default function CircuitsCard({ siteId, circuits, canEdit, onDelete, onChanged }: Props) {
  const [editing, setEditing] = useState<SiteCircuit | 'new' | null>(null)
  const [confirming, setConfirming] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const remove = async (id: string) => {
    setError(null)
    try {
      await onDelete(id)
      setConfirming(null)
      onChanged()
    } catch (err) {
      setError((err as ApiError).message || 'Could not delete the circuit')
    }
  }

  return (
    <section className="card space-y-3 p-4">
      <div className="flex items-center justify-between gap-2">
        <h2 className="text-lg font-light text-white">Circuits</h2>
        {canEdit && (
          <button className="btn-secondary flex items-center gap-1.5 !py-1 text-sm" onClick={() => setEditing('new')}>
            <Plus className="h-3.5 w-3.5" /> Add
          </button>
        )}
      </div>
      {error && <p className="text-sm text-red-400">{error}</p>}
      {circuits.length === 0 ? (
        <p className="text-sm text-slate-500">{canEdit ? 'No circuits yet.' : 'No circuits recorded.'}</p>
      ) : (
        <ul className="space-y-3">
          {circuits.map((c) => {
            const speed = circuitSpeed(c)
            const port = c.port
            return (
              <li key={c.id} className="space-y-2 rounded-lg border border-white/10 p-3">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <div className="font-medium text-slate-200">{c.provider}</div>
                    <div className="text-xs text-slate-400">
                      {circuitKindLabel(c.kind)}
                      {speed && ` · ${speed}`}
                    </div>
                  </div>
                  {canEdit &&
                    (confirming === c.id ? (
                      <div className="flex shrink-0 items-center gap-1.5 text-xs">
                        <span className="text-amber-300">Delete?</span>
                        <button className="btn-secondary !px-2 !py-0.5 text-xs" onClick={() => setConfirming(null)}>
                          Cancel
                        </button>
                        <button className="btn bg-red-600 !px-2 !py-0.5 text-xs text-white hover:bg-red-700" onClick={() => void remove(c.id)}>
                          Delete
                        </button>
                      </div>
                    ) : (
                      <div className="flex shrink-0 gap-1">
                        <button className="rounded p-1 text-slate-400 hover:bg-white/10 hover:text-white" aria-label={`Edit ${c.provider}`} onClick={() => setEditing(c)}>
                          <Pencil className="h-3.5 w-3.5" />
                        </button>
                        <button className="rounded p-1 text-red-400 hover:bg-red-500/10" aria-label={`Delete ${c.provider}`} onClick={() => setConfirming(c.id)}>
                          <Trash2 className="h-3.5 w-3.5" />
                        </button>
                      </div>
                    ))}
                </div>
                <div className="space-y-1">
                  <Detail label="Circuit ID" value={c.circuit_ref} />
                  <Detail label="Support" value={c.support_phone} />
                  <Detail label="Account" value={c.account_number} />
                </div>
                {c.notes && <p className="whitespace-pre-wrap break-words text-xs text-slate-500">{c.notes}</p>}
                {port && (
                  <div className="space-y-1.5 border-t border-white/10 pt-2">
                    <Link to={`/network/devices/${port.device_id}/ports/${port.if_index}`} className="block truncate text-xs text-primary-400 hover:underline">
                      {port.device_name} · {portTitle(port)}
                    </Link>
                    {port.oper_status && port.oper_status !== 'up' ? (
                      <p className="text-xs font-medium text-red-400">Port down</p>
                    ) : port.in_bps == null && port.out_bps == null ? (
                      <p className="text-xs text-slate-500">No recent data</p>
                    ) : (
                      <>
                        <UsageRow label="↓ In (download)" bps={port.in_bps} mbps={c.download_mbps} />
                        <UsageRow label="↑ Out (upload)" bps={port.out_bps} mbps={c.upload_mbps} />
                      </>
                    )}
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      )}
      {editing && (
        <CircuitFormModal
          siteId={siteId}
          initial={editing === 'new' ? undefined : editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            onChanged()
          }}
        />
      )}
    </section>
  )
}
```

- [ ] **Step 4: The two-column site page**

Replace `frontend/src/pages/network/SiteDetail.tsx` with:

```tsx
import { useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { LayoutDashboard, MapPin, Pencil, Plus, Radar, Trash2 } from 'lucide-react'
import { siteAddressLines, useSite, useSiteActions } from '@/hooks/useSites'
import { useDevices } from '@/hooks/useDevices'
import { useDashboards } from '@/hooks/useDashboards'
import { useSiteProfile, useSiteProfileActions } from '@/hooks/useSiteProfile'
import SiteFormModal from '@/components/SiteFormModal'
import SiteSharingPanel from '@/components/SiteSharingPanel'
import NewDashboardModal from '@/components/dashboards/NewDashboardModal'
import DeviceTable from '@/components/network/DeviceTable'
import DeviceFilterBar from '@/components/network/DeviceFilterBar'
import { filterDevices, NO_DEVICE_FILTERS } from '@/utils/devices'
import DeviceFormModal from '@/components/network/DeviceFormModal'
import ScanModal from '@/components/network/ScanModal'
import CircuitsCard from '@/components/sites/CircuitsCard'
import NetworksCard from '@/components/sites/NetworksCard'
import NotesCard from '@/components/sites/NotesCard'
import { useSitePortSummary, usePortEvents, type PortRef } from '@/hooks/usePorts'
import SiteTrafficCharts from '@/components/network/SiteTrafficCharts'
import PortEventList from '@/components/network/PortEventList'
import { CONDITION_LABEL, formatPct, portTitle } from '@/utils/network'
import type { ApiError } from '@/services/api'

function PortRefList({ title, refs, empty, detail }: { title: string; refs: PortRef[]; empty: string; detail: (r: PortRef) => string }) {
  return (
    <section className="space-y-3">
      <h2 className="text-lg font-light text-white">{title}</h2>
      {refs.length === 0 ? (
        <p className="text-sm text-slate-500">{empty}</p>
      ) : (
        <ul className="card divide-y divide-white/10">
          {refs.map((r) => (
            <li key={`${r.device_id}-${r.if_index}`}>
              <Link to={`/network/devices/${r.device_id}/ports/${r.if_index}`} className="flex items-center justify-between gap-3 p-3 text-sm hover:bg-white/5">
                <span className="min-w-0 truncate text-slate-200">
                  {r.device_name} · {portTitle(r)}
                </span>
                <span className="shrink-0 tabular-nums text-slate-400">{detail(r)}</span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

export default function SiteDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { site, loading, notFound, refetch } = useSite(id)
  const { devices, refetch: refetchDevices } = useDevices({ siteId: id })
  const { remove, busy } = useSiteActions()
  const { data: summary } = useSitePortSummary(id)
  const { events } = usePortEvents({ siteId: id }, 15)
  const { profile, error: profileError, refetch: refetchProfile } = useSiteProfile(id)
  const profileActions = useSiteProfileActions(id ?? '')
  const [editing, setEditing] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [addingDevice, setAddingDevice] = useState(false)
  const [deviceFilters, setDeviceFilters] = useState(NO_DEVICE_FILTERS)
  const shownDevices = useMemo(() => filterDevices(devices, deviceFilters), [devices, deviceFilters])
  const [scanning, setScanning] = useState(false)
  const { dashboards } = useDashboards(id)
  const [newDashboard, setNewDashboard] = useState(false)

  if (loading) return <p className="text-sm text-slate-400">Loading…</p>

  // Missing and not-shared look the same on purpose; the API does not say which.
  if (notFound || !site) {
    return (
      <div className="card p-8 text-center">
        <p className="text-slate-300">Site not found.</p>
        <Link to="/network/sites" className="mt-2 inline-block text-sm text-primary-400 hover:underline">
          Back to sites
        </Link>
      </div>
    )
  }

  const isAdmin = site.access === 'admin'
  const canEdit = isAdmin || site.access === 'editable'
  const address = siteAddressLines(site)
  const changed = () => void refetchProfile()

  const handleDelete = async () => {
    setError(null)
    try {
      await remove(site.id)
      navigate('/network/sites')
    } catch (err) {
      setError((err as ApiError).message || 'Failed to delete site')
      setConfirmDelete(false)
    }
  }

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <Link to="/network/sites" className="text-sm text-slate-400 hover:text-slate-300">
            ← Sites
          </Link>
          <h1 className="mt-2 break-words text-4xl font-light text-white">{site.name}</h1>
          {site.description && <p className="mt-2 text-slate-400">{site.description}</p>}
          {address.length > 0 && (
            <address className="mt-2 flex items-start gap-1.5 text-sm not-italic text-slate-500">
              <MapPin className="mt-0.5 h-4 w-4 shrink-0" />
              <span>
                {address.map((line) => (
                  <span key={line} className="block">
                    {line}
                  </span>
                ))}
              </span>
            </address>
          )}
        </div>
        {isAdmin && (
          <div className="flex gap-2">
            <button className="btn-secondary flex items-center gap-2" onClick={() => setEditing(true)}>
              <Pencil className="h-4 w-4" /> Edit
            </button>
            {confirmDelete ? (
              <>
                <span className="self-center text-sm text-amber-300">
                  Its networks, circuits and notes
                  {dashboards.length > 0 &&
                    `, and its ${dashboards.length} dashboard${dashboards.length === 1 ? '' : 's'}${
                      dashboards.some((d) => d.published) ? ` (${dashboards.filter((d) => d.published).length} with a public link)` : ''
                    }`}{' '}
                  will be deleted too.
                </span>
                <button className="btn-secondary" onClick={() => setConfirmDelete(false)}>
                  Cancel
                </button>
                <button className="btn bg-red-600 text-white hover:bg-red-700" disabled={busy} onClick={() => void handleDelete()}>
                  Delete site
                </button>
              </>
            ) : (
              <button className="btn-secondary flex items-center gap-2 text-red-400" onClick={() => setConfirmDelete(true)}>
                <Trash2 className="h-4 w-4" /> Delete
              </button>
            )}
          </div>
        )}
      </div>

      {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

      <div className="grid gap-8 lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
        {/* The site's facts. */}
        <div className="min-w-0 space-y-6">
          {profileError && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{profileError}</div>}
          {profile ? (
            <>
              <CircuitsCard siteId={site.id} circuits={profile.circuits} canEdit={canEdit} onDelete={profileActions.deleteCircuit} onChanged={changed} />
              <NetworksCard siteId={site.id} networks={profile.networks} canEdit={canEdit} onDelete={profileActions.deleteNetwork} onChanged={changed} />
              <NotesCard
                notes={profile.notes}
                canEdit={canEdit}
                busy={profileActions.busy}
                onSave={async (notes) => {
                  await profileActions.saveNotes(notes)
                  changed()
                }}
              />
            </>
          ) : (
            !profileError && <p className="text-sm text-slate-400">Loading the site profile…</p>
          )}
          {isAdmin && <SiteSharingPanel siteId={site.id} />}
        </div>

        {/* What is there. */}
        <div className="min-w-0 space-y-8">
          <section className="space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h2 className="text-lg font-light text-white">Dashboards</h2>
              {canEdit && (
                <button className="btn-secondary flex items-center gap-2" onClick={() => setNewDashboard(true)}>
                  <Plus className="h-4 w-4" /> New site dashboard
                </button>
              )}
            </div>
            {dashboards.length === 0 ? (
              <p className="text-sm text-slate-500">No dashboards for this site yet.</p>
            ) : (
              <ul className="flex flex-wrap gap-2">
                {dashboards.map((d) => (
                  <li key={d.id}>
                    <Link to={`/dashboards/${d.id}`} className="card inline-flex items-center gap-2 px-3 py-2 text-sm hover:bg-white/5">
                      <LayoutDashboard className="h-4 w-4 text-teal-400" /> {d.name}
                      {d.published && <span className="text-xs text-teal-300">public</span>}
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </section>

          <section className="space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h2 className="text-lg font-light text-white">
                Devices ({shownDevices.length === devices.length ? devices.length : `${shownDevices.length} of ${devices.length}`})
              </h2>
              {canEdit && (
                <div className="flex gap-2">
                  <button className="btn-secondary flex items-center gap-2" onClick={() => setScanning(true)}>
                    <Radar className="h-4 w-4" /> Scan subnet
                  </button>
                  <button className="btn-primary flex items-center gap-2" onClick={() => setAddingDevice(true)}>
                    <Plus className="h-4 w-4" /> Add device
                  </button>
                </div>
              )}
            </div>
            {devices.length === 0 ? (
              <div className="card p-8 text-center">
                <p className="text-slate-300">No devices yet.</p>
                {canEdit && <p className="mt-1 text-sm text-slate-500">Add a device by address, or scan a subnet to find them.</p>}
              </div>
            ) : (
              <>
                <DeviceFilterBar filters={deviceFilters} onChange={setDeviceFilters} />
                {shownDevices.length === 0 ? (
                  <div className="card p-6 text-center text-sm text-slate-400">
                    No devices match these filters.{' '}
                    <button type="button" className="text-primary-400 hover:underline" onClick={() => setDeviceFilters(NO_DEVICE_FILTERS)}>
                      Clear filters
                    </button>
                  </div>
                ) : (
                  <DeviceTable devices={shownDevices} showSite={false} />
                )}
              </>
            )}
          </section>

          {devices.length > 0 && (
            <>
              <SiteTrafficCharts siteId={site.id} />
              <div className="grid gap-6 xl:grid-cols-2">
                <PortRefList title="Busiest ports" refs={summary?.busiest ?? []} empty="No traffic figures yet." detail={(r) => formatPct(r.util_pct)} />
                <PortRefList
                  title="Ports with problems"
                  refs={summary?.problems ?? []}
                  empty="Nothing wrong right now."
                  detail={(r) => (r.conditions.length ? r.conditions.map((c) => CONDITION_LABEL[c]).join(', ') : 'Link down')}
                />
              </div>
              <section className="space-y-3">
                <h2 className="text-lg font-light text-white">Recent port events</h2>
                <PortEventList events={events} showDevice />
              </section>
            </>
          )}
        </div>
      </div>

      {editing && (
        <SiteFormModal
          initial={site}
          onClose={() => setEditing(false)}
          onSaved={() => {
            setEditing(false)
            void refetch()
          }}
        />
      )}
      {addingDevice && (
        <DeviceFormModal siteId={site.id} onClose={() => setAddingDevice(false)} onSaved={() => { setAddingDevice(false); void refetchDevices() }} />
      )}
      {scanning && <ScanModal siteId={site.id} onClose={() => setScanning(false)} onAdded={() => void refetchDevices()} />}
      {newDashboard && (
        <NewDashboardModal siteId={site.id} onClose={() => setNewDashboard(false)} onCreated={(d) => navigate(`/dashboards/${d.id}/edit`)} />
      )}
    </div>
  )
}
```

- [ ] **Step 5: Run the frontend gate**

Expected: no errors. If a prop name of an existing component differs from what is used above (e.g. `usePortChoices`' return), adapt the call to the real component or hook — never change them — and note it in the report.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/sites frontend/src/pages/network/SiteDetail.tsx
git commit -m "feat(sites): circuits, networks and notes on a two-column site page

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: What is at the site, the sites list, and Scan subnet

**Files:**
- Create: `frontend/src/components/sites/SiteServersList.tsx`, `frontend/src/components/sites/SiteChecksList.tsx`
- Modify: `frontend/src/pages/network/SiteDetail.tsx` (anchors below), `frontend/src/pages/network/Sites.tsx`, `frontend/src/components/network/ScanModal.tsx`, `docs/superpowers/STATUS.md`
- Test: the frontend gate

**Interfaces:**
- Consumes: Task 4's `siteCounts`, `siteCountsText`, `scanChoices`, `STATE_DOT`; Task 5's `SiteDetail`; existing `useAllMonitors()` (`monitors`, `loading`, `error`), `useAgents()` (`agents`, `loading`, `error`), `useAgentStatus(agentId, pollMs)` (`detail.latest.cpu_percent`), `useDevices()`, `summaryCounts`, `monitorState`, `serverState`, `VIEW_STATUS_LABEL` from `@/utils/monitoringView`, `formatResponseTime` from `@/utils/formatters`.
- Produces: `<SiteServersList agents canHint />`, `<SiteChecksList monitors />`.

- [ ] **Step 1: The two compact lists**

`frontend/src/components/sites/SiteServersList.tsx`:

```tsx
import { Link } from 'react-router-dom'
import { useAgentStatus, type Agent } from '@/hooks/useAgents'
import { serverState, VIEW_STATUS_LABEL } from '@/utils/monitoringView'
import { STATE_DOT } from '@/utils/siteProfile'

function ServerLine({ agent }: { agent: Agent }) {
  const { detail } = useAgentStatus(agent.agent_id, 20000)
  const cpu = detail?.latest?.cpu_percent
  const state = serverState(agent)
  return (
    <li>
      <Link to={`/servers/${agent.agent_id}`} className="flex items-center justify-between gap-3 p-3 text-sm hover:bg-white/5">
        <span className="flex min-w-0 items-center gap-2">
          <span className={`h-2 w-2 shrink-0 rounded-full ${STATE_DOT[state]}`} title={VIEW_STATUS_LABEL[state]} aria-label={VIEW_STATUS_LABEL[state]} />
          <span className="truncate text-slate-200">{agent.name}</span>
        </span>
        <span className="shrink-0 tabular-nums text-slate-400">{cpu == null ? '—' : `CPU ${Math.round(cpu)}%`}</span>
      </Link>
    </li>
  )
}

/** The servers whose site is this site. canHint: the viewer can set a
 *  server's site (admins). */
export default function SiteServersList({ agents, canHint }: { agents: Agent[]; canHint: boolean }) {
  return (
    <section className="space-y-3">
      <h2 className="text-lg font-light text-white">Servers ({agents.length})</h2>
      {agents.length === 0 ? (
        <p className="text-sm text-slate-500">
          None at this site.{canHint && ' A server’s site is set in its Edit form.'}
        </p>
      ) : (
        <ul className="card divide-y divide-white/10">
          {agents.map((a) => (
            <ServerLine key={a.id} agent={a} />
          ))}
        </ul>
      )}
    </section>
  )
}
```

`frontend/src/components/sites/SiteChecksList.tsx`:

```tsx
import { Link } from 'react-router-dom'
import type { Monitor } from '@/types'
import { monitorState, VIEW_STATUS_LABEL } from '@/utils/monitoringView'
import { formatResponseTime } from '@/utils/formatters'
import { STATE_DOT } from '@/utils/siteProfile'

/** The uptime checks whose site is this site that the viewer can see. */
export default function SiteChecksList({ monitors }: { monitors: Monitor[] }) {
  return (
    <section className="space-y-3">
      <h2 className="text-lg font-light text-white">Uptime checks ({monitors.length})</h2>
      {monitors.length === 0 ? (
        <p className="text-sm text-slate-500">None at this site. A monitor’s site is set in its edit form.</p>
      ) : (
        <ul className="card divide-y divide-white/10">
          {monitors.map((m) => {
            const state = monitorState(m)
            return (
              <li key={m.id}>
                <Link to={`/monitors/${m.id}`} className="flex items-center justify-between gap-3 p-3 text-sm hover:bg-white/5">
                  <span className="flex min-w-0 items-center gap-2">
                    <span className={`h-2 w-2 shrink-0 rounded-full ${STATE_DOT[state]}`} title={VIEW_STATUS_LABEL[state]} aria-label={VIEW_STATUS_LABEL[state]} />
                    <span className="truncate text-slate-200">{m.name}</span>
                  </span>
                  <span className="shrink-0 tabular-nums text-slate-400">
                    {m.last_check_at ? formatResponseTime(m.last_response_time_ms) : '—'}
                  </span>
                </Link>
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}
```

- [ ] **Step 2: Wire them into the site page**

In `frontend/src/pages/network/SiteDetail.tsx`:

1. Add imports:

```tsx
import { useAllMonitors } from '@/hooks/useAllMonitors'
import { useAgents } from '@/hooks/useAgents'
import SiteServersList from '@/components/sites/SiteServersList'
import SiteChecksList from '@/components/sites/SiteChecksList'
import { summaryCounts } from '@/utils/monitoringView'
```

2. Directly after the line `const profileActions = useSiteProfileActions(id ?? '')`, add:

```tsx
  const { monitors } = useAllMonitors()
  const { agents } = useAgents()
  const siteMonitors = useMemo(() => monitors.filter((m) => m.site_id === id), [monitors, id])
  const siteAgents = useMemo(() => agents.filter((a) => a.site_id === id), [agents, id])
  const counts = useMemo(() => summaryCounts(siteMonitors, siteAgents, devices), [siteMonitors, siteAgents, devices])
```

3. Directly after the closing `)}` of the `{address.length > 0 && (` block in the header, add the summary line:

```tsx
          <p className="mt-3 text-sm text-slate-400">
            <span className="font-semibold tabular-nums text-white">{counts.watched}</span> watched ·{' '}
            <span className={`font-semibold tabular-nums ${counts.down > 0 ? 'text-red-400' : 'text-white'}`}>{counts.down}</span> down ·{' '}
            <Link to={`/monitoring?site=${site.id}`} className="text-primary-400 hover:underline">
              Open in Monitoring
            </Link>
          </p>
```

4. Directly after the Devices `</section>` in the right column (before `{devices.length > 0 && (`), add:

```tsx
          <SiteServersList agents={siteAgents} canHint={isAdmin} />
          <SiteChecksList monitors={siteMonitors} />
```

5. Change the `ScanModal` line to pass the saved networks:

```tsx
      {scanning && (
        <ScanModal siteId={site.id} networks={profile?.networks ?? []} onClose={() => setScanning(false)} onAdded={() => void refetchDevices()} />
      )}
```

- [ ] **Step 3: Saved networks in Scan subnet**

In `frontend/src/components/network/ScanModal.tsx`:

1. Import `import { scanChoices } from '@/utils/siteProfile'`.
2. Change the signature to:

```tsx
export default function ScanModal({
  siteId,
  networks = [],
  onClose,
  onAdded,
}: {
  siteId: string
  /** The site's saved networks; the scannable ones are offered as one-click choices. */
  networks?: { name: string; cidr: string }[]
  onClose: () => void
  onAdded: () => void
}) {
```

3. Inside the component, after the `cidr` state, add `const choices = scanChoices(networks)`.
4. Inside the Subnet `<label>`, directly before its `<input>`, add:

```tsx
              {choices.length > 0 && (
                <div className="flex flex-wrap gap-1.5 pb-1" aria-label="Saved networks">
                  {choices.map((n) => (
                    <button
                      key={n.cidr}
                      type="button"
                      onClick={() => setCidr(n.cidr)}
                      className={`rounded-full px-2.5 py-1 text-xs transition ${
                        cidr === n.cidr ? 'bg-primary-600 text-white' : 'bg-slate-800 text-slate-300 hover:bg-slate-700'
                      }`}
                    >
                      {n.name} · {n.cidr}
                    </button>
                  ))}
                </div>
              )}
```

- [ ] **Step 4: The sites list status line**

In `frontend/src/pages/network/Sites.tsx`:

1. Add imports:

```tsx
import { useAllMonitors } from '@/hooks/useAllMonitors'
import { useAgents } from '@/hooks/useAgents'
import { useDevices } from '@/hooks/useDevices'
import { siteCounts, siteCountsText } from '@/utils/siteProfile'
```

2. After `const [adding, setAdding] = useState(false)`, add:

```tsx
  // A list that has not loaded, or failed, is left out of each card's line
  // rather than counted as zero.
  const monitorsQ = useAllMonitors()
  const agentsQ = useAgents()
  const devicesQ = useDevices()
  const lists = {
    monitors: monitorsQ.loading || monitorsQ.error ? null : monitorsQ.monitors,
    agents: agentsQ.loading || agentsQ.error ? null : agentsQ.agents,
    devices: devicesQ.loading || devicesQ.error ? null : devicesQ.devices,
  }
```

3. Inside each site card, after the address `<p>` block (the `siteAddressLines(s).length > 0 &&` one), add:

```tsx
              {(() => {
                const counts = siteCounts(s.id, lists)
                const text = siteCountsText(counts)
                return (
                  text && (
                    <p className="mt-3 text-xs text-slate-400">
                      {text} · <span className={counts.down > 0 ? 'text-red-400' : ''}>{counts.down} down</span>
                    </p>
                  )
                )
              })()}
```

- [ ] **Step 5: Run the frontend gate**

Expected: no errors.

- [ ] **Step 6: Record it in STATUS.md**

In `docs/superpowers/STATUS.md`, set the UX reorganization table's piece 3 row's state to `Done (\`feature/ux-piece3\`, awaiting merge to \`dev\`); plan \`plans/2026-10-08-ux-piece3-site-profiles.md\``, and under "To be checked by the owner" add a section "UX reorganization piece 3 (on the dev stack)" with the nine owner checks copied from the spec's "Owner checks (on the dev stack)".

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/sites/SiteServersList.tsx frontend/src/components/sites/SiteChecksList.tsx frontend/src/pages/network/SiteDetail.tsx frontend/src/pages/network/Sites.tsx frontend/src/components/network/ScanModal.tsx docs/superpowers/STATUS.md
git commit -m "feat(sites): servers, checks and counts on site pages; saved networks in Scan

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

- [ ] **Step 8: Whole-branch verification**

From `backend/`: `go vet ./...`, `go test ./...`, `./scripts/test-db.sh` (the restore tests must PASS, not SKIP). The frontend gate.
Expected: all PASS.
