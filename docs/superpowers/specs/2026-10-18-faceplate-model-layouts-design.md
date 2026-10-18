# Faceplate layouts per model — design

Status: written 2026-10-18 for the owner's review. The owner asked for
per-model layouts and skipped the design questions, so every choice below not
listed under "Decided by the owner" is Claude's call and is marked **(call)**.

## Purpose

The virtual faceplate on a device's page is drawn by general rules
(`backend/internal/portmon/layout.go`): copper ports on the left in blocks of
twelve, SFP cages on the right, one row count for the whole panel. Many
switches don't follow that. The EdgeSwitch 16 XG had its four RJ45 ports drawn
as SFP cages in a 2x8 grid, because its firmware calls every port "10G".

Commit `14c790d` fixed that one model with a built-in table in
`portmon/models.go`. Each new model in that table needs a code change and a
release. This design lets an admin fix any model from the device page, once,
for every device of that model.

### Decided by the owner

- Layouts are saved **per model**, not per device: fix it once and every
  switch of that model, at every site, is drawn correctly.
- Admins edit them, from an editor opened **from the faceplate**, with a live
  preview.
- A saved layout overrides the built-in table, which overrides the automatic
  rules. The per-device faceplate settings in Edit details keep working.

## Goals

- An admin can make any switch's main ports match the real front panel:
  - groups of ports, in order left to right
  - each group SFP or RJ45, one row or two
  - how the numbers run in a two-row group
- The fix applies at once to every device with the same model, including
  the faceplate on dashboards (port grid widget) and public dashboard links.
- A layout never hides a port. If a saved layout and a device's ports
  disagree, that device keeps the automatic drawing and admins are told why.
- Devices with no saved layout are drawn exactly as today.

## Non-goals

- Physical detail beyond port groups: LEDs, console and management jacks, the
  logo, true spacing, port sizes, vertical (side-mounted) ports.
- Per-device layouts. The existing Edit details settings cover one-off
  devices.
- Model patterns or regular expressions typed by users. Matching is exact
  (see below); the built-in table keeps its patterns.
- Network module layouts. Modules are still drawn after the main ports, as
  today.
- Stacks that mix models. A stack has one model, and every member uses its
  layout.
- Sharing layouts between Sentinel installs (export or import), or a
  community library.
- Group labels such as "Uplinks", and gaps inside a group.

## Anti-goals (failures even if it works)

- A saved layout that silently drops, duplicates or misplaces a port.
- A non-admin changing what everyone sees.
- Any device without a saved layout drawn differently from today.

## How a faceplate is chosen

For each device (each stack member) the server picks the first of these that
applies:

1. **The device's own settings** (Edit details → Faceplate: rows, port style,
   SFP ports) when any is set. The automatic rules then use them, as today.
2. **A saved model layout** whose model matches the device's effective model
   (the model typed in Edit details, else what SNMP reported:
   `COALESCE(NULLIF(model_override,''), model, '')`).
3. **The built-in table** in `portmon/models.go`.
4. **The automatic rules.**

A saved or built-in layout is used only when the device's main ports are
exactly the numbers the layout covers, each once. Otherwise the server moves
on to the next step. "Main ports" are what `Layout` already treats as main:
on a switch/slot/port device, the ports in the lowest slot.

**Matching (call):** a saved layout's model is compared with the device's
effective model after trimming, collapsing runs of whitespace to one space and
upper-casing. `ubnt-es16xg ` matches `UBNT-ES16XG`. Nothing looser, so a fix
for `USW-24` never lands on `USW-24-PoE`. If SNMP reports a model the admin
didn't expect, they can make it match by setting the model in Edit details,
which is already possible.

**Network modules (call):** a saved or built-in layout describes the main
ports only. Module blocks are still appended after it, labelled "Module N", as
the automatic rules do today. This changes the built-in rule from `14c790d`,
which steps aside whenever a device has modules, so built-in and saved
layouts behave the same.

## What a layout is

An ordered list of **groups**, left to right. Each group has:

| Field | Values | Meaning |
|---|---|---|
| `first`, `last` | port numbers, `first ≤ last` | the ports in the group, as Sentinel numbers them (the number shown on the faceplate) |
| `kind` | `sfp`, `rj45` | how its ports are drawn |
| `rows` | `1`, `2` | one row, or two |
| `numbering` | `alternate`, `across` | two rows only. `alternate`: odd numbers on top (1 above 2, 3 above 4), as on most switches. `across`: the top row first, then the bottom row (1–6 on top, 7–12 below) |

`numbering` is a **(call)**: the photos so far only need `alternate`, but some
Dell and Cisco models number across, and it costs one field.

Rules, checked by the server and shown in the editor:
- 1 to 16 groups, and at most 512 ports in total.
- Port numbers 1–9999.
- Ranges don't overlap.
- `numbering` is required for two-row groups and ignored for one-row groups.

A one-row group in a panel that also has two-row groups is drawn centred,
with its numbers below, as `14c790d` already does.

Two-row `across` group with an odd port count (call): the top row gets the
extra port.

The built-in table in `models.go` uses the same group shape, so the built-in
entries and saved layouts share one drawing function. The built-in entries
stay in code **(call)**, so fixes made by Sentinel's developers ship with
releases. An admin overrides one by saving a layout for that model.

## Storage

Migration `062_faceplate_layouts.sql`. Use the next free number at build
time; the other machine may add migrations to `dev` first.

```sql
CREATE TABLE IF NOT EXISTS faceplate_layouts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model       TEXT NOT NULL,          -- as the admin saved it, for display
    model_key   TEXT NOT NULL UNIQUE,   -- normalized for matching (see above)
    groups      JSONB NOT NULL,         -- [{first,last,kind,rows,numbering}]
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

- `TIMESTAMPTZ`, never bare `TIMESTAMP`.
- The `groups` shape is validated in Go, not by a CHECK constraint.
- It's configuration, so config backups include it: it's a `public` table,
  which `--table='public.*'` already dumps. A restore test must prove it comes
  back.

The server keeps saved layouts in memory, keyed by `model_key`, because the
faceplate is drawn on every port poll and dashboard refresh. Saving or
deleting a layout reloads that copy, and so does a restore, through the
existing restore reload hook (`registryLoader` in `api/backup_handler.go`).

## API

All admin-only, through `RequireAdmin`, which must abort the request, and
audited. Non-admins get 403, and tests must check that the handler did not
run.

| Route | Does |
|---|---|
| `GET /network/faceplate-layouts` | Saved layouts. For each: model, groups, how many devices currently use it, updated by and when. Also the built-in entries, marked built-in, with the same device counts. |
| `GET /devices/:id/faceplate-layout` | What the editor opens with: the effective model; the saved layout for it, if any; the built-in one, if any; the device's main port numbers; whether its own Edit details settings are set; and a **starting layout** (below). |
| `POST /devices/:id/faceplate-preview` | Body: `{groups}`. Returns the device's faceplates drawn with those groups and its live port states, plus `missing` (ports not placed), `extra` (numbers in the layout the device doesn't have) and any rule errors. Changes nothing. |
| `PUT /devices/:id/faceplate-layout` | Body: `{groups, clear_device_settings}`. Saves, or replaces, the layout for this device's effective model. Refused unless the groups place this device's main ports exactly (the same check as the preview). With `clear_device_settings`, it also clears this device's rows, port style and SFP ports in the same transaction. |
| `DELETE /network/faceplate-layouts/:id` | Removes a saved layout. Devices of that model go back to the built-in table or the automatic rules. |

- **Audit actions:** `faceplate_layout_saved` (records the model, the groups
  before and after, and the device it was saved from) and
  `faceplate_layout_deleted`. The resource type is `faceplate_layout`.
- **Empty model:** a device with no effective model can't save a layout. The
  response says to set the model in Edit details first.
- **Starting layout (call):** the saved layout, else the built-in one, else
  the current automatic drawing turned into groups. To turn it into groups,
  each automatic block becomes a group (two-row blocks become `alternate`),
  and blocks that aren't a run of consecutive numbers are split where the
  numbers jump.

## The editor

**Opening it:**
- On the device page, admins see **Fix layout** beside the "Front panel"
  heading. Nobody else sees it.
- It also opens from Settings → Faceplate layouts (below).
- If the device has no model, Fix layout is disabled, with the hint "Set
  this device's model in Edit details first".

**What's in the modal:**
- At the top: "Layout for **UBNT-ES16XG** — used by 3 devices".
- Below that, the groups in order, one row each:
  - **Ports** `[from]`–`[to]`
  - **Type** SFP / RJ45
  - **Rows** 1 / 2
  - **Numbering** "Odd on top" / "Across" (only when Rows is 2)
  - **↑ ↓** and **Remove**
  - Below the list: **+ Add group**, which starts at the next unplaced port
    and runs to the last port.
- **Live preview** under the groups: the real `Faceplate` component, drawn by
  the preview endpoint with this device's live port states, refreshed about
  300 ms after the last change **(call)**. The preview is drawn by the server
  so the editor and the device page can never disagree.
- Under the preview, in plain words:
  - "Not placed: 17, 18"
  - "Not on this device: 15"
  - or "All 16 ports placed".
  - **Save** stays disabled until every port is placed.

**Saving and resetting:**
- **Save for all UBNT-ES16XG devices** saves the layout for the model.
- If the device has its own Edit details settings, a checkbox above Save
  reads "Also clear this device's own faceplate settings, which would
  otherwise override this layout". It's ticked by default **(call)**.
- **Remove saved layout** (only when one exists) asks for confirmation, then
  deletes it.
- **Cancel** discards the changes.

**On the device page afterwards:** if a saved layout exists for the model but
didn't apply because the ports differ, admins see a one-line notice under the
faceplate: "The saved layout for UBNT-ES16XG doesn't fit this device (not
placed: 17). Fix layout". Non-admins see the automatic drawing with no notice.

## Settings → Faceplate layouts

A new admin-only section in the Network group, after Device profiles, at
`/settings/network/faceplate-layouts`.

**The table:**
- Columns: Model, Groups (a short summary such as "1–12 SFP ×2 · 13–16
  RJ45"), Devices, Updated, and Source (Saved or Built in).
- **Edit** opens the editor on one device of that model, picking the device
  with the most recent poll **(call)**. The editor needs real ports for its
  preview, so with no such device Edit is disabled: "No device of this model
  to preview on".
- **Delete** applies to saved rows only.
- Built-in rows can't be edited in place. Editing one saves a layout for that
  model, which then overrides the built-in entry.
- Empty state: "No saved layouts. Open a device's faceplate and choose Fix
  layout to make one."

## Where the code changes

- `portmon`:
  - `Layout` takes an optional saved layout through `LayoutOptions`, applying
    the order of choice above.
  - The groups type and its validation, shared with `models.go`.
  - Drawing a group with `numbering`.
  - Module blocks appended after a saved or built-in layout.
- `services/port_service.go`: looks up the saved layout for the device's
  effective model and passes it in. It also reports when a saved layout
  exists but didn't fit, for the notice above.
- A new service for the table, the in-memory copy, device counts and the
  starting layout.
- New handlers: the routes above, audit records, and the restore reload hook.
- Frontend:
  - the `FaceBlock` type gains numbering where needed
  - `FaceplateLayoutEditor` (modal)
  - Fix layout and the notice on `DeviceDetail`
  - the new Settings section and its `settingsNav.ts` entry

## Testing

**Go unit tests (`portmon`):**
- the order of choice in every case
- exact-coverage fallback (missing, extra, duplicate)
- `alternate` and `across` with odd counts
- module blocks after a layout
- validation rules
- model normalization

All existing layout tests must still pass unchanged.

**Database tests (`./scripts/test-db.sh`):**
- create, replace and delete, with audit rows
- each route refused for a non-admin, with the handler not run
- save refused for a non-matching layout and for an empty model
- `clear_device_settings` clears in the same transaction
- device counts
- a config backup and restore brings the table back and the in-memory copy
  reloads

**Frontend gate:** the `tsc`, `eslint` and `vite build` check in `CLAUDE.md`.

**Owner checks on the sandbox (3200), using the US-48 Pro (`QuantumLink`):**
1. With no saved layout, the faceplate looks exactly as before.
2. Fix layout opens with the current drawing as groups. Change ports 49–52 to
   one row, and the preview updates. Save. The device page shows the new
   drawing, and so does a dashboard port grid widget for that device.
3. Remove a group: Save turns off and "Not placed: …" lists the ports.
4. Set the model in Edit details to something else: the device goes back to
   the automatic drawing. Set it back, and it uses the saved layout again.
5. Settings → Faceplate layouts lists the saved layout with 1 device, plus
   the built-in ES16XG entry. Delete the saved one, and the US-48 Pro goes
   back to the automatic drawing.
6. A non-admin sees no Fix layout, no Settings section, and a 403 from each
   route.

## Left to the builder

- Component structure and styling inside the editor, within the design
  system (slate, `utils/colors.ts`).
- How the in-memory copy is guarded (a mutex or an atomic swap).
- Exact wording of rule errors, in the style of the existing forms.
