#!/usr/bin/env python3
"""Writes the simulator's .snmprec files: OID|TAG|VALUE lines, OID-sorted.

Tags: 2 INTEGER, 4 OCTET STRING, 4x hex OCTET STRING, 6 OID, 65 Counter32,
66 Gauge32, 67 TimeTicks, 70 Counter64.
"""
import os

OUT = os.path.join(os.path.dirname(__file__), "data")


def oid_key(line):
    return tuple(int(x) for x in line.split("|", 1)[0].split("."))


def system(descr, objid, name, location, uptime_ticks):
    return [
        f"1.3.6.1.2.1.1.1.0|4|{descr}",
        f"1.3.6.1.2.1.1.2.0|6|{objid}",
        f"1.3.6.1.2.1.1.3.0|67|{uptime_ticks}",
        "1.3.6.1.2.1.1.4.0|4|noc@example.test",
        f"1.3.6.1.2.1.1.5.0|4|{name}",
        f"1.3.6.1.2.1.1.6.0|4|{location}",
    ]


def octets(oid, tag, rate):
    """An octet counter growing at `rate` bytes/s (numeric variation module);
    a static 0 for a port with no traffic.

    No `cumulative`: snmpsim's numeric module, in cumulative mode, adds
    rate * (time since the simulator booted) to the *previous* stored value
    on every read (variation/numeric.py's variate(), the `cumulative`
    branch) rather than rate * (time since the previous read) - so the
    counter compounds and runs away (confirmed: two reads 3s apart moved by
    tens of thousands, not ~37500, and the gap widened on every further
    read). Dropping `cumulative` uses the module's plain path instead: each
    read recomputes value = rate * (time since boot) from scratch with no
    carried state, which is exactly a counter increasing at a steady
    `rate` bytes/s - confirmed against a live container to move by
    `rate * elapsed` between two reads, and to still count up correctly
    across a 32-bit wrap.
    """
    if rate == 0:
        return [f"{oid}|{tag}|0"]
    wrap = ",wrap=1,max=4294967295" if tag == 65 else ""
    return [f"{oid}|{tag}:numeric|rate={rate},initial=0{wrap}"]


def ports(n, name_fmt, descr_fmt, speed_bps, xtable=True, mac_base=0x788A20000000):
    rows = []
    for i in range(1, n + 1):
        up = 1 if i % 3 else 2  # every third port is down
        admin = 2 if i == n else 1  # last port administratively down
        t = "1.3.6.1.2.1.2.2.1"
        rows += [
            f"{t}.1.{i}|2|{i}",
            f"{t}.2.{i}|4|{descr_fmt.format(i=i)}",
            f"{t}.3.{i}|2|6",
            f"{t}.5.{i}|66|{min(speed_bps, 4294967295)}",
            f"{t}.6.{i}|4x|{mac_base + i:012x}",
            f"{t}.7.{i}|2|{admin}",
            f"{t}.8.{i}|2|{up if admin == 1 else 2}",
            f"{t}.9.{i}|67|{1000 * i}",
            # Traffic: up ports move (i * 12.5 kB/s in, a quarter of that out),
            # via snmpsim's numeric variation module; errors/discards stay 0.
            *octets(f"{t}.10.{i}", 65, i * 12_500 if up == 1 and admin == 1 else 0),
            *octets(f"{t}.16.{i}", 65, i * 3_125 if up == 1 and admin == 1 else 0),
            f"{t}.13.{i}|65|0", f"{t}.14.{i}|65|0", f"{t}.19.{i}|65|0", f"{t}.20.{i}|65|0",
        ]
        if xtable:
            x = "1.3.6.1.2.1.31.1.1.1"
            rows += [
                f"{x}.1.{i}|4|{name_fmt.format(i=i)}",
                f"{x}.15.{i}|66|{speed_bps // 1_000_000}",
                f"{x}.18.{i}|4|{'Uplink to MDF' if i == 1 else ''}",
                *octets(f"{x}.6.{i}", 70, i * 12_500 if up == 1 and admin == 1 else 0),
                *octets(f"{x}.10.{i}", 70, i * 3_125 if up == 1 and admin == 1 else 0),
                f"{x}.17.{i}|2|1",
            ]
    return rows


def entity(model, serial):
    e = "1.3.6.1.2.1.47.1.1.1.1"
    return [f"{e}.5.1|2|3", f"{e}.11.1|4|{serial}", f"{e}.13.1|4|{model}"]


def write(name, lines):
    os.makedirs(OUT, exist_ok=True)
    with open(os.path.join(OUT, name + ".snmprec"), "w") as f:
        f.write("\n".join(sorted(lines, key=oid_key)) + "\n")


def cisco_health():
    """Rows for the Cisco starter profile's metrics: ENTITY names for CPU/fan/
    sensor labels, CPU load (two indexes, one with no entity so its label
    falls back to "Row <index>"), enhanced and classic memory pools, ENVMON
    temperature/fan/supply, ENTITY-SENSOR temperature and voltage (the latter
    must not be read as a temperature), and FRU fan-tray/power status."""
    ent = "1.3.6.1.2.1.47.1.1.1.1.7"
    names = {1000: "Switch 1", 1004: "Switch 1 - Fan 1", 1005: "Switch 1 - Fan 2", 1010: "Switch 1 - Inlet Temp Sensor",
             1011: "Switch 1 - 12V Rail", 1020: "Switch 1 - FAN-T1", 1030: "Switch 1 - Power Supply A"}
    rows = [f"{ent}.{i}|4|{n}" for i, n in names.items()]
    cpu = "1.3.6.1.4.1.9.9.109.1.1.1.1"
    rows += [f"{cpu}.2.1|2|1000", f"{cpu}.8.1|66|23", f"{cpu}.2.2|2|0", f"{cpu}.8.2|66|4"]
    emp = "1.3.6.1.4.1.9.9.221.1.1.1.1"
    rows += [f"{emp}.3.1.1|4|Processor", f"{emp}.18.1.1|70|300", f"{emp}.20.1.1|70|700"]
    cmp_ = "1.3.6.1.4.1.9.9.48.1.1.1"
    rows += [f"{cmp_}.2.1|4|Processor", f"{cmp_}.5.1|66|600", f"{cmp_}.6.1|66|400"]
    env = "1.3.6.1.4.1.9.9.13.1"
    rows += [f"{env}.3.1.2.1|4|Switch 1 Inlet", f"{env}.3.1.3.1|66|31",
             f"{env}.4.1.2.1|4|Switch 1 Fan 1", f"{env}.4.1.3.1|2|1",
             f"{env}.4.1.2.2|4|Switch 1 Fan 2", f"{env}.4.1.3.2|2|3",
             f"{env}.5.1.2.1|4|Switch 1 PS A", f"{env}.5.1.3.1|2|1",
             f"{env}.5.1.2.2|4|Switch 1 PS B", f"{env}.5.1.3.2|2|5"]
    sen = "1.3.6.1.4.1.9.9.91.1.1.1.1"
    rows += [f"{sen}.1.1010|2|8", f"{sen}.3.1010|2|1", f"{sen}.4.1010|2|415",
             f"{sen}.1.1011|2|4", f"{sen}.3.1011|2|3", f"{sen}.4.1011|2|12000"]
    fru = "1.3.6.1.4.1.9.9.117.1"
    rows += [f"{fru}.4.1.1.1.1020|2|2", f"{fru}.1.2.1.2.1030|2|2"]
    return rows


edgeswitch = (system("EdgeSwitch 48-Port 500W, 1.9.3.5372984", "1.3.6.1.4.1.4413", "sim-edgeswitch", "Simulator rack", 123456789)
              + ports(52, "0/{i}", "Slot: 0 Port: {i} Gigabit - Level", 1_000_000_000)
              + entity("ES-48-500W", "SIMSERIAL001"))
cisco = (system("Cisco IOS Software, C2960X Software (C2960X-UNIVERSALK9-M), Version 15.2(7)E8", "1.3.6.1.4.1.9.1.1208",
                "sim-cisco", "Simulator rack", 98765432)
         + ports(26, "Gi1/0/{i}", "GigabitEthernet1/0/{i}", 1_000_000_000, mac_base=0x00AABB000000)
         + entity("WS-C2960X-24TS-L", "SIMSERIAL002")
         + cisco_health())
radio = (system("Cambium ePMP 1000", "1.3.6.1.4.1.17713.21", "sim-radio", "Tower 3", 5555555)
         + ports(2, "", "eth{i}", 100_000_000, xtable=False, mac_base=0x000456000000))

write("edgeswitch", edgeswitch)
write("public", edgeswitch)  # community "public" (SNMPv1/2c) reads this one
# snmpsim 1.2.2 does not map an empty SNMPv3 context name to public.snmprec;
# it only resolves an empty context to a file literally named self.snmprec
# (its "ident" logic strips the literal prefix "self" from "self.snmprec",
# leaving an empty identifier). See deploy/snmpsim/README.md.
write("self", edgeswitch)
write("cisco", cisco)
write("radio", radio)
print("wrote", sorted(os.listdir(OUT)))
