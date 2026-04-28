package smtp

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/mail"
	"net/smtp"
	"strings"
)

type (
	Service interface {
		Send(ctx context.Context, req SendRequest) error
	}

	Config struct {
		Enabled  bool   `yaml:"enabled"`
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		Username string `yaml:"username"`
		Password string `yaml:"password"`
		From     string `yaml:"from"`
	}

	SendRequest struct {
		To      []string
		Subject string
		Body    string
	}

	service struct {
		cfg Config
	}
)

func New(cfg Config) Service {
	return &service{cfg: cfg}
}

func (s *service) Send(ctx context.Context, req SendRequest) error {
	if !s.cfg.Enabled {
		return fmt.Errorf("smtp service is disabled")
	}
	if s.cfg.Host == "" {
		return fmt.Errorf("smtp host is not configured")
	}

	to := strings.Join(req.To, ", ")
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		s.cfg.From, to, req.Subject, req.Body)

	auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)

	fromAddr := s.cfg.From
	if parsed, err := mail.ParseAddress(fromAddr); err == nil {
		fromAddr = parsed.Address
	}

	var toAddrs []string
	for _, t := range req.To {
		if parsed, err := mail.ParseAddress(t); err == nil {
			toAddrs = append(toAddrs, parsed.Address)
		} else {
			toAddrs = append(toAddrs, t)
		}
	}

	// Custom dialer to handle TLS if port 465
	if s.cfg.Port == 465 {
		return s.sendWithTLS(ctx, addr, auth, fromAddr, toAddrs, []byte(msg))
	}

	return smtp.SendMail(addr, auth, fromAddr, toAddrs, []byte(msg))
}

func (s *service) sendWithTLS(ctx context.Context, addr string, auth smtp.Auth, fromAddr string, to []string, msg []byte) error {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: false,
		ServerName:         s.cfg.Host,
	}

	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return err
	}
	defer client.Quit()

	if err = client.Auth(auth); err != nil {
		return err
	}

	if err = client.Mail(fromAddr); err != nil {
		return err
	}

	for _, addr := range to {
		if err = client.Rcpt(addr); err != nil {
			return err
		}
	}

	w, err := client.Data()
	if err != nil {
		return err
	}

	_, err = w.Write(msg)
	if err != nil {
		return err
	}

	err = w.Close()
	if err != nil {
		return err
	}

	return client.Quit()
}
