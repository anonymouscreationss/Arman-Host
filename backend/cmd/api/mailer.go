package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"os"
)

type mailer interface {
	Send(ctx context.Context, recipient, subject, body string) error
	VerificationURL(token string) (string, error)
	PasswordResetURL(token string) (string, error)
}

type smtpMailer struct {
	host      string
	port      string
	username  string
	password  string
	from      string
	publicURL string
}

func newConfiguredMailer() mailer {
	host, from, publicURL := os.Getenv("SMTP_HOST"), os.Getenv("SMTP_FROM"), os.Getenv("APP_PUBLIC_URL")
	if host == "" || from == "" || publicURL == "" {
		return nil
	}
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "587"
	}
	return &smtpMailer{
		host: host, port: port, username: os.Getenv("SMTP_USERNAME"),
		password: os.Getenv("SMTP_PASSWORD"), from: from, publicURL: publicURL,
	}
}

func (m *smtpMailer) Send(ctx context.Context, recipient, subject, body string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	address := net.JoinHostPort(m.host, m.port)
	headers := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n", m.from, recipient, subject)
	var auth smtp.Auth
	if m.username != "" {
		auth = smtp.PlainAuth("", m.username, m.password, m.host)
	}
	if err := smtp.SendMail(address, auth, m.from, []string{recipient}, []byte(headers+body)); err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	return nil
}

func (m *smtpMailer) VerificationURL(token string) (string, error) {
	return appendToken(m.publicURL, "/verify-email", token)
}

func (m *smtpMailer) PasswordResetURL(token string) (string, error) {
	return appendToken(m.publicURL, "/reset-password", token)
}

func appendToken(base, path, token string) (string, error) {
	if base == "" || token == "" {
		return "", errors.New("email link configuration is incomplete")
	}
	return fmt.Sprintf("%s%s?token=%s", base, path, token), nil
}
