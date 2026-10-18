# Realistic faceplates — design

Status: written 2026-10-18 from a mockup session with the owner. They approved
the look of three models in the browser. Choices the owner made are under
"Decided by the owner". Everything else is Claude's call and is marked
**(call)**.

**Builds on:** `feature/unifi-port-numbers` (commit `b1e82ea`, not yet on
`dev`). That branch numbers UniFi console ports from 1 and adds the
built-in UDM-SE layout. Merge or rebase onto it first.

## Purpose

The faceplate on a device's page is a flat diagram: coloured rounded
rectangles in the right arrangement. The owner wants it to look like the
real device, as if standing in front of the rack. That means the whole
front panel, drawn per model, with every current feature kept.

They'll add models a few at a time. This design covers the first three,
the ones in their own network:
- UniFi Dream Machine SE (`Quantum-Gate`)
- USW-Pro-48-PoE (`QuantumLink`)
- EdgeSwitch 16 XG (at work)

It also sets up the way later models (Cisco and others) get added.

The approved mockups are the visual reference for each model. They're
committed beside this file in `assets/2026-10-18-realistic-faceplates/`:
`udm-se.html`, `usw-pro-48-poe.html` and `es-16-xg.html`, which open in any
browser. Where this text and a mockup disagree on how something looks, the
mockup wins. On behaviour, this text wins.

### Decided by the owner

- **The look:**
  - Draw the whole front of the device as that exact model: case colour and
    finish, vents, screens, drive bays, logos, printed labels and icons,
    speed tags and the reset pinhole, each in its real place.
  - The owner rejected option B, real-looking ports on a generic chassis,
    as the way to make a device realistic.
- **One port style for every model.** RJ45 jacks and SFP+ cages look the
  same on every drawing, as in the UDM-SE mockup. The owner asked for this
  explicitly.
- **Port state shows where the real device shows it:**
  - **Jack LEDs:** on the UDM-SE and the USW-Pro-48, in the LED's real
    corner.
  - **The status panel:** on the ES-16-XG, whose jacks and cages have no
    lights.
  - **The ▲ ▼ arrows on SFP+ housings:** for the UDM-SE's and USW-Pro-48's
    SFP+ cages, which have no lights on the real case. ▲ is the top cage,
    ▼ the bottom.
- **Both SFP+ cages in a housing are drawn the same.** They're always drawn
  as empty cages with their connector edges facing each other across the
  arrow bar. A plugged-in module is not drawn: the owner asked for port 11
  to match port 10.
- **Colours are Sentinel's state colours, not the vendor's white,** except
  on the ES-16-XG panel, which also shows link speed the way the real one
  does (see "Lights").
- **UDM-SE drive bay light:**
  - steady white when a drive is in the bay
  - flickers when the drive was active since the last poll
  - off when the bay is empty
- **PoE light on the USW-Pro-48:**
  - stays unlit for now, because the switch doesn't report PoE over SNMP
    (checked 2026-10-18)
  - lights later, when the UniFi controller source (network phase 7) can
    supply PoE state
- **Models are added a few at a time,** with more later. Cisco is wanted
  eventually.
- **Everything the faceplate does today stays:**
  - hover details
  - click to select, and the selection ring
  - state colours, and flicker while busy
  - stacks
  - the dashboard port grid widget

## Goals

- The three models above draw as their mockups do, on:
  - the device page
  - the dashboard port grid widget
  - public dashboard links
  - the wall display
- Each port in a drawing has the same behaviour as today's faceplate:
  - hover or keyboard focus shows the existing port popover
  - click selects it, and shows the selection ring
  - it is a focusable button with the same accessible label
- Port state reads at a glance from the lights. Problems (yellow) and
  important ports down (red) stand out.
- Adding the next model means adding one drawing, plus one line in the
  backend's drawing table.

## Non-goals

- Models beyond these three, in this piece. Cisco comes in a later batch,
  using the same shared parts.
- Drawings from photos, or a drawing editor in the UI. Drawings are code,
  added by developers, using the owner's photos as reference.
- Drawing a plugged-in SFP module, or which kind it is.
- PoE state on UniFi switches (it waits for network phase 7). Fan, PSU or
  temperature lights.
- Rear panels.
- Per-packet live blinking. Lights reflect the latest stats poll, about
  once a minute.
- The USW-Pro-48 without PoE, and other look-alike models. Each gets its
  own drawing later.

## Anti-goals (failures even if it works)

- **A drawing that hides or misplaces a port.** If a device's ports don't
  match its drawing exactly, it gets the simple faceplate instead (see
  "Choosing a drawing").
- **A light that claims something Sentinel doesn't know.** For example, a
  lit PoE LED with no PoE data, or a lit drive bay when the disk table
  can't be read.
- **Losing any current faceplate feature,** or the simple faceplate itself.

## Choosing a drawing

The backend decides, so matching and fit rules stay in one place
**(call)**.

- **The drawing table:** `portmon` gains a drawing table beside the
  built-in layouts. Each entry has:
  - a key (`udm-se`, `usw-pro-48-poe`, `es-16-xg`)
  - a model pattern, matched on the normalized effective model
  - the exact set of main port numbers the drawing places
  - its device lights (see "Device lights")
- **Which models match each entry:**

  | Key | Matches | Ports |
  |---|---|---|
  | `udm-se` | `UDM-SE` | 1–11 (after the UniFi numbering) |
  | `usw-pro-48-poe` | `UBNT-US48PRO-POE` (what QuantumLink's ENTITY-MIB gives) and `USW-Pro-48-PoE` | 1–52 |
  | `es-16-xg` | the same pattern as the built-in ES-16-XG layout | 1–16 |

- **When a drawing is used:** for each stack unit (each `UnitFaceplate`),
  when the model matches and the unit's main port numbers
  (`MainPortNumbers` with the device's numbering) equal the drawing's set
  exactly. Then `UnitFaceplate` gains `drawing: "<key>"`.
- **When it isn't:** otherwise `drawing` is absent and the unit draws as
  today.
- **The simple view's data stays unchanged:** the `blocks` and `rows`
  fields are still filled in, so the simple view and the Fix layout editor
  keep working.
- **Precedence:** a drawing ignores saved layouts and the device's own
  Edit details faceplate settings. Those only shape the simple faceplate
  **(call)**. The drawing is the real arrangement; layouts were a way to
  approximate it.
- **Fix layout:** stays on the device page. Its editor still previews the
  simple faceplate. For a device that has a drawing, the editor says:
  "This model has a realistic drawing; this layout applies to the Simple
  view" **(call)**.

## Realistic or simple

On the device page, admins and everyone else get a two-way switch above
the faceplate: **Realistic | Simple** **(call)**.
- It's shown only when at least one unit has a drawing.
- The choice is remembered per browser, under `sentinel:faceplate-view`
  through `utils/preferences.ts`. The default is Realistic.
- Simple is today's faceplate, unchanged.

Dashboards (the port grid widget, public links, the wall display) always
show the drawing when there is one **(call)**. They have no switch.

## Scale

Each drawing is laid out at one base size, in CSS pixels, as in the
mockups, and scaled as a whole with a CSS transform **(call)**:
- **Device page:** fit the container's width, between 0.6× and 1.5×.
  Below 0.6× the panel scrolls sideways, as today.
- **Dashboard widgets:** fit inside the widget, at no minimum, so the whole
  device always shows.

Scaling must keep hit areas, focus rings and popover positions correct.
The existing portal popover positions from the button's real on-screen
rectangle, so a transformed ancestor is fine. Check that in the build.

## Shared parts

So every model's ports look identical, the drawings use shared
components, styled as in `udm-se.html` **(call)**:

- `Rj45Jack`:
  - light grey metal body and dark opening, with the latch notch and the
    contact pins
  - mirrored for the bottom row (`flip`)
  - takes zero, one or two LEDs, placed by the drawing (top-right, or
    top-left and top-right, mirrored on the bottom row)
- `Rj45Block`: the dark-edged frame around a run of jacks, with optional
  dividers between groups. The USW-Pro-48 uses one frame around all 48,
  with a divider after every 12.
- `SfpHousing`:
  - one housing with two empty cages, their connector edges facing the
    middle, and spring clips
  - the middle bar shows either ▲ ▼ arrows that can carry state (UDM-SE,
    USW-Pro-48), or a plain metal divider (ES-16-XG)
- `PanelLight`: a round status light with its number under it, for status
  panels like the ES-16-XG's.
- `PrintedLabel`: the port number plus the icons printed on the case: PoE
  bolt (with `+` or `++`), WAN globe, and speed tag pills (`2.5G`, `10G`).

Each model's case (colour, finish, vents, screen, bay, logo, layout
positions) lives in its own component under
`frontend/src/components/network/drawings/` (`UdmSe.tsx`,
`UswPro48Poe.tsx`, `Es16Xg.tsx`). A registry maps each key to its
component. An unknown key falls back to the simple faceplate.

Styling is a dedicated CSS file for the drawings rather than Tailwind
utility classes, because the gradients and clip paths would be unreadable
as utilities **(call)**. Never write `*/` inside a CSS comment: it ends
the comment early and breaks the build. This has happened three times in
this repo.

The drawings look the same in light and dark mode: they're physical
objects. Only the area around them follows the theme.

## Lights

Every light comes from data Sentinel already has, except the drive bay
light.

### Port lights

The UDM-SE jack LED, the USW-Pro-48 right-hand jack LED, the SFP+ arrows,
and the ES-16-XG panel lights. "State" is today's `portState()`.

| State | Jack LED / arrow | ES-16-XG panel light |
|---|---|---|
| `up` (in use) | green | white if `speed_bps ≥ 10 Gbps`, else green, as the real 1G/10G legend shows |
| `warning` (a problem condition) | yellow | yellow |
| `critical` (important port down) | red | red |
| `down` (nothing plugged in) | unlit grey; an arrow goes back to its printed white | off (dark) |
| `disabled` (admin down) | unlit grey | off |

- **Flicker (call):** a lit light flickers when the port's latest busiest
  utilisation (`busiestUtil`) is above 0.5%. It flickers faster above 50%.
  That replaces today's fill from the bottom, which doesn't fit a real
  jack. The popover still gives the exact figures.
- **The USW-Pro-48 left-hand jack LED** is the PoE light. It's always
  unlit in this piece, with no tooltip claim. A later phase drives it
  amber when PoE data exists.

### Device lights

Lights that aren't ports, declared per drawing:

- **ES-16-XG power light:** white while the device is up, off when
  Sentinel has it down.
- **UDM-SE drive bay light:** from the disk activity table (UCD-DISKIO-MIB,
  table `1.3.6.1.4.1.2021.13.15.1.1`). It reads three columns:
  - `.2` `diskIODevice` (name)
  - `.12` `diskIONReadX`
  - `.13` `diskIONWrittenX`

  The bay disk on the UDM-SE is named `sdb`. The owner proved that on
  2026-10-18 by pulling the drive: `sdb` disappeared, then came back as
  `sdb` when it was reinserted. `sda` is the built-in SSD. The drawing
  table records `drive_bay_disk: "sdb"`.

The drive bay state is one of:

| State | Meaning | Light |
|---|---|---|
| `absent` | the table answered without `sdb` | off |
| `present` | `sdb` listed, counters unchanged since the last poll | steady white |
| `active` | `sdb`'s read or write counter went up since the last poll | white, flickering |
| `unknown` | the table didn't answer, or this is the first poll since Sentinel started | off |

- **Counter resets:** a counter that went down (a reset, or the drive was
  reinserted) is treated as `present` for that poll. Its value becomes the
  new baseline.
- **The tooltip on the bay says the state:**
  - "3.5″ drive bay: drive installed, active"
  - "drive installed"
  - "empty"
  - "Sentinel can't read this drive's status"

**Where it's polled (call):**
- The device's regular stats poll reads those three columns, only for
  devices whose drawing declares a drive bay.
- The poller keeps the last counters in memory, per device. Nothing is
  stored in the database.
- The monitor is built after `PortService` in `main.go`, so `PortService`
  gets a setter, in the style of `SetFaceplateLayouts`, to read the drive
  bay state from it.
- The state is served on the ports view as
  `indicators: { drive_bay: "<state>" }`, beside `faceplates`.
- A failed walk never fails the stats poll.

### When Sentinel isn't hearing from the device

**Decided by the owner (2026-10-18):** when Sentinel isn't hearing from a
device, **every light on its drawing goes dark**, like a powered-off unit,
and a badge on the drawing says why. Nobody should look at a drawing and
think the device is up and working when it isn't.

**"Not hearing from it" means either of these (call):**
- The device's status is anything but `up`: `down`, `paused`, `error` or
  `pending`.
- Its last stats poll (`last_stats_at`) is older than three poll
  intervals.

**Then:**
- **Lights:** every port light, panel light, arrow, power light and drive
  bay light is drawn dark. Arrows go back to their printed white. Nothing
  flickers.
- **The badge** sits over the drawing's top-left corner and reads
  **(call)**:

  | Situation | Badge |
  |---|---|
  | `down` | "Not responding since 10:42", from `last_seen_at`; "Not responding" when that's empty |
  | `paused` | "Paused" |
  | `error` | "Error: <status_detail>" |
  | `pending` | "Waiting for the first poll" |
  | old stats | "No port data since 10:42", from `last_stats_at` |

- **Hover:** a port's popover still opens, with its last known details,
  headed "Last known (10:42)".
- **The drive bay** goes to `unknown`. Its in-memory state is cleared when
  a stats poll is skipped because the device isn't up, so it can't freeze
  on "active".
- **The Simple view is unchanged:** it keeps showing the last known state,
  as today.

**The data this needs:** the drawing needs the device's `status`,
`status_detail`, `last_seen_at`, `last_stats_at` and poll interval.

- The device page already has them.
- `PortGridData` (dashboard widgets and public links) copies fields from
  the ports view rather than embedding it, so the build adds them there
  **(call)**:
  - a `device_state` object with the status and the two times
  - `indicators`

  For public links, `status_detail` is cut to the status word alone, since
  error text can name internal hosts **(call)**.

## Per-model notes

The mockups show these. They're listed so the build can be checked
against them.

**UDM-SE** (silver):
- **Left:** the touchscreen, with "Dream Machine SE" under it.
- **Top edge:** thin vent slots in segments.
- **Middle:** the 3.5″ drive bay tray with its latch, and the bay light.
- **LAN:** ports 1–8 in one dark-edged block, 2 × 4, odd numbers on top.
  Each jack has one LED (top-right on the top row, bottom-right on the
  bottom row). The printed labels show a PoE+ bolt on 1 and 2, and a PoE
  bolt on 3–8.
- **WAN 9:** framed, level with the bottom row, with "9" and a globe below
  it and a "2.5G" tag.
- **SFP+:** one housing with 10 over 11 and the ▲ ▼ bar. "10" and a globe
  above it, "11" below, and a "10G" tag.
- **RESET:** a pinhole on the label row, level with "11", at the right.

**USW-Pro-48-PoE** (silver):
- **Left:** the touchscreen with its four icons, with "USW Pro ⚡" under it.
- **Top edge:** five vent slots.
- **Ports 1–48:** one frame with a divider after every 12, two rows,
  odd numbers on top.
  - Two LEDs per jack: PoE on the left, state on the right, mirrored on
    the bottom row.
  - Labels: PoE+ bolts on 1–40, PoE++ on 41–48.
- **SFP+:** two housings, 49 over 50 and 51 over 52, each with ▲ ▼, and one
  "10G" tag under both.
- **RESET:** at the bottom right.

**ES-16-XG** (black):
- **Left:** the "U" logo, a divider and "EdgeSwitch 16 XG" with its badge,
  with the power light under the logo.
- **Status panel:**
  - 12 lights numbered 1–12, with a three-line "SFP+" legend (○ DOWN,
    ● 1G green, ● 10G white)
  - a gap
  - 4 lights numbered 13–16, with the same "RJ45" legend
- **Ports 1–12:** six SFP+ housings, odd numbers above and even below,
  each with a plain metal divider and no lights.
- **Ports 13–16:** one silver frame, its bottom level with the bottom row
  of cages, numbers on the same line as 2–12, and no lights.
- **RESET:** on the case, low and just right of port 16.

## Where the code changes

- `portmon`:
  - the drawing table and `DrawingFor(model string) *Drawing`
  - the exact-ports check
  - `UnitFaceplate.Drawing`
- `services`:
  - `DevicePorts` sets `drawing` per unit, and `indicators`
  - the stats poll's disk walk and the in-memory drive bay state
  - the dashboard port grid widget's data, which already embeds the ports
    view, carries both through
- Frontend:
  - the `UnitFaceplate` type gains `drawing?`, and `DevicePorts` gains
    `indicators?`
  - `components/network/drawings/`: the shared parts, the three models,
    the registry and the CSS
  - the Realistic | Simple switch on `DeviceDetail`
  - `PortGridWidget` picks the drawing when there is one
  - the note in `FaceplateLayoutEditor`

## Testing

**Go unit tests:**
- drawing selection for each key:
  - matching model names (both USW spellings)
  - a near-miss model that doesn't match
- the exact-ports rule: a missing port or an extra port gives no drawing
- stack units each get their own decision
- the drive bay state machine across consecutive polls: absent, present,
  active, a counter going down, unknown after a failed walk, and the first
  poll after a restart
- the drive bay state is cleared when the device stops being up
- the "not hearing from it" rule: each status, and old stats

**Database test:** `DevicePorts` for a UDM-SE device returns
`drawing: "udm-se"` and the drive bay indicator. Feed it with a fake SNMP
client in the poller test.

**Frontend gate:** the `tsc`, `eslint` and `vite build` check in
`CLAUDE.md`.

**Visual check:** headless screenshots of each drawing compared side by
side with its mockup in `assets/`, and with the owner's photos. Check
hover, focus and click on a scaled drawing.

**Owner checks on the sandbox (3200):**
1. Quantum-Gate looks like `udm-se.html`. Hovering port 9 shows its
   popover, and clicking a jack selects it.
2. The drive bay light is steady white or flickering with the drive in.
   With the drive pulled, within two polls it goes off, and the tooltip
   says "empty".
3. QuantumLink looks like `usw-pro-48-poe.html`, with the PoE LEDs unlit.
4. Realistic | Simple switches between the drawing and today's faceplate,
   and the choice survives a reload.
5. A dashboard port grid widget for QuantumLink shows the whole drawing,
   scaled to fit.
6. At work, the ES-16-XG looks like `es-16-xg.html`. The 10G ports light
   white and the 1G ports light green.
7. Pause Quantum-Gate in Sentinel. Its drawing goes dark with "Paused",
   hovering a port shows "Last known", and Simple still shows the last
   known colours. Resume it, and it lights again after the next poll.

## Left to the builder

- **Exact pixel values, gradients and component boundaries,** within the
  mockups.
- **How the flicker is animated,** as long as it respects
  `prefers-reduced-motion`: with reduced motion, keep it lit and steady.
- **Whether the drive bay state lives on the existing port monitor or a
  small sibling of it.**
