package mail

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// SMTPSend 通过 SMTP 发送纯文本邮件。
// security 为 starttls 时使用 587 + STARTTLS，其余按 465 隐式 SSL 处理。
func SMTPSend(host string, port int, security, username, password, fromName, to, subject, body string) error {
	if host == "" || port <= 0 || username == "" || password == "" {
		return errors.New("SMTP 未配置完整")
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	msg := buildSMTPMessage(fromName, username, to, subject, body)
	auth := smtp.PlainAuth("", username, password, host)
	dialer := &net.Dialer{Timeout: 15 * time.Second}

	var client *smtp.Client
	if security == "starttls" {
		conn, err := dialer.Dial("tcp", addr)
		if err != nil {
			return err
		}
		c, err := smtp.NewClient(conn, host)
		if err != nil {
			_ = conn.Close()
			return err
		}
		if ok, _ := c.Extension("STARTTLS"); !ok {
			_ = c.Close()
			return errors.New("SMTP 服务器不支持 STARTTLS，请改用 SSL 端口")
		}
		if err := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			_ = c.Close()
			return err
		}
		client = c
	} else {
		conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
			ServerName: host,
			MinVersion: tls.VersionTLS12,
		})
		if err != nil {
			return err
		}
		c, err := smtp.NewClient(conn, host)
		if err != nil {
			_ = conn.Close()
			return err
		}
		client = c
	}
	defer client.Close()

	if err := client.Auth(auth); err != nil {
		return err
	}
	if err := client.Mail(username); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	wc, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := wc.Write(msg); err != nil {
		_ = wc.Close()
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func buildSMTPMessage(fromName, from, to, subject, body string) []byte {
	name := strings.NewReplacer("\r", "", "\n", "").Replace(fromName)
	fromHeader := from
	if name != "" {
		fromHeader = fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("utf-8", name), from)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", fromHeader)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	for len(encoded) > 76 {
		b.WriteString(encoded[:76])
		b.WriteString("\r\n")
		encoded = encoded[76:]
	}
	b.WriteString(encoded)
	b.WriteString("\r\n")
	return []byte(b.String())
}

// IsSMTPDailyLimit 判断是否为 Gmail 当日发信配额耗尽（500 封/天）。
func IsSMTPDailyLimit(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "daily user sending quota") ||
		strings.Contains(s, "5.4.5") ||
		strings.Contains(s, "sending limit") ||
		strings.Contains(s, "quota exceeded")
}
