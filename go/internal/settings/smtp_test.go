package settings

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

type smtpTestCapture struct {
	message        string
	authenticated  bool
	tlsEstablished bool
	err            error
}

func TestSendSMTPVerificationCodeTLSAndSTARTTLS(t *testing.T) {
	for _, security := range []string{"tls", "starttls"} {
		t.Run(security, func(t *testing.T) {
			port, roots, captureCh := startSMTPTestServer(t, security, "user", "secret", false)
			cfg := smtpConfig{
				host: "localhost", port: port, security: security,
				username: "user", password: "secret",
				fromAddress: "noreply@example.com", fromName: "商品工作台",
			}
			if err := sendSMTPVerificationCode(context.Background(), cfg, "buyer@example.com", "123456", roots); err != nil {
				t.Fatal(err)
			}
			select {
			case capture := <-captureCh:
				if capture.err != nil {
					t.Fatal(capture.err)
				}
				if !capture.tlsEstablished {
					t.Fatal("SMTP transaction was not TLS protected")
				}
				if !capture.authenticated {
					t.Fatal("SMTP AUTH credentials were not accepted")
				}
				if !strings.Contains(capture.message, "123456") {
					t.Fatalf("message does not contain code: %q", capture.message)
				}
				if !strings.Contains(capture.message, "Content-Type: text/plain; charset=UTF-8") {
					t.Fatalf("message content type missing: %q", capture.message)
				}
				if !strings.Contains(capture.message, "From: =?utf-8?q?") {
					t.Fatalf("unicode sender name was not MIME encoded: %q", capture.message)
				}
				if !strings.Contains(capture.message, "您的 ProductFlow 注册验证码是 123456") {
					t.Fatalf("unicode code body missing: %q", capture.message)
				}
				if !strings.Contains(capture.message, "Subject: "+testSMTPSubject()) {
					t.Fatalf("encoded subject missing: %q", capture.message)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("SMTP test server did not finish")
			}
		})
	}
}

func TestSendSMTPVerificationCodeKeepsAcceptedDATAOnQUITFailure(t *testing.T) {
	port, roots, captureCh := startSMTPTestServer(t, "tls", "user", "secret", true)
	cfg := smtpConfig{
		host: "localhost", port: port, security: "tls",
		username: "user", password: "secret", fromAddress: "noreply@example.com",
	}
	if err := sendSMTPVerificationCode(context.Background(), cfg, "buyer@example.com", "123456", roots); err != nil {
		t.Fatalf("accepted DATA should succeed despite QUIT failure: %v", err)
	}
	capture := <-captureCh
	if capture.err != nil {
		t.Fatal(capture.err)
	}
	if !strings.Contains(capture.message, "123456") {
		t.Fatalf("accepted message missing code: %q", capture.message)
	}
}

func TestSendSMTPPasswordResetCodeUsesRecoveryMessage(t *testing.T) {
	for _, security := range []string{"tls", "starttls"} {
		t.Run(security, func(t *testing.T) {
			port, roots, captureCh := startSMTPTestServer(t, security, "user", "secret", false)
			cfg := smtpConfig{
				host: "localhost", port: port, security: security,
				username: "user", password: "secret",
				fromAddress: "noreply@example.com", fromName: "商品工作台",
			}
			if err := sendSMTPPasswordResetCode(context.Background(), cfg, "buyer@example.com", "654321", roots); err != nil {
				t.Fatal(err)
			}
			select {
			case capture := <-captureCh:
				if capture.err != nil {
					t.Fatal(capture.err)
				}
				if !capture.tlsEstablished || !capture.authenticated {
					t.Fatalf("reset mail transport was not protected/authenticated: %#v", capture)
				}
				if !strings.Contains(capture.message, "密码恢复验证码是 654321") || !strings.Contains(capture.message, "只能使用一次") {
					t.Fatalf("recovery message missing purpose/code: %q", capture.message)
				}
				if strings.Contains(capture.message, "注册验证码") {
					t.Fatalf("recovery message used registration purpose: %q", capture.message)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("SMTP test server did not finish")
			}
		})
	}
}

func TestSMTPRejectsUntrustedCertificateAndMissingSTARTTLS(t *testing.T) {
	for _, mode := range []string{"tls", "plain"} {
		t.Run(mode, func(t *testing.T) {
			port, _, captureCh := startSMTPTestServer(t, mode, "user", "secret", false)
			security := mode
			if mode == "plain" {
				security = "starttls"
			}
			cfg := smtpConfig{host: "localhost", port: port, security: security, username: "user", password: "secret", fromAddress: "sender@example.com"}
			if err := sendSMTPVerificationCode(context.Background(), cfg, "buyer@example.com", "123456", nil); err == nil {
				t.Fatal("unsafe SMTP connection accepted")
			}
			capture := <-captureCh
			if capture.authenticated || capture.message != "" {
				t.Fatal("credentials or message sent over rejected connection")
			}
		})
	}
}

func TestVerificationMessageRejectsHeaderInjection(t *testing.T) {
	cfg := smtpConfig{fromAddress: "sender@example.com", fromName: "Sender\r\nBcc: attacker@example.com"}
	if _, err := verificationMessage(cfg, "buyer@example.com", "123456"); err == nil {
		t.Fatal("expected header injection rejection")
	}
	if _, err := verificationMessage(smtpConfig{fromAddress: "sender@example.com"}, "buyer@example.com\nBcc: attacker@example.com", "123456"); err == nil {
		t.Fatal("expected recipient header injection rejection")
	}
}

func TestPasswordResetMessageUsesRecoveryPurposeAndOneTimeCode(t *testing.T) {
	cfg := smtpConfig{fromAddress: "sender@example.com", fromName: "商品工作台"}
	message, err := passwordResetMessage(cfg, "buyer@example.com", "654321")
	if err != nil {
		t.Fatal(err)
	}
	raw := string(message)
	if !strings.Contains(raw, "654321") {
		t.Fatalf("reset message does not contain code: %q", raw)
	}
	if !strings.Contains(raw, "密码恢复") || !strings.Contains(raw, "只能使用一次") {
		t.Fatalf("reset message purpose or one-time wording missing: %q", raw)
	}
	if strings.Contains(raw, "注册验证码") {
		t.Fatalf("reset message uses registration purpose: %q", raw)
	}
	if _, err := passwordResetMessage(cfg, "buyer@example.com\nBcc: attacker@example.com", "654321"); err == nil {
		t.Fatal("expected recipient header injection rejection")
	}
}

func TestSMTPConfigPersistsSecretAndReportsReadiness(t *testing.T) {
	pool, _ := testdb.Open(t)
	store := NewStore(pool, config.Config{})
	ctx := context.Background()
	if err := store.db.WithContext(ctx).Where("key LIKE ?", "smtp_%").Delete(&schema.AppSettings{}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.db.WithContext(ctx).Where("key LIKE ?", "smtp_%").Delete(&schema.AppSettings{}).Error
	})

	if _, err := store.UpdateConfig(ctx, map[string]any{"smtp_host": "localhost"}, nil); err != nil {
		t.Fatal(err)
	}
	available, err := store.RegistrationAvailable(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if available {
		t.Fatal("partial SMTP configuration should not enable registration")
	}
	if _, err := store.UpdateConfig(ctx, map[string]any{"smtp_from_address": "sender\r\nBcc: bad@example.com"}, nil); err == nil {
		t.Fatal("expected header injection rejection")
	}
	view, err := store.UpdateConfig(ctx, map[string]any{
		"smtp_security":     "tls",
		"smtp_username":     "sender@example.com",
		"smtp_password":     "  secret with spaces  ",
		"smtp_from_address": "sender@example.com",
		"smtp_from_name":    "商品工作台",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var passwordItem *ConfigItem
	for i := range view.Items {
		if view.Items[i].Key == "smtp_password" {
			passwordItem = &view.Items[i]
			break
		}
	}
	if passwordItem == nil || !passwordItem.Secret || passwordItem.Value != "" || !passwordItem.HasValue {
		t.Fatalf("password projection %+v", passwordItem)
	}
	available, err = store.RegistrationAvailable(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !available {
		t.Fatal("complete SMTP configuration should enable registration")
	}
	var stored string
	if err := store.db.WithContext(ctx).Table("app_settings").Select("value").Where("key = ?", "smtp_password").Scan(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored != "  secret with spaces  " {
		t.Fatalf("stored password %q", stored)
	}
	exported, err := store.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := exported.RuntimeConfig["smtp_password"]; ok {
		t.Fatal("SMTP password leaked into export")
	}
}

func TestSMTPEnvDefaultsDBOverrideAndReset(t *testing.T) {
	pool, _ := testdb.Open(t)
	store := NewStore(pool, config.Config{
		SMTPHost:        "smtp.env.invalid",
		SMTPPort:        465,
		SMTPSecurity:    "tls",
		SMTPUsername:    "env-user",
		SMTPPassword:    "  env secret  ",
		SMTPFromAddress: "env@example.com",
		SMTPFromName:    "Env Sender",
	})
	ctx := context.Background()
	if err := store.db.WithContext(ctx).Where("key LIKE ?", "smtp_%").Delete(&schema.AppSettings{}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.db.WithContext(ctx).Where("key LIKE ?", "smtp_%").Delete(&schema.AppSettings{}).Error
	})

	view, err := store.ConfigView(ctx)
	if err != nil {
		t.Fatal(err)
	}
	items := map[string]ConfigItem{}
	for _, item := range view.Items {
		if strings.HasPrefix(item.Key, "smtp_") {
			items[item.Key] = item
		}
	}
	if items["smtp_host"].Value != "smtp.env.invalid" || items["smtp_host"].Source != "env_default" {
		t.Fatalf("host env projection %+v", items["smtp_host"])
	}
	if items["smtp_port"].Value != 465 || items["smtp_security"].Value != "tls" {
		t.Fatalf("port/security env projection %+v/%+v", items["smtp_port"], items["smtp_security"])
	}
	if items["smtp_password"].Value != "" || !items["smtp_password"].HasValue || !items["smtp_password"].Secret {
		t.Fatalf("password env projection %+v", items["smtp_password"])
	}
	available, err := store.RegistrationAvailable(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !available {
		t.Fatal("complete env SMTP configuration should be ready")
	}

	view, err = store.UpdateConfig(ctx, map[string]any{"smtp_host": "smtp.db.invalid"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range view.Items {
		if item.Key == "smtp_host" {
			if item.Value != "smtp.db.invalid" || item.Source != "database" {
				t.Fatalf("host DB projection %+v", item)
			}
		}
	}
	view, err = store.UpdateConfig(ctx, nil, []string{"smtp_host", "smtp_password"})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range view.Items {
		switch item.Key {
		case "smtp_host":
			if item.Value != "smtp.env.invalid" || item.Source != "env_default" {
				t.Fatalf("host reset projection %+v", item)
			}
		case "smtp_password":
			if item.Value != "" || !item.HasValue || item.Source != "env_default" {
				t.Fatalf("password reset projection %+v", item)
			}
		}
	}
}

func testSMTPSubject() string {
	return "=?UTF-8?q?ProductFlow_=E9=82=AE=E7=AE=B1=E9=AA=8C=E8=AF=81=E7=A0=81?="
}

func startSMTPTestServer(t *testing.T, security, username, password string, closeAfterData bool) (int, *x509.CertPool, <-chan smtpTestCapture) {
	t.Helper()
	cert, roots := testSMTPCertificate(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	captureCh := make(chan smtpTestCapture, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			captureCh <- smtpTestCapture{err: err}
			return
		}
		captureCh <- serveSMTPTestConnection(conn, security, username, password, cert, closeAfterData)
	}()
	return listener.Addr().(*net.TCPAddr).Port, roots, captureCh
}

func serveSMTPTestConnection(conn net.Conn, security, username, password string, cert tls.Certificate, closeAfterData bool) smtpTestCapture {
	capture := smtpTestCapture{}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	var active net.Conn = conn
	if security == "tls" {
		tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
		if err := tlsConn.Handshake(); err != nil {
			capture.err = err
			return capture
		}
		active = tlsConn
		capture.tlsEstablished = true
	}
	reader := bufio.NewReader(active)
	writer := bufio.NewWriter(active)
	writeSMTPLine(writer, "220 localhost ESMTP")
	startTLSDone := security == "tls"
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			capture.err = err
			return capture
		}
		command := strings.TrimSpace(line)
		fields := strings.Fields(command)
		if len(fields) == 0 {
			writeSMTPLine(writer, "500 malformed command")
			continue
		}
		switch strings.ToUpper(fields[0]) {
		case "EHLO", "HELO":
			if security == "starttls" && !startTLSDone {
				writeSMTPLines(writer, "250-localhost", "250-STARTTLS", "250 AUTH PLAIN")
			} else {
				writeSMTPLines(writer, "250-localhost", "250 AUTH PLAIN")
			}
		case "STARTTLS":
			if security != "starttls" || startTLSDone {
				writeSMTPLine(writer, "454 TLS unavailable")
				continue
			}
			writeSMTPLine(writer, "220 Ready to start TLS")
			tlsConn := tls.Server(active, &tls.Config{Certificates: []tls.Certificate{cert}})
			if err := tlsConn.Handshake(); err != nil {
				capture.err = err
				return capture
			}
			active = tlsConn
			reader = bufio.NewReader(active)
			writer = bufio.NewWriter(active)
			startTLSDone = true
			capture.tlsEstablished = true
		case "AUTH":
			if len(fields) < 3 || strings.ToUpper(fields[1]) != "PLAIN" {
				writeSMTPLine(writer, "504 unsupported auth")
				continue
			}
			decoded, err := base64.StdEncoding.DecodeString(fields[2])
			if err != nil {
				writeSMTPLine(writer, "535 authentication failed")
				continue
			}
			capture.authenticated = string(decoded) == "\x00"+username+"\x00"+password && capture.tlsEstablished
			if capture.authenticated {
				writeSMTPLine(writer, "235 2.7.0 Authentication successful")
			} else {
				writeSMTPLine(writer, "535 authentication failed")
			}
		case "MAIL":
			writeSMTPLine(writer, "250 2.1.0 Ok")
		case "RCPT":
			writeSMTPLine(writer, "250 2.1.5 Ok")
		case "DATA":
			writeSMTPLine(writer, "354 End data with <CR><LF>.<CR><LF>")
			var message strings.Builder
			for {
				dataLine, readErr := reader.ReadString('\n')
				if readErr != nil {
					capture.err = readErr
					return capture
				}
				if dataLine == ".\r\n" || dataLine == ".\n" {
					break
				}
				if strings.HasPrefix(dataLine, "..") {
					dataLine = dataLine[1:]
				}
				message.WriteString(dataLine)
			}
			capture.message = message.String()
			writeSMTPLine(writer, "250 2.0.0 queued")
			if closeAfterData {
				return capture
			}
		case "QUIT":
			writeSMTPLine(writer, "221 2.0.0 Bye")
			return capture
		default:
			writeSMTPLine(writer, "250 2.0.0 Ok")
		}
	}
}

func writeSMTPLine(writer *bufio.Writer, line string) {
	_, _ = writer.WriteString(line + "\r\n")
	_ = writer.Flush()
}

func writeSMTPLines(writer *bufio.Writer, lines ...string) {
	for _, line := range lines {
		_, _ = writer.WriteString(line + "\r\n")
	}
	_ = writer.Flush()
}

func testSMTPCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("failed to add test certificate to root pool")
	}
	return cert, roots
}
