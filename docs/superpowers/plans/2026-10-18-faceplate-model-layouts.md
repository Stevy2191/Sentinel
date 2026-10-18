# Faceplate Layouts per Model Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Admins fix a switch model's faceplate once, from the device page, and every device of that model is drawn that way.

**Architecture:** `portmon` gains a layout "group" type, shared by the built-in table and saved layouts, and one drawing path for both. A new `faceplate_layouts` table is cached in memory by `FaceplateLayoutService`. `PortService` feeds the saved layout into `Layout` and serves the editor's start state and preview. Admin-only routes, audit and the restore reload sit in a new handler file. The frontend adds an editor modal on the device page and a Settings section.

**Tech Stack:** Go (gin, gorm, PostgreSQL/TimescaleDB), React + TypeScript + Tailwind.

**Spec:** `docs/superpowers/specs/2026-10-18-faceplate-model-layouts-design.md`

## Global Constraints

- Group fields and values: `first`, `last`, `kind` (`sfp` | `rj45`), `rows` (`1` | `2`), `numbering` (`alternate` | `across`, two-row groups only).
- Group limits:
  - 1–16 groups
  - at most 512 ports in total
  - port numbers 1–9999, `first ≤ last`
  - no overlapping ranges
  - `numbering` required when `rows` is 2, ignored when it's 1
- Order of choice:
  1. the device's own settings (rows, style or SFP ports set), then the automatic rules
  2. the saved layout
  3. the built-in table
  4. the automatic rules
- A saved or built-in layout applies only when it places the device's main ports exactly. Module blocks are always appended after it.
- Model matching: trim, collapse whitespace to one space, upper-case. Exact equality.
- Migration: next free number, `062_faceplate_layouts.sql` if still free. `TIMESTAMPTZ` only.
- Every route is admin-only, through `RequireAdmin`, which aborts. Tests must assert that the handler did not run.
- Audit actions are `faceplate_layout_saved` and `faceplate_layout_deleted`; the resource type is `faceplate_layout`.
- Devices without a saved layout draw exactly as today. Every existing `portmon` test passes unchanged, except the `14c790d` built-in test that Task 1 updates.
- Frontend gate (repo root): `docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"`
- DB tests: `cd backend && ./scripts/test-db.sh -run <Name> -v` (needs Docker). Every `./scripts/test-db.sh` below runs from `backend/`.

## Review Focus

1. **A stack whose members differ in port count.** A saved layout that fits unit 1 but not unit 2 must draw unit 2 automatically and raise the notice. Test in Task 3: `TestDevicePortsSavedLayoutStackMisfit`.
2. **A model typed with different case or spacing in Edit details.** `" ubnt-es16xg "` must find the `UBNT-ES16XG` layout. Test in Task 2: `TestDBFaceplateLayoutNormalizedModel`.
3. **Saving a second time for the same model.** It replaces the first, with no unique-key error, and audit records before and after. Test in Task 4: `TestDBFaceplateLayoutReplaceAudited`.
4. **A layout's model changed underneath it** (the device's model edited after saving). The preview and save use the device's model at that moment, and the old layout stays for its own model. Test in Task 2: `TestDBFaceplateLayoutModelChange`.
5. **A body with `numbering` on a one-row group, or a missing `kind`.** One-row `numbering` is ignored; a missing `kind` is refused with a clear message, not stored. Test in Task 1: `TestValidateGroups`.

---

### Task 1: Layout groups in `portmon`

**Files:**
- Create: `backend/internal/portmon/groups.go`
- Modify: `backend/internal/portmon/models.go` (built-in table on groups; exported lookups)
- Modify: `backend/internal/portmon/layout.go` (`LayoutOptions.Saved`, the order of choice, modules after a layout)
- Test: `backend/internal/portmon/groups_test.go`, `backend/internal/portmon/layout_test.go`

**Interfaces:**
- Produces:
  - `type LayoutGroup struct { First int \`json:"first"\`; Last int \`json:"last"\`; Kind string \`json:"kind"\`; Rows int \`json:"rows"\`; Numbering string \`json:"numbering,omitempty"\` }`
  - consts `GroupKindSFP = "sfp"`, `GroupKindRJ45 = "rj45"`, `NumberingAlternate = "alternate"`, `NumberingAcross = "across"`
  - `func ValidateGroups(gs []LayoutGroup) error`
  - `func NormalizeModel(s string) string`
  - `type Fit struct { Missing []int \`json:"missing"\`; Extra []int \`json:"extra"\` }`, with `func (f Fit) OK() bool`. Duplicates count as Extra.
  - `func CheckFit(gs []LayoutGroup, main []int) Fit` (both lists sorted ascending, never nil)
  - `func MainPortNumbers(ports []LayoutPort) []int`: the main ports' numbers, by `Layout`'s own definition of main (lowest slot; on slotted devices, ports with no slot name dropped)
  - `func FitUnits(ports []LayoutPort, gs []LayoutGroup) *Fit`: the first stack unit (by `LayoutUnits`' grouping and order) whose main ports don't fit, else nil
  - `type BuiltinLayout struct { Name string; Groups []LayoutGroup }` with `func (b BuiltinLayout) Matches(model string) bool`; `func BuiltinLayouts() []BuiltinLayout`; `func BuiltinFor(model string) *BuiltinLayout`
  - `LayoutOptions.Saved []LayoutGroup` (nil means none)
  - `LayoutOptions.IgnoreDeviceSettings bool` (the preview sets it)
  - `func GroupsFromFaceplate(fp Faceplate) []LayoutGroup`: the starting layout from an automatic drawing

- [ ] **Step 1: Write failing tests in `groups_test.go`**
  - `TestValidateGroups`, table-driven:
    - valid ES-16-XG groups → nil
    - `[]` → error containing "at least one group"
    - 17 groups → "at most 16 groups"
    - `first 5 last 3` → "first port"
    - port 0 or 10000 → "1 to 9999"
    - overlapping 1–8 and 8–12 → "overlap"
    - 513 ports → "at most 512 ports"
    - `kind ""` → "SFP or RJ45"
    - `rows 3` → "one or two rows"
    - `rows 2, numbering ""` → "numbering"
    - `rows 1, numbering "across"` → nil
  - `TestNormalizeModel`: `" ubnt-es16xg "` → `"UBNT-ES16XG"`; `"EdgeSwitch  16   XG"` → `"EDGESWITCH 16 XG"`; `""` → `""`.
  - `TestCheckFit`: ES-16-XG groups against 1..16 → OK; against 1..15 → `Extra [16]`; against 1..17 → `Missing [17]`; an overlap counted twice → its number in Extra.
  - `TestGroupNumbering`, drawing one group through `Layout` with `Saved`:
    - `alternate`, ports 1–12: top 1,3,5,7,9,11 and bottom 2,4,…,12
    - `across`, ports 1–12: top 1–6, bottom 7–12
    - `across`, ports 1–7: top 1–4, bottom 5–7
  - `TestGroupsFromFaceplate`: on `uswLayoutPorts()` automatic drawing → five groups:
    - 1–12, 13–24, 25–36 and 37–48: rj45, 2 rows, alternate
    - 49–52: sfp, 2 rows, alternate
  - `TestGroupsFromFaceplateSplitsJumps`: a one-row block numbered 1,2,3,7,8 → groups 1–3 and 7–8.

- [ ] **Step 2: Add failing tests to `layout_test.go`**
  - `TestLayoutSavedOverridesBuiltin`: ES-16-XG ports, Model `UBNT-ES16XG`, Saved one group 1–16 rj45 2 alternate → one block, `SFP == false`.
  - `TestLayoutSavedMisfitFallsBack`: Saved 1–20 → the built-in ES-16-XG drawing (two blocks).
  - `TestLayoutDeviceSettingsBeatSaved`: Rows set and Saved set → no block has `Rows != 0`. With `IgnoreDeviceSettings: true` → the saved drawing.
  - `TestLayoutModulesAfterLayout`: the Cisco 4500-X fixture with Saved covering the main ports, one group of 16 SFP, 2 rows, alternate → the module blocks still follow, labelled "Module 2".
  - Update `TestLayoutModelFallsBack`: no change for the built-in. Its `14c790d` behaviour on modules is replaced by `TestLayoutModulesAfterLayout`.

- [ ] **Step 3: Run the tests and see them fail**

  Run: `cd backend && go test ./internal/portmon/`. Expected: build failure (undefined `LayoutGroup`, …).

- [ ] **Step 4: Implement**
  - `groups.go` holds the types, `ValidateGroups`, `NormalizeModel`, `CheckFit`, `MainPortNumbers`, `FitUnits`, `GroupsFromFaceplate`, and `groupBlocks(gs []LayoutGroup, main []FacePort) []FaceBlock`.
    - Each group block sets `Rows`.
    - `across` puts `ceil(n/2)` ports on top.
  - `models.go`: the table becomes `[]struct{ BuiltinLayout; pattern *regexp.Regexp }`, with the ES-16-XG entry named `UBNT-ES16XG`. `modelFaceplate` is replaced by `groupBlocks` and `CheckFit`.
  - `Layout`:
    - after splitting main and module ports, use the device's settings if they're set and `!IgnoreDeviceSettings`
    - otherwise try `Saved`, then `BuiltinFor(o.Model)`, each only when `CheckFit(...).OK()`
    - otherwise use the automatic rules
    - in every case, module blocks are appended with panel rows = the max of the group rows (or the automatic rows)
  - `MainPortNumbers` must share `Layout`'s main/module split. Extract it as `mainAndModules(ports) (main []LayoutPort, modules map[int]map[int]LayoutPort)` and use it in both.

- [ ] **Step 5: Run the tests and see them pass**

  Run: `cd backend && gofmt -l ./internal/portmon; go vet ./internal/portmon/ && go test ./internal/portmon/`. Expected: no gofmt output, `ok`.

- [ ] **Step 6: Commit** `feat(network): faceplate layout groups shared by built-in and saved layouts`

### Task 2: The `faceplate_layouts` table and `FaceplateLayoutService`

**Files:**
- Create: `backend/migrations/062_faceplate_layouts.sql` (the spec's SQL, verbatim)
- Create: `backend/internal/models/faceplate_layout.go`
- Create: `backend/internal/services/faceplate_layout_service.go`
- Modify: `backend/internal/models/audit.go` (three constants)
- Test: `backend/internal/services/faceplate_layout_service_db_test.go`; append to `backup_metrics_db_test.go`

**Interfaces:**
- Consumes: `portmon.LayoutGroup`, `ValidateGroups`, `NormalizeModel`, `BuiltinLayouts`.
- Produces:
  - `models.FaceplateLayout{ID uuid.UUID; Model, ModelKey string; Groups FaceplateGroups; CreatedBy, UpdatedBy *uuid.UUID; CreatedAt, UpdatedAt time.Time}` with `TableName() "faceplate_layouts"`. `type FaceplateGroups []portmon.LayoutGroup` implements `driver.Valuer` / `sql.Scanner` as JSON.
  - `models.ActionFaceplateLayoutSaved = "faceplate_layout_saved"`, `ActionFaceplateLayoutDeleted = "faceplate_layout_deleted"`, `ResourceFaceplateLayout = "faceplate_layout"`
  - `services.ErrFaceplateLayoutNotFound`; `services.ErrFaceplateLayoutNoModel` (message: "Set this device's model in Edit details first")
  - `func NewFaceplateLayoutService(db *gorm.DB) *FaceplateLayoutService`
  - `func (s) Load(ctx) error`: reloads the in-memory copy (guarded by `sync.RWMutex`)
  - `func (s) For(model string) *models.FaceplateLayout`: nil when none
  - `func (s) DeviceCount(ctx, model string) (int, error)`: devices whose normalized effective model equals `NormalizeModel(model)`
  - `func (s) List(ctx) ([]FaceplateLayoutView, error)`: saved rows by model, then built-ins that no saved row overrides. `FaceplateLayoutView{ID *uuid.UUID; Model string; Groups []portmon.LayoutGroup; Source string ("saved"|"builtin"); Devices int; PreviewDeviceID *uuid.UUID (the matching device with the latest last_polled_at); UpdatedAt *time.Time; UpdatedBy string (username)}`. Built-in counts use `BuiltinLayout.Matches` on each device's effective model.
  - `func (s) Save(ctx, model string, groups []portmon.LayoutGroup, actor uuid.UUID, clearDevice *uuid.UUID) (before, after *models.FaceplateLayout, err error)`
    - validates, and refuses a blank model with `ErrFaceplateLayoutNoModel`
    - upserts on `model_key`; `before` is nil when it's new
    - when `clearDevice` is set, nulls that device's `faceplate_rows`, `faceplate_sfp_ports` and `faceplate_port_style` in the same transaction
    - then runs `Load`
  - `func (s) Delete(ctx, id uuid.UUID) (*models.FaceplateLayout, error)`, then `Load`

- [ ] **Step 1: Write failing DB tests**
  - `TestDBFaceplateLayoutSaveListDelete`:
    - save `UBNT-ES16XG` with two seeded devices of that model
    - `For("ubnt-es16xg")` is non-nil
    - `List` has the saved row first with `Devices 2`, and no built-in row for that model
    - `Delete`, then `For` is nil and `List` shows the built-in row with `Devices 2`
  - `TestDBFaceplateLayoutReplace`: save twice for one model → one row; `before` is non-nil the second time.
  - `TestDBFaceplateLayoutClearsDeviceSettings`: a device with rows 2, style `rj45` and SFP ports `{1}`; Save with `clearDevice` → all three are NULL. With invalid groups → error and the settings are untouched.
  - `TestDBFaceplateLayoutNormalizedModel` (Review Focus 2): a device with `model_override ' ubnt-es16xg '` counts toward `UBNT-ES16XG`.
  - `TestDBFaceplateLayoutModelChange` (Review Focus 4): save for `A`, change the device's model to `B`, `DeviceCount("A") == 0`, and `For("A")` is still non-nil.
  - `TestDBFaceplateLayoutNoModel`: `Save(ctx, "  ", …)` returns `ErrFaceplateLayoutNoModel`.
  - `TestDBRestoreKeepsFaceplateLayouts`, beside `TestDBRestoreWithMetrics` and with the same skip and dump steps:
    - save a layout, dump, delete the row with SQL, restore
    - the row is back, and after `Load`, `For` finds it

- [ ] **Step 2: Run them and see them fail**

  Run: `./scripts/test-db.sh -run 'FaceplateLayout' -v`. Expected: build failure.

- [ ] **Step 3: Implement** the migration, model, constants and service, as in the Interfaces block above.

- [ ] **Step 4: Run them and see them pass**

  Run: `./scripts/test-db.sh -run 'FaceplateLayout|RestoreKeeps' -v`. Expected: PASS.

- [ ] **Step 5: Commit** `feat(network): saved faceplate layouts per model, cached and restored`

### Task 3: `PortService` uses saved layouts; editor start and preview

**Files:**
- Modify: `backend/internal/services/port_service.go:276-304` (`DevicePorts`)
- Create: `backend/internal/services/faceplate_editor.go`
- Test: `backend/internal/services/faceplate_editor_db_test.go`

**Interfaces:**
- Consumes: Task 1 (`Saved`, `IgnoreDeviceSettings`, `FitUnits`, `MainPortNumbers`, `GroupsFromFaceplate`, `BuiltinFor`), Task 2 (`For`).
- Produces:
  - `type FaceplateLayoutLookup interface { For(model string) *models.FaceplateLayout }`; `func (s *PortService) SetFaceplateLayouts(l FaceplateLayoutLookup)`. When it isn't set, `DevicePorts` behaves as today, so existing constructors and tests stay unchanged.
  - `DevicePortsView.LayoutNotice *FaceplateLayoutNotice \`json:"layout_notice,omitempty"\``, where `FaceplateLayoutNotice{Model string; Fit portmon.Fit}`. It's set only when a saved layout exists, the device has no own settings, and `FitUnits` reports a misfit.
  - `func (s *PortService) FaceplateEditor(ctx, d *DeviceView) (*FaceplateEditorView, error)`. `FaceplateEditorView{Model string; Saved *models.FaceplateLayout; Builtin []portmon.LayoutGroup; Ports []int; DeviceSettings bool; Start []portmon.LayoutGroup}`.
    - `Ports` is the first unit's `MainPortNumbers`.
    - `Start` is the saved groups, else the built-in ones, else `GroupsFromFaceplate` of the first unit's automatic drawing (drawn with `IgnoreDeviceSettings` and no `Saved`).
  - `func (s *PortService) PreviewFaceplate(ctx, d *DeviceView, gs []portmon.LayoutGroup) (*FaceplatePreview, error)`. `FaceplatePreview{Faceplates []portmon.UnitFaceplate; Fit portmon.Fit}`.
    - A `ValidateGroups` error is returned wrapped in `ErrFaceplateGroupsInvalid`.
    - It draws with `Saved: gs, IgnoreDeviceSettings: true`.
    - `Fit` is `FitUnits` (a zero Fit when nil).

- [ ] **Step 1: Write failing DB tests**, seeding a device and interfaces the way `site_profile_db_test.go` and `port_service` tests do:
  - `TestDevicePortsUsesSavedLayout`: 16 ports `0/1…0/16`, model `UBNT-ES16XG`, a saved one-group rj45 layout → faceplate has one non-SFP block; `LayoutNotice` nil.
  - `TestDevicePortsSavedLayoutStackMisfit` (Review Focus 1): units 1 and 2, with unit 2 missing a port → unit 1 drawn by the saved layout, unit 2 automatically, and `LayoutNotice.Fit.Extra` lists the missing number.
  - `TestDevicePortsNoticeHiddenByDeviceSettings`: same misfit, but the device has rows set → `LayoutNotice` nil.
  - `TestFaceplateEditorStart`: no saved layout and a US-48-shaped device → `Start` has 5 groups. With a saved layout → `Start` equals the saved groups, and `DeviceSettings` reflects the columns.
  - `TestPreviewFaceplate`: invalid groups → `errors.Is(err, ErrFaceplateGroupsInvalid)`. Groups missing port 16 → `Fit.Missing == [16]`. And the preview ignores the device's own rows setting.

- [ ] **Step 2: Run them and see them fail**

  Run: `./scripts/test-db.sh -run 'DevicePortsUsesSaved|SavedLayoutStack|NoticeHidden|FaceplateEditor|PreviewFaceplate' -v`

- [ ] **Step 3: Implement** as in the Interfaces block. `DevicePorts` passes `Saved` from `For(d.EffectiveModel)` (its `Groups`).

- [ ] **Step 4: Run them and see them pass**, plus `go test ./internal/services/` for the non-DB tests.

- [ ] **Step 5: Commit** `feat(network): faceplates use saved layouts; editor start and preview`

### Task 4: Routes, audit, restore reload and wiring

**Files:**
- Create: `backend/internal/api/faceplate_layout_handler.go`
- Modify: `backend/internal/api/backup_handler.go` (a `registryLoaders` slice type)
- Modify: `backend/cmd/sentinel/main.go` (construct, `Load` at startup next to `profileService.Load` near line 475, `portService.SetFaceplateLayouts`, register routes, pass both loaders to the restore handler)
- Test: `backend/internal/api/faceplate_layout_handler_test.go` (fakes); `backend/internal/api/faceplate_layout_db_test.go`

**Interfaces:**
- Consumes: Task 2 service methods; Task 3 `FaceplateEditor` / `PreviewFaceplate`.
- Produces:
  - `func RegisterFaceplateLayoutRoutes(rg *gin.RouterGroup, layouts faceplateLayoutStore, ports faceplateEditor, devices deviceStore, sites siteAccessChecker, audit auditRecorder, users adminChecker)`, mounting:
    - `GET /network/faceplate-layouts` (admin group)
    - `DELETE /network/faceplate-layouts/:id`
    - `GET /devices/:id/faceplate-layout` (admin)
    - `POST /devices/:id/faceplate-preview`
    - `PUT /devices/:id/faceplate-layout`
    - Each device route uses `RequireAdmin(users)`, then `loadDevice(c, devices, sites, services.SiteAccessReadonly)`.
  - GET editor response: `FaceplateEditorView` plus `devices` (from `DeviceCount`).
  - PUT body: `{groups: LayoutGroup[], clear_device_settings: bool}`.
    - 400 with `ErrFaceplateLayoutNoModel`'s message for a blank model.
    - 400 for invalid groups.
    - 400 `"the layout must place every port of this device: not placed 17; not on this device 15"` when the fit check fails. Omit an empty half.
    - 200 with the saved layout.
    - Audit `faceplate_layout_saved`, resource `faceplate_layout` and the layout id. Before and after are `{model, groups}`; the summary is `{device_id, device_name}`.
  - DELETE: 404 when it isn't found; audit `faceplate_layout_deleted` with `before`.
  - `type registryLoaders []registryLoader` with `Load(ctx) error`, which runs each loader and joins the errors. `RestoreBackupHandler` keeps its signature; main passes `registryLoaders{profileService, faceplateLayouts}`. Update the log text to "reloading in-memory state".

- [ ] **Step 1: Write failing tests**
  - `TestFaceplateLayoutRoutesAdminOnly` (fakes): as a non-admin, each of the 5 routes returns 403 and the fake records **no call**.
  - `TestDBFaceplateLayoutReplaceAudited` (Review Focus 3):
    - PUT twice for one device → both 200, one row
    - the second audit entry has `before.groups` from the first save
  - `TestDBFaceplateLayoutPutRefusesMisfit`: a layout covering ports 1–15 for a 16-port device → 400 naming "not placed 16". No row is written and there's no audit entry.
  - `TestDBFaceplateLayoutPutClearsSettings`: `clear_device_settings: true` → the device's three columns are NULL.
  - `TestDBFaceplateLayoutDelete`: 200, then 404 the second time; audit `faceplate_layout_deleted`.

- [ ] **Step 2: Run them and see them fail**

  Run: `cd backend && go test ./internal/api/ -run FaceplateLayoutRoutes`, then `./scripts/test-db.sh -run 'DBFaceplateLayout(Replace|Put|Delete)' -v`

- [ ] **Step 3: Implement the handlers and the wiring.**

- [ ] **Step 4: Run them and see them pass**, then the full `go vet ./... && go test ./...` and `./scripts/test-db.sh` (the full suite, once).

- [ ] **Step 5: Commit** `feat(network): faceplate layout routes, audit and restore reload`

### Task 5: Frontend data layer and the editor on the device page

**Files:**
- Create: `frontend/src/hooks/useFaceplateLayouts.ts`
- Create: `frontend/src/components/network/FaceplateLayoutEditor.tsx`
- Modify: `frontend/src/hooks/usePorts.ts` (`DevicePorts.layout_notice?`)
- Modify: `frontend/src/pages/network/DeviceDetail.tsx:258-273` (Fix layout button, notice, `?fixLayout=1` opens the editor)

**Interfaces:**
- Consumes: the Task 4 routes and JSON.
- Produces:
  - TypeScript types `LayoutGroup`, `LayoutFit`, `FaceplateEditorData`, `FaceplatePreviewData` and `FaceplateLayoutRow`, mirroring the Go JSON.
  - `useFaceplateEditor(deviceId: string | null)` → `{ data, loading, error, refetch }`
  - `previewFaceplate(deviceId, groups): Promise<FaceplatePreviewData>`
  - `saveFaceplateLayout(deviceId, groups, clearDeviceSettings): Promise<void>`
  - `deleteFaceplateLayout(id): Promise<void>`
  - `useFaceplateLayouts()` → `{ data: FaceplateLayoutRow[] | null, loading, error, refetch }`
  - `<FaceplateLayoutEditor device={Device} ports={PortView[]} onClose={() => void} onSaved={() => void} />`

- [ ] **Step 1: Hooks and types**, using the `api` client and `ApiResponse` as `usePorts.ts` does.

- [ ] **Step 2: The editor modal**, using the modal shell of `EditDetailsModal.tsx` with `max-w-3xl`.
  - **Header:** "Layout for **{model}** — used by {n} device(s)".
  - **One row per group:**
    - Ports from–to (number inputs)
    - Type select SFP / RJ45
    - Rows select 1 / 2
    - Numbering select "Odd on top" / "Across" (shown only when Rows is 2)
    - ↑ ↓ Remove, as icon buttons with aria-labels
  - **"+ Add group":** from the lowest unplaced port to the device's highest port; RJ45, 2 rows, alternate.
  - **Preview:** `<Faceplate>` fed by `previewFaceplate`, debounced 300 ms after the last change, with `selected={null}`. While a request is in flight the last preview stays.
  - **Fit line**, exact copy:
    - "Not placed: 17, 18"
    - "Not on this device: 15" (both shown when both apply)
    - otherwise "All {n} ports placed"
    - a server validation error shown in its place
  - **Checkbox** (only when `DeviceSettings`), ticked by default: "Also clear this device's own faceplate settings, which would otherwise override this layout".
  - **Buttons:**
    - "Save for all {model} devices", disabled unless the fit is OK and nothing is in flight
    - "Remove saved layout", only when `Saved`, behind a confirm "Remove the saved layout for {model}? Devices of this model go back to the built-in or automatic drawing."
    - "Cancel"
  - A save error is shown in the modal.

- [ ] **Step 3: The device page**
  - Admins (`currentUser?.is_admin`) get a "Fix layout" text button next to "Front panel". It's disabled with `title="Set this device's model in Edit details first"` when `!device.effective_model`.
  - When `portsView.layout_notice` is set and the user is an admin, show under the faceplate: "The saved layout for {model} doesn't fit this device (not placed: …; not on this device: …)." with a "Fix layout" link.
  - `?fixLayout=1` opens the editor once and then removes the parameter.
  - On save: close, `refetchPorts()` and `refetch()`.

- [ ] **Step 4: Run the frontend gate.** Expected: no output from tsc or eslint, and the build succeeds.

- [ ] **Step 5: Commit** `feat(network): Fix layout editor with live preview on the device page`

### Task 6: Settings → Faceplate layouts

**Files:**
- Create: `frontend/src/pages/settings/FaceplateLayouts.tsx`
- Modify: `frontend/src/utils/settingsNav.ts` (after `profiles`: `{ key: 'faceplate-layouts', label: 'Faceplate layouts', path: '/settings/network/faceplate-layouts', group: 'Network', adminOnly: true }`)
- Modify: `frontend/src/App.tsx` (`<Route path="network/faceplate-layouts" element={<FaceplateLayouts />} />` beside `network/profiles`)

**Interfaces:** consumes `useFaceplateLayouts` and `deleteFaceplateLayout` from Task 5.

- [ ] **Step 1: The page**, styled like `pages/network/Profiles.tsx`.
  - **Columns:**
    - Model
    - Groups: e.g. "1–12 SFP ×2 · 13–16 RJ45", where "×2" marks two rows and "across" adds " across"
    - Devices
    - Updated: date and by whom, "—" for built-in rows
    - Source: "Saved" / "Built in"
  - **Edit:** navigates to `/network/devices/{preview_device_id}?fixLayout=1`. Disabled with `title="No device of this model to preview on"` when there's no `preview_device_id`.
  - **Delete:** saved rows only, confirmed as in Task 5.
  - **Empty state, exact copy:** "No saved layouts. Open a device's faceplate and choose Fix layout to make one." Shown when no row is saved (built-in rows still list below it).

- [ ] **Step 2: Run the frontend gate.**

- [ ] **Step 3: Commit** `feat(network): Settings section listing faceplate layouts`

### Task 7: Sandbox, review, status

- [ ] **Step 1:** Deploy the branch to the sandbox:

  ```bash
  git -C /srv/docker/sentinel-dev fetch /home/sysadmin/sentinel-faceplate feature/faceplate-layout-editor:feature/faceplate-layout-editor
  git -C /srv/docker/sentinel-dev checkout feature/faceplate-layout-editor
  cd /srv/docker/sentinel-dev && docker compose up -d --build
  ```

  Expected: the backend is healthy, and `docker compose logs backend | grep 062_faceplate_layouts` shows the migration applied.

- [ ] **Step 2:** Render the editor with the throwaway harness used for `14c790d`, and check the QuantumLink start state against the spec's owner check 2.

- [ ] **Step 3:** One fresh reviewer (most capable model) reads the branch diff against the spec. Fix each finding test-first.

- [ ] **Step 4:** In `docs/superpowers/STATUS.md`, add a line under the UX/network section: "Faceplate layouts per model — on `feature/faceplate-layout-editor` / `dev`", plus the spec's owner checks 1–6 under "To be checked by the owner".

- [ ] **Step 5: Commit** `docs: faceplate layouts status and owner checks`. Pushing waits for the owner.
