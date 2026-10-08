# UX piece 3: deferred follow-ups

This records what was deliberately left for later while executing `2026-10-08-ux-piece3-site-profiles.md` on `feature/ux-piece3`, and the rulings made at the final review.

## Rulings (left for follow-ups at the final review)

- A `LivePorts` failure turns the whole profile into a 500, so notes and networks go too. Consider degrading to ports without rates.
- A circuit update that changes only account number, phone or notes records identical before/after audit values (those fields are redacted). Consider logging the changed field names.
- Logical WAN interfaces (PPPoE, VLAN, tunnel) can be chosen but are not collected by default, so the card always reads "No recent data" with no hint why (`PortView.Collected` exists).
- `PUT /sites/:id/notes` with the field missing clears the notes (it binds a plain string). A script that omits the field wipes them.
- Writes reload outside a transaction, so a failed reload reports 500/404 for a change that was saved and not audited; a unique-index race returns 500 instead of 400.
- Site notes are written to the (admin-only) audit log in full; circuit secrets are not. A door code noted on a site appears in the audit log.

## Deferred minors

Backend:

- No boundary tests for VLAN 1/4094, speeds 100000/0.1, negatives or an IPv4-mapped gateway; a gateway equal to the network or broadcast address, and `0.0.0.0/0`, are accepted.
- Update network/circuit before/after audit values are not checked by tests; rates going to nil (no recent sample) and circuit ordering are untested; some tests can panic on a discarded Profile error or an unchecked index.
- `LivePorts` runs one `LatestMany` per device for all eight live metrics, though only in/out are used.
- Create on a missing site gives a wrapped FK 500 (the handler checks access first); circuit order has no id tie-breaker.
- `bindNetwork` and `bindCircuit` are near duplicates; there is no API test for the port-not-at-site 400 or for clearing notes via PUT.
- Two commit trailers (`fdcab3c` and the Task 4 commit) say Claude Sonnet 5.5 rather than Opus 5.5.

Frontend:

- A single busy flag covers overlapping profile actions; action functions are not memoised; `loading` stays true for an undefined site id; `scanChoices` does not range-check octets.
- An unknown or dormant port `oper_status` shows "Port down".
- The site-delete warning row cannot wrap on a phone.
- Each server row polls `/status` every 20 s.
- `Sites.tsx` builds each card's counts in an IIFE inside the JSX.
- The checks-list edit hint shows to everyone (ruled to stand: anyone signed in can edit their own monitors), so a read-only sharer sees a hint they cannot act on for others' monitors.
