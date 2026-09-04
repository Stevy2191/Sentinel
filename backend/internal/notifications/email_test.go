package notifications

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// These tests exercise EmailPlugin.deliver against an in-process fake SMTP
// server. The point is the security behaviour: that implicit TLS actually
// handshakes on connect, that STARTTLS is required rather than opportunistic,
// and - asserted against the recorded protocol transcript rather than inferred
// from an error string - that credentials never reach the wire when the
// connection is not safe to carry them.

// ---- fake SMTP server ------------------------------------------------------

type fakeSMTPOpts struct {
	implicitTLS       bool
	advertiseSTARTTLS bool
	advertiseAUTH     bool
	tlsConfig         *tls.Config
}

type fakeSMTP struct {
	host string
	port int
	ln   net.Listener

	mu   sync.Mutex
	cmds []string
}

// startFakeSMTP listens on an ephemeral loopback port and serves exactly one
// SMTP conversation, recording every command line it receives.
func startFakeSMTP(t *testing.T, opts fakeSMTPOpts) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}

	f := &fakeSMTP{host: host, port: port, ln: ln}
	go f.serveOnce(opts)
	t.Cleanup(func() { _ = ln.Close() })
	return f
}

func (f *fakeSMTP) record(line string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cmds = append(f.cmds, line)
}

// sawCommand reports whether any recorded line starts with prefix (case-insensitive).
func (f *fakeSMTP) sawCommand(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.cmds {
		if strings.HasPrefix(strings.ToUpper(c), strings.ToUpper(prefix)) {
			return true
		}
	}
	return false
}

func (f *fakeSMTP) transcript() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.cmds, " | ")
}

func (f *fakeSMTP) serveOnce(opts fakeSMTPOpts) {
	conn, err := f.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	if opts.implicitTLS {
		tconn := tls.Server(conn, opts.tlsConfig)
		if err := tconn.Handshake(); err != nil {
			return
		}
		conn = tconn
	}
	f.handle(conn, opts)
}

// handle speaks just enough SMTP for net/smtp to complete a send.
func (f *fakeSMTP) handle(conn net.Conn, opts fakeSMTPOpts) {
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	write := func(s string) {
		_, _ = rw.WriteString(s)
		_ = rw.Flush()
	}

	write("220 fake.test ESMTP ready\r\n")

	inData := false
	for {
		line, err := rw.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")

		if inData {
			// Message body: swallow until the lone "." terminator.
			if line == "." {
				inData = false
				write("250 OK queued\r\n")
			}
			continue
		}

		f.record(line)
		up := strings.ToUpper(line)

		switch {
		case strings.HasPrefix(up, "EHLO"), strings.HasPrefix(up, "HELO"):
			_, isTLS := conn.(*tls.Conn)
			var caps []string
			if opts.advertiseSTARTTLS && !isTLS {
				caps = append(caps, "STARTTLS")
			}
			if opts.advertiseAUTH {
				caps = append(caps, "AUTH PLAIN LOGIN")
			}
			write(ehloResponse(caps))

		case up == "STARTTLS":
			if !opts.advertiseSTARTTLS {
				write("500 5.5.1 unrecognized command\r\n")
				continue
			}
			write("220 2.0.0 Ready to start TLS\r\n")
			tconn := tls.Server(conn, opts.tlsConfig)
			if err := tconn.Handshake(); err != nil {
				return
			}
			conn = tconn
			rw = bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
			write = func(s string) {
				_, _ = rw.WriteString(s)
				_ = rw.Flush()
			}

		case strings.HasPrefix(up, "AUTH"):
			write("235 2.7.0 Authentication successful\r\n")

		case strings.HasPrefix(up, "MAIL FROM"), strings.HasPrefix(up, "RCPT TO"):
			write("250 2.1.0 OK\r\n")

		case up == "DATA":
			write("354 End data with <CR><LF>.<CR><LF>\r\n")
			inData = true

		case up == "QUIT":
			write("221 2.0.0 Bye\r\n")
			return

		default:
			write("250 2.0.0 OK\r\n")
		}
	}
}

// ehloResponse formats a multiline 250 reply; the final line uses a space.
func ehloResponse(caps []string) string {
	if len(caps) == 0 {
		return "250 fake.test\r\n"
	}
	var b strings.Builder
	b.WriteString("250-fake.test\r\n")
	for i, c := range caps {
		sep := "-"
		if i == len(caps)-1 {
			sep = " "
		}
		fmt.Fprintf(&b, "250%s%s\r\n", sep, c)
	}
	return b.String()
}

// selfSignedCert builds an ephemeral certificate valid for 127.0.0.1, so no
// fixture files need to be committed.
func selfSignedCert(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "fake.test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
}

// testPlugin builds a plugin pointed at the fake server, with logging discarded.
func testPlugin(f *fakeSMTP, security string, user, password string, skipVerify bool) *EmailPlugin {
	return &EmailPlugin{
		host:          f.host,
		port:          f.port,
		user:          user,
		password:      password,
		from:          "sentinel@fake.test",
		to:            []string{"ops@fake.test"},
		security:      security,
		skipTLSVerify: skipVerify,
		logger:        log.New(io.Discard, "", 0),
	}
}

func testMIME() string {
	return "Subject: test\r\n\r\nbody\r\n"
}

// ---- tests -----------------------------------------------------------------

// Implicit TLS (SMTPS) is the mode that could not work at all before: the server
// handshakes on connect and never sends a plaintext greeting.
func TestDeliverImplicitTLSConnects(t *testing.T) {
	tlsCfg := selfSignedCert(t)
	f := startFakeSMTP(t, fakeSMTPOpts{implicitTLS: true, advertiseAUTH: true, tlsConfig: tlsCfg})
	p := testPlugin(f, models.SMTPSecuritySSLTLS, "user@fake.test", "secret", true)

	if err := p.deliver(context.Background(), testMIME()); err != nil {
		t.Fatalf("deliver over implicit TLS: %v (transcript: %s)", err, f.transcript())
	}
	if !f.sawCommand("AUTH") {
		t.Errorf("expected AUTH over implicit TLS, transcript: %s", f.transcript())
	}
}

// STARTTLS upgrades, then authenticates over the upgraded connection.
func TestDeliverSTARTTLSUpgrades(t *testing.T) {
	tlsCfg := selfSignedCert(t)
	f := startFakeSMTP(t, fakeSMTPOpts{advertiseSTARTTLS: true, advertiseAUTH: true, tlsConfig: tlsCfg})
	p := testPlugin(f, models.SMTPSecuritySTARTTLS, "user@fake.test", "secret", true)

	if err := p.deliver(context.Background(), testMIME()); err != nil {
		t.Fatalf("deliver over STARTTLS: %v (transcript: %s)", err, f.transcript())
	}
	if !f.sawCommand("STARTTLS") {
		t.Errorf("expected a STARTTLS command, transcript: %s", f.transcript())
	}
	if !f.sawCommand("AUTH") {
		t.Errorf("expected AUTH after upgrade, transcript: %s", f.transcript())
	}
}

// The regression this change exists for: a server that does not advertise
// STARTTLS must abort, not silently continue in cleartext and authenticate.
func TestDeliverSTARTTLSStrictWhenUnsupported(t *testing.T) {
	f := startFakeSMTP(t, fakeSMTPOpts{advertiseSTARTTLS: false, advertiseAUTH: true})
	p := testPlugin(f, models.SMTPSecuritySTARTTLS, "user@fake.test", "secret", false)

	err := p.deliver(context.Background(), testMIME())
	if err == nil {
		t.Fatalf("expected an error when STARTTLS is unsupported, transcript: %s", f.transcript())
	}
	var nr nonRetriable
	if !errors.As(err, &nr) {
		t.Errorf("expected a nonRetriable error, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("error should name STARTTLS, got: %v", err)
	}
	// The security property, asserted against the wire and not the error text.
	if f.sawCommand("AUTH") {
		t.Errorf("credentials sent over an unencrypted connection, transcript: %s", f.transcript())
	}
	if f.sawCommand("MAIL FROM") {
		t.Errorf("delivery continued after a failed upgrade, transcript: %s", f.transcript())
	}
}

// Credentials configured against a server with no AUTH is a configuration error,
// not something to proceed past with an unauthenticated send.
func TestDeliverErrorsWhenAuthUnadvertised(t *testing.T) {
	tlsCfg := selfSignedCert(t)
	f := startFakeSMTP(t, fakeSMTPOpts{advertiseSTARTTLS: true, advertiseAUTH: false, tlsConfig: tlsCfg})
	p := testPlugin(f, models.SMTPSecuritySTARTTLS, "user@fake.test", "secret", true)

	err := p.deliver(context.Background(), testMIME())
	if err == nil {
		t.Fatalf("expected an error when AUTH is unadvertised, transcript: %s", f.transcript())
	}
	var nr nonRetriable
	if !errors.As(err, &nr) {
		t.Errorf("expected a nonRetriable error, got %T: %v", err, err)
	}
	if f.sawCommand("MAIL FROM") {
		t.Errorf("delivery continued without authentication, transcript: %s", f.transcript())
	}
}

// An internal relay on port 25 with no credentials stays supported.
func TestDeliverPlaintextRelayWithoutCredentials(t *testing.T) {
	f := startFakeSMTP(t, fakeSMTPOpts{advertiseAUTH: false})
	p := testPlugin(f, models.SMTPSecurityNone, "", "", false)

	if err := p.deliver(context.Background(), testMIME()); err != nil {
		t.Fatalf("plaintext relay delivery: %v (transcript: %s)", err, f.transcript())
	}
	if f.sawCommand("AUTH") {
		t.Errorf("unexpected AUTH with no credentials, transcript: %s", f.transcript())
	}
	if !f.sawCommand("MAIL FROM") {
		t.Errorf("expected delivery to proceed, transcript: %s", f.transcript())
	}
}

// A self-signed certificate is rejected by default and accepted only on opt-in.
func TestDeliverCertificateVerification(t *testing.T) {
	t.Run("rejected by default", func(t *testing.T) {
		tlsCfg := selfSignedCert(t)
		f := startFakeSMTP(t, fakeSMTPOpts{implicitTLS: true, advertiseAUTH: true, tlsConfig: tlsCfg})
		p := testPlugin(f, models.SMTPSecuritySSLTLS, "user@fake.test", "secret", false)

		if err := p.deliver(context.Background(), testMIME()); err == nil {
			t.Fatal("expected a self-signed certificate to be rejected")
		}
	})

	t.Run("accepted when skipping verification", func(t *testing.T) {
		tlsCfg := selfSignedCert(t)
		f := startFakeSMTP(t, fakeSMTPOpts{implicitTLS: true, advertiseAUTH: true, tlsConfig: tlsCfg})
		p := testPlugin(f, models.SMTPSecuritySSLTLS, "user@fake.test", "secret", true)

		if err := p.deliver(context.Background(), testMIME()); err != nil {
			t.Fatalf("expected skipTLSVerify to accept a self-signed cert: %v", err)
		}
	})
}

// allowCleartextAuth is the guard that decides whether credentials may be sent.
// It is unit-tested directly because the loopback exemption cannot be exercised
// end-to-end: a fake server necessarily listens on 127.0.0.1.
func TestAllowCleartextAuth(t *testing.T) {
	cases := []struct {
		security string
		host     string
		want     bool
	}{
		{models.SMTPSecurityNone, "smtp.example.com", false},
		{models.SMTPSecurityNone, "192.0.2.10", false},
		{models.SMTPSecurityNone, "localhost", true},
		{models.SMTPSecurityNone, "LOCALHOST", true},
		{models.SMTPSecurityNone, "127.0.0.1", true},
		{models.SMTPSecurityNone, "::1", true},
		{models.SMTPSecuritySTARTTLS, "smtp.example.com", true},
		{models.SMTPSecuritySSLTLS, "smtp.example.com", true},
	}
	for _, c := range cases {
		if got := allowCleartextAuth(c.security, c.host); got != c.want {
			t.Errorf("allowCleartextAuth(%q, %q) = %v, want %v", c.security, c.host, got, c.want)
		}
	}
}

func TestResolveSecurityFromEnv(t *testing.T) {
	cases := []struct {
		name     string
		security string
		tls      string
		want     string
		wantErr  bool
	}{
		{name: "explicit ssltls", security: "ssltls", want: models.SMTPSecuritySSLTLS},
		{name: "explicit none", security: "none", want: models.SMTPSecurityNone},
		{name: "case insensitive", security: "STARTTLS", want: models.SMTPSecuritySTARTTLS},
		{name: "invalid value is an error", security: "tls", wantErr: true},
		{name: "unset defaults to starttls", want: models.SMTPSecuritySTARTTLS},
		// Backward compatibility with the older boolean.
		{name: "legacy SMTP_TLS=false", tls: "false", want: models.SMTPSecurityNone},
		{name: "legacy SMTP_TLS=true", tls: "true", want: models.SMTPSecuritySTARTTLS},
		{name: "SMTP_SECURITY wins over SMTP_TLS", security: "ssltls", tls: "false", want: models.SMTPSecuritySSLTLS},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("SMTP_SECURITY", c.security)
			t.Setenv("SMTP_TLS", c.tls)
			got, err := resolveSecurityFromEnv()
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected an error for SMTP_SECURITY=%q", c.security)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
