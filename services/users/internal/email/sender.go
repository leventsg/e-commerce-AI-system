package email

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"

	"github.com/leventsg/e-commerce-AI-system/services/users/internal/config"
	"github.com/zeromicro/go-zero/core/logx"
)

type Sender struct {
	conf config.EmailCodeConfig
}

func NewSender(conf config.EmailCodeConfig) *Sender {
	return &Sender{conf: conf}
}

func (s *Sender) SendCode(to, scene, code string) error {
	if s == nil || !s.conf.Enabled {
		logx.Infow("email sender disabled",
			logx.Field("to", maskEmail(to)),
			logx.Field("scene", scene),
			logx.Field("code", code),
		)
		return nil
	}
	if s.conf.SMTPHost == "" || s.conf.SMTPPort == 0 || s.conf.SMTPFrom == "" {
		return fmt.Errorf("smtp config is incomplete")
	}

	from := s.conf.SMTPFrom
	displayName := strings.TrimSpace(s.conf.SMTPDisplayName)
	fromHeader := from
	if displayName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", displayName, from)
	}
	subject := "Your go-mall verification code"
	body := fmt.Sprintf("Your verification code is %s. It will expire soon. If you did not request this, ignore this email.", code)
	msg := strings.Join([]string{
		"From: " + fromHeader,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n")

	addr := net.JoinHostPort(s.conf.SMTPHost, fmt.Sprintf("%d", s.conf.SMTPPort))
	var auth smtp.Auth
	if s.conf.SMTPUsername != "" {
		auth = smtp.PlainAuth("", s.conf.SMTPUsername, s.conf.SMTPPassword, s.conf.SMTPHost)
	}
	if s.conf.SMTPTLS {
		return sendMailTLS(addr, s.conf.SMTPHost, auth, from, []string{to}, []byte(msg))
	}
	return smtp.SendMail(addr, auth, from, []string{to}, []byte(msg))
}

func sendMailTLS(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(msg); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}

func maskEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" {
		return "***"
	}
	name := parts[0]
	if len(name) <= 2 {
		return name[:1] + "***@" + parts[1]
	}
	return name[:2] + "***@" + parts[1]
}
