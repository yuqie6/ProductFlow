package settings

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

const (
	defaultSMTPPort     = 587
	defaultSMTPSecurity = "starttls"

	smtpDialTimeout        = 5 * time.Second
	smtpTransactionTimeout = 15 * time.Second
)

type smtpConfig struct {
	host        string
	port        int
	security    string
	username    string
	password    string
	fromAddress string
	fromName    string
}

func isSMTPConfigKey(key string) bool {
	return strings.HasPrefix(key, "smtp_")
}

func normalizeSMTPConfigValue(def configDefinition, value any) (string, error) {
	switch def.Key {
	case "smtp_port":
		text := strings.TrimSpace(fmt.Sprint(value))
		if value == nil {
			text = ""
		}
		port, err := strconv.Atoi(text)
		if err != nil || port < 1 || port > 65535 {
			return "", apperr.Validation(def.Label + " 必须是 1 到 65535 之间的整数")
		}
		return strconv.Itoa(port), nil
	case "smtp_security":
		text := strings.TrimSpace(fmt.Sprint(value))
		if value == nil {
			text = ""
		}
		if text != "starttls" && text != "tls" {
			return "", apperr.Validation(def.Label + " 必须是 starttls 或 tls")
		}
		return text, nil
	case "smtp_host", "smtp_username", "smtp_password", "smtp_from_address", "smtp_from_name":
		text := fmt.Sprint(value)
		if value == nil {
			text = ""
		}
		if def.Key != "smtp_password" {
			text = strings.TrimSpace(text)
		}
		if hasSMTPControl(text) {
			return "", apperr.Validation(def.Label + " 不能包含换行或控制字符")
		}
		if def.Key == "smtp_host" && strings.IndexFunc(text, unicode.IsSpace) >= 0 {
			return "", apperr.Validation(def.Label + " 不能包含空白字符")
		}
		if def.Key == "smtp_from_address" && text != "" {
			if _, err := parseSMTPMailbox(text); err != nil {
				return "", err
			}
		}
		return text, nil
	default:
		return "", apperr.Validation("未知配置项: " + def.Key)
	}
}

func hasSMTPControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func validateMergedSMTP(merged map[string]string) error {
	for _, key := range []string{
		"smtp_host", "smtp_port", "smtp_security", "smtp_username",
		"smtp_password", "smtp_from_address", "smtp_from_name",
	} {
		value, ok := merged[key]
		if !ok {
			continue
		}
		def, ok := definitionByKey(key)
		if !ok {
			continue
		}
		if _, err := normalizeSMTPConfigValue(def, value); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) smtpConfig(ctx context.Context) (smtpConfig, error) {
	if s == nil || s.db == nil {
		return smtpConfig{}, apperr.Internal("设置存储不可用")
	}
	rows, err := s.configRows(ctx)
	if err != nil {
		return smtpConfig{}, err
	}
	values := map[string]string{}
	for _, key := range []string{
		"smtp_host", "smtp_port", "smtp_security", "smtp_username",
		"smtp_password", "smtp_from_address", "smtp_from_name",
	} {
		raw := envDefault(s, key)
		if row, ok := rows[key]; ok {
			raw = row.value
		}
		def, ok := definitionByKey(key)
		if !ok {
			return smtpConfig{}, apperr.Internal("SMTP 配置定义缺失")
		}
		value, err := normalizeSMTPConfigValue(def, raw)
		if err != nil {
			return smtpConfig{}, err
		}
		values[key] = value
	}
	port, err := strconv.Atoi(values["smtp_port"])
	if err != nil {
		return smtpConfig{}, apperr.Validation("SMTP 端口必须是 1 到 65535 之间的整数")
	}
	return smtpConfig{
		host:        values["smtp_host"],
		port:        port,
		security:    values["smtp_security"],
		username:    values["smtp_username"],
		password:    values["smtp_password"],
		fromAddress: values["smtp_from_address"],
		fromName:    values["smtp_from_name"],
	}, nil
}

func (cfg smtpConfig) ready() bool {
	if cfg.host == "" || cfg.fromAddress == "" {
		return false
	}
	if cfg.port < 1 || cfg.port > 65535 {
		return false
	}
	if cfg.security != "starttls" && cfg.security != "tls" {
		return false
	}
	return (cfg.username == "") == (cfg.password == "")
}

// RegistrationAvailable reports whether enough valid SMTP configuration exists
// for the registration flow. A partial configuration is a normal false result;
// malformed persisted values are returned as validation errors.
func (s *Store) RegistrationAvailable(ctx context.Context) (bool, error) {
	cfg, err := s.smtpConfig(ctx)
	if err != nil {
		return false, err
	}
	return cfg.ready(), nil
}

// SendVerificationCode sends a registration code over a TLS-protected SMTP
// connection. The caller owns code lifetime and retry semantics.
func (s *Store) SendVerificationCode(ctx context.Context, email, code string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	cfg, err := s.smtpConfig(ctx)
	if err != nil {
		return err
	}
	if !cfg.ready() {
		return apperr.Unavailable("邮件服务尚未配置")
	}
	if _, err := parseSMTPMailbox(email); err != nil {
		return err
	}
	if code == "" || hasSMTPControl(code) {
		return apperr.Validation("验证码格式无效")
	}
	return sendSMTPVerificationCode(ctx, cfg, email, code, nil)
}

// sendSMTPVerificationCode is kept as a narrow sender helper so package tests
// can supply a test CA pool. Production callers pass nil and use system roots.
func sendSMTPVerificationCode(ctx context.Context, cfg smtpConfig, email, code string, roots *x509.CertPool) error {
	message, err := verificationMessage(cfg, email, code)
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	opCtx, cancel := context.WithTimeout(ctx, smtpTransactionTimeout)
	defer cancel()

	dialer := &net.Dialer{Timeout: smtpDialTimeout}
	conn, err := dialer.DialContext(opCtx, "tcp", net.JoinHostPort(cfg.host, strconv.Itoa(cfg.port)))
	if err != nil {
		return smtpTransportError(opCtx)
	}
	stopClose := closeSMTPConnOnCancel(opCtx, conn)
	defer stopClose()
	defer conn.Close()
	if deadline, ok := opCtx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return smtpTransportError(opCtx)
		}
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: cfg.host,
		RootCAs:    roots,
	}
	var smtpConn net.Conn = conn
	if cfg.security == "tls" {
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.HandshakeContext(opCtx); err != nil {
			return smtpTransportError(opCtx)
		}
		smtpConn = tlsConn
	} else if cfg.security != "starttls" {
		return apperr.Validation("SMTP 连接加密方式无效")
	}

	client, err := smtp.NewClient(smtpConn, cfg.host)
	if err != nil {
		return smtpTransportError(opCtx)
	}
	defer client.Close()
	if cfg.security == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return apperr.Unavailable("SMTP 服务不支持安全连接")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return smtpTransportError(opCtx)
		}
	}
	if cfg.username != "" || cfg.password != "" {
		if _, ok := client.TLSConnectionState(); !ok {
			return apperr.Unavailable("SMTP 服务未建立安全连接")
		}
		if err := client.Auth(smtp.PlainAuth("", cfg.username, cfg.password, cfg.host)); err != nil {
			return smtpTransportError(opCtx)
		}
	}
	if err := client.Mail(cfg.fromAddress); err != nil {
		return smtpTransportError(opCtx)
	}
	if err := client.Rcpt(email); err != nil {
		return smtpTransportError(opCtx)
	}
	writer, err := client.Data()
	if err != nil {
		return smtpTransportError(opCtx)
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return smtpTransportError(opCtx)
	}
	if err := writer.Close(); err != nil {
		return smtpTransportError(opCtx)
	}
	// DATA's final response acknowledges the message. A server can close the
	// connection or reject QUIT after accepting the transaction; that does not
	// make the delivered verification code a send failure.
	_ = client.Quit()
	return nil
}

func closeSMTPConnOnCancel(ctx context.Context, conn net.Conn) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

func smtpTransportError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return apperr.Unavailable("验证码邮件发送失败")
}

func parseSMTPMailbox(value string) (*mail.Address, error) {
	if value == "" || strings.TrimSpace(value) != value || hasSMTPControl(value) {
		return nil, apperr.Validation("SMTP 邮箱地址格式无效")
	}
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || address.Name != "" {
		return nil, apperr.Validation("SMTP 邮箱地址格式无效")
	}
	return address, nil
}

func verificationMessage(cfg smtpConfig, email, code string) ([]byte, error) {
	from, err := formatSMTPHeaderAddress(cfg.fromAddress, cfg.fromName)
	if err != nil {
		return nil, err
	}
	to, err := formatSMTPHeaderAddress(email, "")
	if err != nil {
		return nil, err
	}
	if code == "" || hasSMTPControl(code) {
		return nil, apperr.Validation("验证码格式无效")
	}
	subject := mime.QEncoding.Encode("UTF-8", "ProductFlow 邮箱验证码")
	body := "您好，您的 ProductFlow 注册验证码是 " + code + "。\r\n验证码 10 分钟内有效，请勿将验证码告知他人。\r\n"
	message := "From: " + from + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n" +
		"\r\n" + body
	return []byte(message), nil
}

func formatSMTPHeaderAddress(address, name string) (string, error) {
	parsed, err := parseSMTPMailbox(address)
	if err != nil {
		return "", err
	}
	if hasSMTPControl(name) {
		return "", apperr.Validation("SMTP 发件人名称不能包含换行或控制字符")
	}
	return (&mail.Address{Address: parsed.Address, Name: name}).String(), nil
}
