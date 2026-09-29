# SNMP simulator (development and test only)

Not part of any release; used only to give Sentinel's SNMP client (`backend/internal/snmp`)
real devices to talk to for local development and the `TestSim*` end-to-end tests.

Start it with `SNMPSIM_VERSION=1.2.2 ./run.sh` (the pinned version is also the Dockerfile's
default `SNMPSIM_VERSION` build arg). It builds and runs a container named `sentinel-snmpsim`
listening on UDP `127.0.0.1:1161-1190`, and joins it to the `sentinel-dev_default` network
(as `sentinel-snmpsim`) when that network exists, so the dev backend can reach it too.

Regenerate the simulated data (four device profiles) with `python3 gen_data.py`.

## Simulated devices (select by SNMPv1/v2c community)

- `edgeswitch` — Ubiquiti EdgeSwitch, 52 ports, ENTITY-MIB present
- `cisco` — Cisco IOS switch, 26 ports, ENTITY-MIB present
- `radio` — Cambium radio, v1-style: no ifXTable, no ENTITY-MIB
- `public` — same data as `edgeswitch` (community `public`)

## SNMPv3 users (all resolve to the EdgeSwitch data)

- `sentinel-auth` — authNoPriv, SHA, password `authpass123`
- `sentinel-priv` — authPriv, SHA/AES, `authpass123` / `privpass123`

## Running the Go tests against it

`SENTINEL_TEST_SNMPSIM=127.0.0.1:1161 go test ./internal/snmp/ -run TestSim` (from `backend/`).
Without the env var the four `TestSim*` tests skip.

## Deviations from the naive setup (snmpsim 1.2.2, pinned via `pip index versions snmpsim`)

- **Missing runtime dependencies.** `pip install snmpsim` alone is not enough to run it:
  - `pysmi` is imported unconditionally by `snmpsim.utils`, but is only declared under the
    package's `dev` extra, so plain `pip install snmpsim` omits it and the responder crashes
    at startup with `ModuleNotFoundError: No module named 'pysmi'`.
  - `cryptography` isn't declared at all, but pysnmp's USM privacy layer needs it for AES/DES.
    Without it, SNMPv3 `authPriv` requests fail server-side with `DecryptionError('Ciphering
    services not available or ciphertext is broken')` — indistinguishable from a bad key
    unless you read the server log.

  The Dockerfile installs both alongside `snmpsim` to work around this.

- **Empty SNMPv3 context does not map to `public.snmprec`.** In 1.2.2, a request's
  `(community | context name)` is turned into a "file identifier" and looked up among the
  data files; an **empty** context name is dropped from that candidate list entirely (see
  `snmpsim/datafile.py:probe_context`), so it never resolves to a file named after a
  community like `public`. The one way to register a file under the *empty* identifier is
  to name it literally `self.snmprec` — `datafile.py`'s `process_file_extension` strips the
  literal `self` prefix from that one reserved filename, producing an empty ident. `gen_data.py`
  therefore writes a `self.snmprec` (identical content to `edgeswitch.snmprec`) in addition to
  `public.snmprec`; only `self.snmprec` is actually reached by an SNMPv3 request with no
  context name (which is what `gosnmp`/Sentinel's client sends, since `Credential` carries no
  context field). `public.snmprec` is kept for the (untested) SNMPv1/v2c community `public`.

Verify manually: `docker run --rm --network host alpine sh -c "apk add -q net-snmp-tools && snmpget -v2c -c edgeswitch 127.0.0.1:1161 1.3.6.1.2.1.1.5.0"` →
`STRING: "sim-edgeswitch"`; and `snmpget -v3 -l authPriv -u sentinel-priv -a SHA -A authpass123 -x AES -X privpass123 127.0.0.1:1161 1.3.6.1.2.1.1.5.0` → same.
