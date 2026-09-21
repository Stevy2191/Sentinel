package models

import "testing"

func strptr(s string) *string { return &s }

func TestResolveSMTPSecurity(t *testing.T) {
	cases := []struct {
		name string
		in   *string
		want string
	}{
		{"nil means starttls", nil, SMTPSecuritySTARTTLS},
		{"empty means starttls", strptr(""), SMTPSecuritySTARTTLS},
		{"none", strptr(SMTPSecurityNone), SMTPSecurityNone},
		{"starttls", strptr(SMTPSecuritySTARTTLS), SMTPSecuritySTARTTLS},
		{"ssltls", strptr(SMTPSecuritySSLTLS), SMTPSecuritySSLTLS},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveSMTPSecurity(c.in); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// emailConfig builds a minimally valid email config for validation tests.
func emailConfig(security *string) *NotificationConfig {
	port := 587
	return &NotificationConfig{
		Channel:      "email",
		SMTPHost:     strptr("smtp.example.com"),
		SMTPPort:     &port,
		SMTPUser:     strptr("user@example.com"),
		SMTPFrom:     strptr("sentinel@example.com"),
		SMTPTo:       strptr("ops@example.com"),
		SMTPSecurity: security,
	}
}

func TestValidateSMTPSecurity(t *testing.T) {
	t.Run("accepts every valid mode", func(t *testing.T) {
		for mode := range ValidSMTPSecurity {
			if err := emailConfig(strptr(mode)).Validate(); err != nil {
				t.Errorf("mode %q should be valid: %v", mode, err)
			}
		}
	})

	t.Run("nil is valid and means starttls", func(t *testing.T) {
		if err := emailConfig(nil).Validate(); err != nil {
			t.Errorf("nil security should be valid: %v", err)
		}
	})

	t.Run("rejects an unrecognized mode", func(t *testing.T) {
		// "tls" is the plausible wrong guess this rejection exists to catch.
		if err := emailConfig(strptr("tls")).Validate(); err == nil {
			t.Error("expected an error for an unrecognized security mode")
		}
	})

	// "none" plus a password must stay saveable: net/smtp permits cleartext
	// credentials to localhost, so the plugin makes that call at send time.
	t.Run("allows none with a password", func(t *testing.T) {
		cfg := emailConfig(strptr(SMTPSecurityNone))
		cfg.SMTPPassword = strptr("secret")
		if err := cfg.Validate(); err != nil {
			t.Errorf("none + password should be saveable: %v", err)
		}
	})
}

func TestHideSecretsKeepsSecurityFields(t *testing.T) {
	cfg := emailConfig(strptr(SMTPSecuritySSLTLS))
	cfg.SMTPPassword = strptr("secret")
	cfg.SMTPSkipTLSVerify = true
	cfg.HideSecrets()

	if cfg.SMTPPassword != nil {
		t.Error("password must be stripped")
	}
	// Neither is a secret; the UI needs both to render the current settings.
	if cfg.SMTPSecurity == nil || *cfg.SMTPSecurity != SMTPSecuritySSLTLS {
		t.Error("security mode must survive HideSecrets")
	}
	if !cfg.SMTPSkipTLSVerify {
		t.Error("skip-verify flag must survive HideSecrets")
	}
}

func TestDestinationKeyDistinguishesWebhooks(t *testing.T) {
	str := func(s string) *string { return &s }

	a := &NotificationConfig{Channel: "slack", WebhookURL: str("https://hooks.slack.com/services/T1/B1/aaa")}
	b := &NotificationConfig{Channel: "slack", WebhookURL: str("https://hooks.slack.com/services/T2/B2/bbb")}
	// Both summarise to "hooks.slack.com", so identity must not come from the
	// display summary or two legitimate Slack channels would collide.
	if a.Summary() != b.Summary() {
		t.Fatalf("precondition: expected equal summaries, got %q and %q", a.Summary(), b.Summary())
	}
	if a.DestinationKey() == b.DestinationKey() {
		t.Error("two different webhook URLs must not share a destination key")
	}

	same := &NotificationConfig{Channel: "slack", WebhookURL: str("https://hooks.slack.com/services/T1/B1/aaa/")}
	if a.DestinationKey() != same.DestinationKey() {
		t.Error("a trailing slash does not change where a webhook delivers")
	}
}

func TestDestinationKeyNtfy(t *testing.T) {
	str := func(s string) *string { return &s }

	base := &NotificationConfig{Channel: "ntfy", NtfyURL: str("https://ntfy.sh"), NtfyTopic: str("alerts")}
	// Case and a trailing slash do not change the destination.
	variant := &NotificationConfig{Channel: "ntfy", NtfyURL: str("https://ntfy.sh/"), NtfyTopic: str("Alerts")}
	if base.DestinationKey() != variant.DestinationKey() {
		t.Errorf("expected the same key, got %q and %q", base.DestinationKey(), variant.DestinationKey())
	}

	other := &NotificationConfig{Channel: "ntfy", NtfyURL: str("https://ntfy.sh"), NtfyTopic: str("different")}
	if base.DestinationKey() == other.DestinationKey() {
		t.Error("different topics are different destinations")
	}
}

func TestValidateEmailRequiresDestination(t *testing.T) {
	t.Run("rejects a missing destination", func(t *testing.T) {
		cfg := emailConfig(nil)
		cfg.SMTPTo = nil
		if err := cfg.Validate(); err == nil {
			t.Error("expected an error when smtp_to is unset")
		}
	})

	t.Run("rejects a blank destination", func(t *testing.T) {
		cfg := emailConfig(nil)
		cfg.SMTPTo = strptr("   ")
		if err := cfg.Validate(); err == nil {
			t.Error("expected an error when smtp_to is blank")
		}
	})

	t.Run("rejects a malformed address", func(t *testing.T) {
		cfg := emailConfig(nil)
		cfg.SMTPTo = strptr("not-an-email")
		if err := cfg.Validate(); err == nil {
			t.Error("expected an error for a malformed destination address")
		}
	})

	t.Run("accepts several comma-separated addresses", func(t *testing.T) {
		cfg := emailConfig(nil)
		cfg.SMTPTo = strptr("ops@example.com, manager@example.com")
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected multiple addresses to be valid: %v", err)
		}
	})
}

// The motivating scenario: two channels sharing one SMTP account but
// alerting different people must not be flagged as duplicates of each other.
func TestDestinationKeyDistinguishesEmailRecipients(t *testing.T) {
	itTeam := emailConfig(nil)
	itTeam.SMTPTo = strptr("it@example.com")

	managers := emailConfig(nil)
	managers.SMTPTo = strptr("managers@example.com")

	if itTeam.DestinationKey() == managers.DestinationKey() {
		t.Error("channels with different recipients must not share a destination key")
	}

	t.Run("order and case do not change the destination", func(t *testing.T) {
		a := emailConfig(nil)
		a.SMTPTo = strptr("ops@example.com, Manager@Example.com")
		b := emailConfig(nil)
		b.SMTPTo = strptr("manager@example.com,ops@example.com")
		if a.DestinationKey() != b.DestinationKey() {
			t.Errorf("expected the same key regardless of order/case, got %q and %q",
				a.DestinationKey(), b.DestinationKey())
		}
	})
}
