# Realistic Faceplates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Draw the UDM-SE, USW-Pro-48-PoE and ES-16-XG as their real front panels, with every faceplate feature kept, dark when Sentinel can't reach the device.

**Architecture:**
- `portmon` gains a drawing table. `LayoutUnits` tags each stack unit with a drawing key when the model matches and its ports fit exactly.
- A small `DriveBayMonitor`, run by the device poller, reads UCD-DISKIO-MIB for drawings that have a drive bay. `PortService` serves its state, plus the device's reachability, on the ports view. The dashboard port grid copies both.
- The frontend has shared port parts, one component per model, and a `RealisticFaceplate` wrapper (scale, dark state, badge, fallback). It's used on the device page (with a Realistic | Simple switch) and in the port grid widget.

**Tech Stack:** Go (gin, gorm, gosnmp), React + TypeScript; plain CSS for the drawings.

**Spec:** `docs/superpowers/specs/2026-10-18-realistic-faceplates-design.md`. Mockups: `docs/superpowers/specs/assets/2026-10-18-realistic-faceplates/{udm-se,usw-pro-48-poe,es-16-xg}.html`. They are the visual reference; where the look and the text disagree, the mockup wins.

## Global Constraints

- **Drawing keys and models** (matched on `NormalizeModel`):
  - `udm-se`: `UDM-SE`, ports 1–11
  - `usw-pro-48-poe`: `UBNT-US48PRO-POE` or `USW-Pro-48-PoE`, ports 1–52
  - `es-16-xg`: the built-in ES-16-XG pattern, ports 1–16
- **When a drawing is used:** only when a unit's `MainPortNumbers` (with the device's numbering) equal the drawing's set exactly. A drawing ignores saved layouts and device faceplate settings.
- **Drive bay:**
  - The UDM-SE bay disk is `sdb`.
  - Columns read: `1.3.6.1.4.1.2021.13.15.1.1.2` (name), `.12` (`diskIONReadX`), `.13` (`diskIONWrittenX`).
  - States: `absent`, `present`, `active`, `unknown`.
  - A counter that goes down counts as `present` and re-baselines.
- **Port light colours:**
  - `up`: green. On the ES-16-XG panel, white if `speed_bps ≥ 10_000_000_000`.
  - `warning`: yellow. `critical`: red.
  - `down` and `disabled`: unlit. An arrow goes back to its printed white.
- **Flicker:** when `busiestUtil > 0.5` (percent), faster when `> 50`. With `prefers-reduced-motion`, lights stay steady.
- **PoE LED** (USW-Pro-48 left): always unlit.
- **Dark rule:** the device's status is not `up`, or `last_stats_at` is older than 3 × `poll_interval` seconds. Then:
  - every light is dark and nothing flickers
  - a badge shows (copy below)
  - popovers are headed "Last known (HH:MM)"
  - Simple is unchanged
- **Badge copy:**

  | Situation | Badge |
  |---|---|
  | `down` | "Not responding since HH:MM", or "Not responding" with no `last_seen_at` |
  | `paused` | "Paused" |
  | `error` | "Error: <status_detail>" (public links: "Error") |
  | `pending` | "Waiting for the first poll" |
  | old stats | "No port data since HH:MM" |

- **Drive bay tooltip:**
  - "3.5″ drive bay: drive installed, active"
  - "3.5″ drive bay: drive installed"
  - "3.5″ drive bay: empty"
  - "3.5″ drive bay: Sentinel can't read this drive's status"
- **The switch:** "Realistic | Simple", stored at `sentinel:faceplate-view` (`realistic` | `simple`), defaulting to `realistic`. It's shown only when a unit has a drawing. Dashboards always show the drawing.
- **Scale:**
  - device page: fit the width, clamped to 0.6–1.5×, then scroll
  - widget: fit, with no minimum
- **CSS:** the drawings' styles live in `frontend/src/components/network/drawings/drawings.css`. Never write `*/` inside a CSS comment.
- **Fix layout:** for a drawn model, the editor shows "This model has a realistic drawing; this layout applies to the Simple view".
- **Frontend gate:** `docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"`
- **DB tests:** `cd backend && ./scripts/test-db.sh -run <Name> -v`

## Review Focus

1. **A device that goes down while its drive was `active`.** The bay must not keep flickering; it reads `unknown` (dark) until polls resume. Test in Task 2: `TestPollerForgetsDriveBayWhenNotUp`.
2. **Old stats on a device that is `up`.** It goes dark with "No port data since …". Test in Task 3: `TestFaceplateDeviceState` covers the timing, and the frontend rule in Task 4 uses those exact fields.
3. **A public dashboard link on a device in `error`.** The badge must not leak `status_detail`. Test in Task 3: `TestPortGridPublicTrimsStatusDetail`.
4. **A model that matches but has an extra or missing port** (a firmware that exposes a 12th UDM port). It falls back to Simple, never a drawing with a missing port. Test in Task 1: `TestLayoutUnitsDrawingNeedsExactPorts`.
5. **A drive bay walk that times out.** The stats and other polls continue, and the state is `unknown`, not `absent`. Test in Task 2: `TestDriveBayWalkErrorIsUnknown`.

---

### Task 1: The drawing table in `portmon`

**Files:**
- Create: `backend/internal/portmon/drawings.go`
- Modify: `backend/internal/portmon/layout.go` (`UnitFaceplate.Drawing`; `LayoutUnits` sets it)
- Test: `backend/internal/portmon/drawings_test.go`

**Interfaces:**
- Produces:
  - `type Drawing struct { Key string; Ports []int; DriveBayDisk string }`
  - `func DrawingFor(model string) *Drawing`, which matches on `NormalizeModel` and returns nil otherwise
  - `UnitFaceplate.Drawing string` with `json:"drawing,omitempty"`. `LayoutUnits` sets it per unit when `DrawingFor(o.Model)` is non-nil and `MainPortNumbers(unit ports, o.Numbering)` equals `Ports` exactly.
  - Keys: consts `DrawingUDMSE = "udm-se"`, `DrawingUSWPro48PoE = "usw-pro-48-poe"`, `DrawingES16XG = "es-16-xg"`
  - `udm-se` has `DriveBayDisk: "sdb"`

- [ ] **Step 1: Write the failing tests**
  - `TestDrawingFor`:
    - `"UDM-SE"` → `udm-se`
    - `" udm-se "` → `udm-se`
    - `"UBNT-US48PRO-POE"` and `"USW-Pro-48-PoE"` → `usw-pro-48-poe`
    - `"UBNT-ES16XG"` → `es-16-xg`
    - `"USW-Pro-48"`, `"UDM-Pro"`, `""` → nil
    - `DrawingFor("UDM-SE").DriveBayDisk == "sdb"`
  - `TestLayoutUnitsDrawing`, using the fixtures:
    - `udmSELayoutPorts()` with `Model "UDM-SE"` and `NumberingFor(udmSEDescr)` → `[0].Drawing == "udm-se"`, and the same units with `Numbering{}` → `""`
    - `uswLayoutPorts()` with `"UBNT-US48PRO-POE"` → `"usw-pro-48-poe"`
    - `es16XGLayoutPorts()` with `"UBNT-ES16XG"` → `"es-16-xg"`
    - The `blocks` are still filled in each time.
  - `TestLayoutUnitsDrawingNeedsExactPorts` (Review Focus 4):
    - ES-16-XG ports minus port 16 → `""`
    - plus an extra `0/17` → `""`
    - a two-unit stack of ES-16-XG-shaped units where unit 2 lacks port 3 → unit 1 has the drawing and unit 2 doesn't

- [ ] **Step 2: Run them and see them fail**

  Run: `cd backend && go test ./internal/portmon/ -run 'Drawing'`. Expected: build failure (undefined `DrawingFor`).

- [ ] **Step 3: Implement** as in the Interfaces block. Use one table, like `builtinLayouts`, with patterns:
  - `(?i)^UDM-?SE$`
  - `(?i)^(UBNT-US48PRO-POE|USW-PRO-48-POE)$`
  - the ES pattern from `models.go`

- [ ] **Step 4: Run them and see them pass**, then `go vet ./internal/portmon/ && go test ./internal/portmon/`.

- [ ] **Step 5: Commit** `feat(network): drawing table for realistic faceplates`

### Task 2: The drive bay monitor and poller hook

**Files:**
- Create: `backend/internal/services/drive_bay.go`
- Modify: `backend/internal/services/device_poller.go` (`DriveBayPoller`, `SetDriveBay`, calls)
- Test: `backend/internal/services/drive_bay_test.go`; append to `device_poller_test.go`

**Interfaces:**
- Consumes: `portmon.DrawingFor` (Task 1).
- Produces:
  - consts `DriveBayAbsent = "absent"`, `DriveBayPresent = "present"`, `DriveBayActive = "active"`, `DriveBayUnknown = "unknown"`
  - `type DriveBayMonitor struct`, with `func NewDriveBayMonitor(client snmp.Client) *DriveBayMonitor`
  - `func (m *DriveBayMonitor) PollDriveBay(ctx context.Context, d models.Device, t snmp.Target)`. It does nothing unless `DrawingFor(effective model of d)` has a `DriveBayDisk`. The effective model is `model_override` if not blank, else `model`.
  - `func (m *DriveBayMonitor) Forget(deviceID uuid.UUID)`
  - `func (m *DriveBayMonitor) State(deviceID uuid.UUID) string`, which is `unknown` when there's no entry. Guarded by a mutex.
  - poller: `type DriveBayPoller interface { PollDriveBay(ctx, models.Device, snmp.Target); Forget(uuid.UUID) }` and `func (p *DevicePoller) SetDriveBay(b DriveBayPoller)`
    - after a poll that is OK and leaves the device `up`: `PollDriveBay`
    - otherwise: `Forget(d.ID)`
- The state machine is a pure function: `func nextDriveBay(prev *bayCounters, found bool, read, written uint64) (state string, next *bayCounters)`.
  - `found == false` → `absent`, and next is nil
  - the first sighting (prev nil) → `present`
  - either counter up → `active`
  - either counter down, or both unchanged → `present`
  - next always holds the current values
  - A walk error never calls it: the state becomes `unknown` and prev is kept.

- [ ] **Step 1: Write the failing tests**
  - `TestNextDriveBay`, table-driven over consecutive calls:
    - not found → absent
    - first found → present
    - same counters → present
    - written +4096 → active
    - read +1 → active
    - written goes down → present, and the next call with the same values → present
    - not found after found → absent
  - `TestDriveBayPoll`, with a `fakeSNMP`-style client returning walk PDUs for names `sda,sdb` and counters:
    - first poll → `present`
    - second poll with `sdb` written +1 → `active`
    - third poll where `sdb` is absent → `absent`
    - a device with model `"USW-24"` → never walked, and `State` is `unknown`
  - `TestDriveBayWalkErrorIsUnknown` (Review Focus 5): a walk error → `unknown`. The next good poll with unchanged counters → `present`, because the counters were kept.
  - `TestPollerForgetsDriveBayWhenNotUp` (Review Focus 1), with a recording fake:
    - an `up` poll → one PollDriveBay
    - a failing poll (`getErr`) → one Forget and no poll

- [ ] **Step 2: Run them and see them fail**

  Run: `cd backend && go test ./internal/services/ -run 'DriveBay|ForgetsDriveBay'`

- [ ] **Step 3: Implement.**
  - Walk the three columns with `client.Walk`, joining by the last OID arc (the row index).
  - Find the row whose name is the drawing's `DriveBayDisk`. Counters are `Counter64`: read them with `PDU.Text()` and `strconv.ParseUint`.
  - Log a walk error once per state change, never per poll.

- [ ] **Step 4: Run them and see them pass**, plus `go test ./internal/services/`.

- [ ] **Step 5: Commit** `feat(network): drive bay state from the disk activity table`

### Task 3: The ports view carries device state and indicators, and so does the port grid

**Files:**
- Modify: `backend/internal/services/port_service.go` (`DevicePortsView`, `DevicePorts`)
- Create: `backend/internal/services/faceplate_state.go`
- Modify: `backend/internal/dashboards/widget_port_grid.go` (`PortGridData`, public trim)
- Modify: `backend/cmd/sentinel/main.go` (construct `DriveBayMonitor`, `devicePoller.SetDriveBay`, `portService.SetDriveBays`)
- Test: `backend/internal/services/faceplate_state_test.go`; `backend/internal/dashboards/widget_port_grid_test.go` (create or append)

**Interfaces:**
- Consumes: `DriveBayMonitor.State` (Task 2); `UnitFaceplate.Drawing` (Task 1).
- Produces:
  - `type FaceplateDeviceState struct`, with fields:
    - `Status string` (`json:"status"`)
    - `StatusDetail string` (`json:"status_detail"`)
    - `LastSeenAt *time.Time` (`json:"last_seen_at"`)
    - `LastStatsAt *time.Time` (`json:"last_stats_at"`)
    - `PollInterval int` (`json:"poll_interval"`)
  - `func faceplateDeviceState(d *DeviceView) FaceplateDeviceState`
  - `type FaceplateIndicators struct { DriveBay string \`json:"drive_bay,omitempty"\` }`
  - `type DriveBayStates interface { State(uuid.UUID) string }` and `func (s *PortService) SetDriveBays(b DriveBayStates)`
  - `DevicePortsView.DeviceState FaceplateDeviceState` (`json:"device_state"`)
  - `DevicePortsView.Indicators *FaceplateIndicators` (`json:"indicators,omitempty"`). It's set only when some unit's drawing has a `DriveBayDisk` and `SetDriveBays` was called.
  - `PortGridData` gains `DeviceState services.FaceplateDeviceState` (`json:"device_state"`) and `Indicators *services.FaceplateIndicators` (`json:"indicators,omitempty"`). With `Viewer.Public`, `DeviceState.StatusDetail` is `""`.

- [ ] **Step 1: Write the failing tests**
  - `TestFaceplateDeviceState`: a `DeviceView` with status `up`, `LastStatsAt` = t, `PollInterval` 60 → all five fields copied. With status `error` → `StatusDetail` copied.
  - `TestDevicePortsIndicators`, with a fake `DriveBayStates` returning `active`:
    - DB test via `newEditorFixture`, a UDM-SE device with `sys_descr` set and `eth0–10` seeded → `Indicators.DriveBay == "active"` and `Faceplates[0].Drawing == "udm-se"`
    - a USW-24 device → `Indicators == nil`
  - `TestPortGridPublicTrimsStatusDetail` (Review Focus 3): a device in `error` with detail `"timeout to 10.0.0.9"`
    - the public resolve gives `DeviceState.StatusDetail == ""` and `Status == "error"`
    - a signed-in viewer gets the detail

    Follow the existing widget tests' fakes for `DeviceReader` and `PortReader`.

- [ ] **Step 2: Run them and see them fail.**

- [ ] **Step 3: Implement and wire `main.go`.**
  - Construct the monitor with `snmpClient`.
  - Call `devicePoller.SetDriveBay(bays)` beside `SetUPS`.
  - Call `portService.SetDriveBays(bays)`.

- [ ] **Step 4: Run** `go vet ./... && go test ./...`, then the full `./scripts/test-db.sh`. Expected: no failures.

- [ ] **Step 5: Commit** `feat(network): faceplate device state and drive bay indicator on the ports view`

### Task 4: Shared drawing parts, light rules and the wrapper

**Files:**
- Create: `frontend/src/components/network/drawings/drawings.css`
- Create: `frontend/src/components/network/drawings/parts.tsx` (`Rj45Jack`, `Rj45Block`, `SfpHousing`, `PanelLight`, `PrintedLabel`, `PortHit`)
- Create: `frontend/src/utils/faceplateLights.ts`
- Create: `frontend/src/components/network/RealisticFaceplate.tsx`
- Create: `frontend/src/components/network/drawings/registry.ts`
- Modify: `frontend/src/components/network/Faceplate.tsx` (export `PortPopover`, with an optional `lastKnown?: string` header)
- Modify: `frontend/src/hooks/usePorts.ts` (`UnitFaceplate.drawing?`, `DevicePorts.device_state`, `DevicePorts.indicators?`)
- Modify: `frontend/src/types/dashboards.ts` (`PortGridData.device_state`, `indicators?`)

**Interfaces:**
- Consumes: Task 3 JSON.
- Produces:
  - `faceplateLights.ts`:
    - `type Light = 'green' | 'white' | 'yellow' | 'red' | 'off'`
    - `portLight(p: PortView, opts?: { tenGigWhite?: boolean }): Light`
    - `flicker(p: PortView): '' | 'slow' | 'fast'`
    - `offline(s: FaceplateDeviceState, now: Date, isPublic?: boolean): { dark: boolean; badge: string | null; since: string | null }`
    - `driveBayLight(state?: string): { light: 'white' | 'off'; flicker: boolean; title: string }`
  - `parts.tsx`: every port part takes `port?: PortView`, `light: Light`, `flicker`, `selected`, `onSelect`, and an `aria-label` from `portTitle`. `PortHit` wraps a part as a `<button>` and opens `PortPopover` on hover or focus, as `PortCell` does today. `Rj45Jack` takes `flip`, `leds: ('poe' | 'state')[]` and `ledSide`. `SfpHousing` takes `top`/`bottom` ports, their lights, and `bar: 'arrows' | 'plain'`.
  - `registry.ts`: `DRAWINGS: Record<string, React.ComponentType<DrawingProps>>`, where `DrawingProps = { unit: UnitFaceplate; byNumber: Map<number, PortView>; dark: boolean; indicators?: FaceplateIndicators; selected: number | null; onSelect: (ifIndex: number) => void; lastKnown: string | null }`
  - `RealisticFaceplate` props:
    - `{ faceplates, ports, deviceState, indicators, title, subtitle, selected, onSelect, fit: 'page' | 'widget', isPublic?: boolean }`
    - per unit: the registered drawing, else the existing `Faceplate` for that unit alone
    - it measures its container with a ResizeObserver and scales (page 0.6–1.5×, widget no minimum)
    - when `dark`, it renders the badge over the top-left and passes `dark` down
  - `byNumber` keys on `PortView.number` within the unit.

- [ ] **Step 1: Write the parts and CSS from `udm-se.html`'s style.** Use the class names and values from that mockup's `<style>`; where the CSS needs a comment, never put `*/` inside it. Flicker classes: `.flicker-slow` (1 s) and `.flicker-fast` (0.4 s). Wrap both in `@media (prefers-reduced-motion: no-preference)`.

- [ ] **Step 2: Write `faceplateLights.ts`** to the colour table, flicker thresholds, dark rule and badge copy in Global Constraints. `since` formats `last_seen_at` or `last_stats_at` with the existing time preference helpers.

- [ ] **Step 3: Write `RealisticFaceplate` and the empty registry.** Export `PortPopover`, and add its `lastKnown` header line ("Last known (HH:MM)").

- [ ] **Step 4: Run the frontend gate.** Expected: it passes.

- [ ] **Step 5: Commit** `feat(network): shared realistic faceplate parts and light rules`

### Task 5: The three drawings

**Files:**
- Create: `frontend/src/components/network/drawings/UdmSe.tsx`, `UswPro48Poe.tsx`, `Es16Xg.tsx`
- Modify: `drawings.css` (case styles), `registry.ts`

**Interfaces:**
- Consumes: Task 4 parts and `DrawingProps`.

- [ ] **Step 1: UdmSe**, as `udm-se.html` (the full-device view).
  - The jack LED is `state` only. The SFP+ arrows carry the state of 10 (▲) and 11 (▼).
  - The drive bay light uses `driveBayLight(indicators?.drive_bay)` and is dark when `dark`.

- [ ] **Step 2: UswPro48Poe**, as `usw-pro-48-poe.html` (full view).
  - LEDs are `['poe', 'state']`. PoE is always `off`.
  - The arrows show the state of 49/50 and 51/52.

- [ ] **Step 3: Es16Xg**, as `es-16-xg.html`.
  - Jacks and cages have no LEDs. The SFP bar is `plain`.
  - The panel lights use `portLight(p, { tenGigWhite: true })`.
  - The power light is white unless `dark`.

- [ ] **Step 4: Register the three keys and run the gate.**

- [ ] **Step 5: Render each drawing** in the headless harness (scratchpad, mocked data, as for `14c790d`) and compare it side by side with its mockup screenshot. Fix differences until they match.

- [ ] **Step 6: Commit** `feat(network): realistic UDM-SE, USW-Pro-48-PoE and ES-16-XG faceplates`

### Task 6: Device page, widget and editor note

**Files:**
- Modify: `frontend/src/pages/network/DeviceDetail.tsx` (switch, `RealisticFaceplate`)
- Modify: `frontend/src/utils/preferences.ts` (`PREF.faceplateView = 'sentinel:faceplate-view'`)
- Modify: `frontend/src/components/dashboards/widgets/PortGridWidget.tsx`
- Modify: `frontend/src/components/network/FaceplateLayoutEditor.tsx` (the note)

- [ ] **Step 1: DeviceDetail**
  - When some unit has a `drawing`, show a segmented "Realistic | Simple" control next to "Front panel". It's stored with `getString` / `setString`, defaulting to `realistic`.
  - Realistic renders `RealisticFaceplate` with `fit="page"`. Simple renders today's `Faceplate`, unchanged.
  - The selection and `PortQuickPanel` work as today in both views.

- [ ] **Step 2: PortGridWidget** renders `RealisticFaceplate` with `fit="widget"` and `isPublic={!linkable}` when any unit has a drawing, and today's `Faceplate` otherwise.

- [ ] **Step 3: The editor note.** When any unit in the device's ports view has a drawing, show the note (exact copy in Global Constraints) above the preview. The editor gets that from a new prop `hasDrawing: boolean`, passed by DeviceDetail.

- [ ] **Step 4: Run the gate, then a headless check.**
  - The harness renders the UDM-SE drawing with a `paused` device state: everything is dark and the badge reads "Paused".
  - Hovering a jack opens the popover headed "Last known".
  - At 0.6× scale, clicking a jack still selects it.

- [ ] **Step 5: Commit** `feat(network): realistic faceplates on the device page and port grid widget`

### Task 7: Review, status, push and sandbox

- [ ] **Step 1:** One fresh reviewer (the most capable model) reviews the branch diff against the spec and this plan's Review Focus. Fix Critical and Important findings test-first.

- [ ] **Step 2:** In `docs/superpowers/STATUS.md`, add the realistic faceplates line and the spec's owner checks 1–7. Commit `docs: realistic faceplates status and owner checks`.

- [ ] **Step 3: Push** (the owner asked for this).
  - `git fetch origin`. If `origin/dev` moved, merge it in, rerun `go test ./...`, the full `./scripts/test-db.sh` and the frontend gate, then push.
  - `git push origin feature/realistic-faceplates:dev`. This carries `b1e82ea` (UniFi numbering) too.

- [ ] **Step 4: Update the sandbox.**

  ```bash
  cd /srv/docker/sentinel-dev
  git checkout dev
  git pull --ff-only
  docker compose up -d --build
  ```

  Expected: the backend is healthy, and `curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:3201/api/v1/network/faceplate-layouts` returns `401`.
