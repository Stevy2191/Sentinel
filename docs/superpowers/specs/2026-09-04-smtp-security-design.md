# SMTP Connection Security (STARTTLS / SSL-TLS)

Date: 2026-09-04
Status: Approved, ready for implementation planning

## Problem

Sentinel's email notification channel cannot talk to a mail server that requires
implicit TLS, and it can leak SMTP credentials in cleartext.

`EmailPlugin.deliver()` (`backend/internal/notifications/email.go`) always opens a
plain TCP connection and then attempts an opportunistic STARTTLS upgrade:

```go
if p.tlsEnabled {
    if ok, _ := client.Extension("STARTTLS"); ok {
        client.StartTLS(&tls.Config{ServerName: p.host})
    }
}
if p.user != "" && p.password != "" {
    if ok, _ := client.Extension("AUTH"); ok {
        client.Auth(smtp.PlainAuth("", p.user, p.password, p.host))
    }
}
```

Three defects follow from this:

1. **No implicit TLS.** Port 465 (SMTPS) expects a TLS handshake immediately on
   connect. There is no `tls.Dial` path, so a 465 server can only hang until the
   timeout. This is the user-visible "SMTP is broken" symptom.
2. **STARTTLS degrades silently.** When the server does not advertise `STARTTLS`,
   the guard falls through and the session continues unencrypted. The connection
   is then used for authentication.
3. **The AUTH guard hides failures.** When `AUTH` is not advertised, authentication
   is skipped entirely and delivery is attempted anyway, producing a confusing
   downstream rejection instead of a clear configuration error.

Separately, the database-backed configuration cannot express any of this:
`notification_configs` has no security column, and `BuildPluginFromConfig`
(`config_plugins.go:130`) hardcodes `tlsEnabled: true`.

## Goals

- Support all three real-world SMTP connection modes: none, STARTTLS, SSL/TLS.
- Make the mode selectable from the admin UI and from the environment.
- Never send credentials over an unencrypted connection.
- Support self-signed certificates on internal mail servers, as an explicit opt-in.
- Break no existing installation.

## Non-goals

- Configurable authentication mechanisms (LOGIN, CRAM-MD5, XOAUTH2). PLAIN remains
  the only mechanism. Explicitly deferred.
- Client certificate authentication.
- Per-monitor or per-recipient SMTP settings.

## Design

### 1. Data model

Migration `backend/migrations/014_smtp_security.sql`:

```sql
ALTER TABLE notification_configs
    ADD COLUMN IF NOT EXISTS smtp_security        VARCHAR(16),
    ADD COLUMN IF NOT EXISTS smtp_skip_tls_verify BOOLEAN NOT NULL DEFAULT false;

UPDATE notification_configs
   SET smtp_security = CASE WHEN smtp_port = 465 THEN 'ssltls' ELSE 'starttls' END
 WHERE channel = 'email' AND smtp_security IS NULL;
```

`smtp_security` is nullable: non-email rows have no use for it, and NULL on the
email row resolves to `starttls` at read time, so the column is never a source of
"unset" ambiguity.

The backfill infers the mode from the port. A row on 465 was previously broken and
starts working; a row on 587 or 25 keeps behaving exactly as it does today.

`models.NotificationConfig` gains:

```go
SMTPSecurity      *string `json:"smtp_security"       gorm:"column:smtp_security"`
SMTPSkipTLSVerify bool    `json:"smtp_skip_tls_verify" gorm:"column:smtp_skip_tls_verify"`
```

alongside constants and a validation set:

```go
const (
    SMTPSecurityNone     = "none"
    SMTPSecuritySTARTTLS = "starttls"
    SMTPSecuritySSLTLS   = "ssltls"
)
var ValidSMTPSecurity = map[string]bool{ ... }
```

`Validate()` rejects an unrecognized `smtp_security` on an email config. A nil
value is valid and means `starttls`.

`HideSecrets()` is unchanged — neither new field is a secret.

`NotificationConfigService.DeleteConfig` clears fields through an explicit map;
both new columns are added to it (`"smtp_security": nil`,
`"smtp_skip_tls_verify": false`) so a reset is complete.

`CreateOrUpdateConfig` uses `Save()`, a full-row write, so the non-pointer bool
persists correctly including when false. No change needed there.

### 2. Plugin

`EmailPlugin` replaces `tlsEnabled bool` with:

```go
security      string // one of models.SMTPSecurity*; "" resolves to starttls
skipTLSVerify bool
```

The constants live in `models` because that is where `Validate()` needs them, and
`email.go` imports `models` to reference them. That import direction already exists
in the package (`config_plugins.go` imports `models`), so it introduces no new
coupling. All references below are qualified as `models.SMTPSecurity*`.

`deliver()` chooses its connection by mode:

```go
tlsCfg := &tls.Config{ServerName: p.host, InsecureSkipVerify: p.skipTLSVerify}

switch p.security {
case models.SMTPSecuritySSLTLS:
    d := &tls.Dialer{NetDialer: &net.Dialer{}, Config: tlsCfg}
    conn, err = d.DialContext(ctx, "tcp", addr)
default:
    conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", addr)
}
```

STARTTLS becomes strict. When the mode is `starttls` and the server does not
advertise the extension, that is a `nonRetriable` error naming the remedy, not a
silent downgrade:

```go
if p.security == models.SMTPSecuritySTARTTLS {
    if ok, _ := client.Extension("STARTTLS"); !ok {
        return nonRetriable{errors.New(
            "server does not support STARTTLS; use SSL/TLS (port 465) or None")}
    }
    if err := client.StartTLS(tlsCfg); err != nil {
        return fmt.Errorf("STARTTLS failed: %w", err)
    }
}
```

Authentication becomes strict for the same reason: credentials configured against
a server that does not advertise `AUTH` is a configuration error, not something to
proceed past.

The cleartext-credential hole closes as a consequence rather than through new
logic. Go's `smtp.PlainAuth.Start` refuses to run over an unencrypted connection
for any non-localhost host, so `security = none` plus a password now fails at the
auth step. That error is wrapped as `nonRetriable` with a message stating that
authentication requires STARTTLS or SSL/TLS, instead of surfacing Go's bare
"unencrypted connection" string.

`nonRetriable` is the correct classification for all three: `sendWithRetry` must
not burn its backoff attempts on a misconfiguration that cannot resolve itself.

### 3. Configuration plumbing

`NewEmailPluginFromConfig` takes `security string, skipVerify bool` in place of
`tlsEnabled bool`. `BuildPluginFromConfig` passes the stored values through,
replacing the hardcoded `true`.

The environment path gains two variables, read in `NewEmailPlugin`:

- `SMTP_SECURITY` — `none` | `starttls` | `ssltls`. An unrecognized value is a
  startup error naming the field, matching how `SMTP_PORT` and `SMTP_FROM` already
  behave.
- `SMTP_SKIP_TLS_VERIFY` — bool, default false.

When `SMTP_SECURITY` is unset, the existing `SMTP_TLS` bool is the fallback:
`true` maps to `starttls`, `false` maps to `none`. With neither set the default is
`starttls`, which is today's behavior. No existing `.env` changes meaning.

Both variables are added to `docker-compose.yml` and `.env.example`.

The API handler binds `models.NotificationConfig` directly
(`notification_config_handler.go:68`), so both fields are exposed over the API
with no handler change.

### 4. Frontend

`useNotificationConfig.ts` adds to its config interface:

```ts
smtp_security?: 'none' | 'starttls' | 'ssltls' | null
smtp_skip_tls_verify?: boolean | null
```

`NotificationSettings.tsx` adds to the email section:

- A **Security** select: `STARTTLS (recommended)` / `SSL/TLS` / `None`.
- A **Skip certificate verification** checkbox, rendered only when security is not
  `none`, carrying an inline warning that it disables certificate validation.

Both fields join the form state, the `formFromConfig` hydration, and the payload
builder. Security defaults to `starttls` for an unconfigured channel.

Selecting a security mode retargets the port **only when the current port is one of
the three known defaults** (25, 587, 465). A hand-entered port such as 2525 is
never overwritten.

Security `none` combined with a password shows a non-blocking inline warning
("credentials will not be sent over an unencrypted connection"), not a validation
error. It must not block saving: Go permits PLAIN over cleartext when the host is
localhost, so a local relay with credentials is a legitimate configuration that the
frontend cannot reliably distinguish. The authoritative check stays in the plugin,
where the host is known, and the Test Connection button surfaces it immediately.
The backend `Validate()` likewise does not reject this combination, so both layers
agree on what is saveable.

The existing helper line becomes mode-aware, e.g.
`STARTTLS: port 587 · SSL/TLS: port 465 · Gmail: smtp.gmail.com:587`.

### 5. Testing

This introduces the repository's first `_test.go` files. The change makes a
security claim, and hand-verifying it would require three differently configured
mail servers, so the claim needs mechanical proof.

`backend/internal/notifications/email_test.go` runs an in-process fake SMTP server
on a `net.Listener` with a scripted greeting and EHLO capability list, letting each
mode be exercised without a network:

| Test | Asserts |
|---|---|
| Implicit TLS on 465 | `ssltls` completes a TLS handshake on connect and delivers |
| STARTTLS upgrade | `starttls` against a server advertising it delivers over TLS |
| STARTTLS strictness | `starttls` against a server *not* advertising it errors, is `nonRetriable`, and sends no credentials |
| Cleartext auth refusal | `none` plus a password errors before any `AUTH` command reaches the wire |
| No-auth relay | `none` with empty credentials delivers, covering internal port-25 relays |
| Cert verification | a self-signed server fails by default and succeeds with `skipTLSVerify` |

The fake server records every command it receives, so "sends no credentials" is
asserted against the actual protocol transcript rather than inferred from the
returned error. The TLS cases generate an ephemeral self-signed certificate at test
setup (`crypto/x509` + `crypto/ecdsa`) so no fixture files are committed; the
verification test trusts it via a custom `RootCAs` pool for the success case and an
empty pool for the failure case.

`backend/internal/models/notification_config_test.go` covers `Validate()`:
accepted values, a rejected unrecognized value, and nil meaning `starttls`.

## Risks

- **Strict STARTTLS is a behavior change.** An installation whose server does not
  advertise STARTTLS is currently sending mail in cleartext and will start failing
  with an explicit error. This is the intended correction, and the error names the
  two ways to resolve it. Worth a line in the changelog.
- **Backfill heuristic.** A server on a non-standard implicit-TLS port (not 465)
  is backfilled to `starttls` and will need one manual UI correction. Rare enough
  to accept over a more elaborate probe.

## Out of scope, noted for later

`README.md` references `docs/INSTALLATION.md`, `docs/API.md`,
`docs/NOTIFICATIONS.md`, and `CONTRIBUTING.md`, none of which exist. The new
environment variables belong in `docs/NOTIFICATIONS.md` when it is written.
