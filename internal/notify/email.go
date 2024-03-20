// Copyright 2020 The Moov Authors
// Use of this source code is governed by an Apache License
// license that can be found in the LICENSE file.

package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/moov-io/ach"
	"github.com/moov-io/achgateway/internal/service"
	"github.com/moov-io/base/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	gomail "github.com/ory/mail/v3"
)

type Email struct {
	cfg    *service.Email
	dialer *gomail.Dialer
}

type EmailTemplateData struct {
	CompanyName string // e.g. Moov
	Verb        string // e.g. upload, download
	Filename    string // e.g. 20200529-131400.ach
	Hostname    string

	Successful bool

	DebitTotal  string
	CreditTotal string

	BatchCount int
	EntryCount int
}

var (
	// Ensure the default template validates against our data struct
	_ = service.DefaultEmailTemplate.Execute(io.Discard, EmailTemplateData{})
)

func NewEmail(cfg *service.Email) (*Email, error) {
	dialer, err := setupGoMailClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Email{
		cfg:    cfg,
		dialer: dialer,
	}, nil
}

func setupGoMailClient(cfg *service.Email) (*gomail.Dialer, error) {
	uri, err := url.Parse(cfg.ConnectionURI)
	if err != nil {
		return nil, err
	}
	password, _ := uri.User.Password()
	port, _ := strconv.ParseInt(uri.Port(), 10, 64)
	if port > 65535 {
		return nil, fmt.Errorf("invalid port: %d", port)
	}

	host, _, _ := net.SplitHostPort(uri.Host)
	tlsConfig := &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	}

	skipVerify, _ := strconv.ParseBool(uri.Query().Get("insecure_skip_verify"))
	tlsConfig.InsecureSkipVerify = skipVerify

	ssl := strings.EqualFold(uri.Scheme, "smtps")
	if strings.Contains(host, ".gmail.com") {
		// GMail explicitly disables SSL, but our mailslurp image requires it.
		ssl = false
	}

	return &gomail.Dialer{
		TLSConfig:    tlsConfig,
		SSL:          ssl,
		Host:         uri.Hostname(),
		Port:         int(port),
		Username:     uri.User.Username(),
		Password:     password,
		Timeout:      time.Second * 10,
		RetryFailure: true,
	}, nil
}

func (mailer *Email) Info(ctx context.Context, msg *Message) error {
	ctx, span := telemetry.StartSpan(ctx, "notify-email-info", trace.WithAttributes(
		attribute.String("achgateway.filename", msg.Filename),
	))
	defer span.End()

	successful := true
	subject := marshalSubject(mailer.cfg, msg, msg.Filename, successful)
	contents, err := marshalEmail(mailer.cfg, msg, successful)
	if err != nil {
		return err
	}

	return sendEmail(ctx, mailer.cfg, mailer.dialer, subject, contents)
}

func (mailer *Email) Critical(ctx context.Context, msg *Message) error {
	ctx, span := telemetry.StartSpan(ctx, "notify-email-critical", trace.WithAttributes(
		attribute.String("achgateway.filename", msg.Filename),
	))
	defer span.End()

	successful := false
	subject := marshalSubject(mailer.cfg, msg, msg.Filename, successful)
	contents, err := marshalEmail(mailer.cfg, msg, successful)
	if err != nil {
		return err
	}

	return sendEmail(ctx, mailer.cfg, mailer.dialer, subject, contents)
}

func marshalSubject(cfg *service.Email, msg *Message, filename string, successful bool) string {
	if successful {
		return fmt.Sprintf("%s %sed by %s", filename, string(msg.Direction), cfg.CompanyName)
	}
	return fmt.Sprintf("%s FAILED %s by %s", filename, string(msg.Direction), cfg.CompanyName)
}

func marshalEmail(cfg *service.Email, msg *Message, successful bool) (string, error) {
	if msg.Contents != "" {
		return msg.Contents, nil
	}

	data := EmailTemplateData{
		CompanyName: cfg.CompanyName,
		Verb:        string(msg.Direction),
		Filename:    msg.Filename,
		Hostname:    msg.Hostname,
		Successful:  successful,
	}
	if msg.File != nil {
		data.BatchCount = msg.File.Control.BatchCount
		data.EntryCount = countEntries(msg.File)

		data.DebitTotal = convertDollar(msg.File.Control.TotalDebitEntryDollarAmountInFile)
		data.CreditTotal = convertDollar(msg.File.Control.TotalCreditEntryDollarAmountInFile)
	}

	var buf bytes.Buffer
	if err := cfg.Tmpl().Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func sendEmail(ctx context.Context, cfg *service.Email, dialer *gomail.Dialer, subject, body string) error {
	span := telemetry.SpanFromContext(ctx)

	m := gomail.NewMessage()
	m.SetHeader("From", cfg.From)
	m.SetHeader("To", cfg.To...)
	m.SetHeader("Subject", subject)
	m.SetBody("text/plain", body)

	maxRetries := 3
	if cfg.MaxRetries > 0 {
		maxRetries = cfg.MaxRetries
	}

	var outErr error
	for tries := 1; tries <= maxRetries; tries++ {
		if span != nil {
			span.AddEvent(fmt.Sprintf("attempt-%d", tries))
		}

		outErr = dialer.DialAndSend(context.Background(), m)
		if outErr != nil {
			if strings.Contains(outErr.Error(), "i/o timeout") {
				time.Sleep(250 * time.Millisecond)
				continue
			}
			return outErr
		} else {
			return nil
		}
	}
	return outErr
}

func countEntries(file *ach.File) int {
	var total int
	if file == nil {
		return total
	}
	for i := range file.Batches {
		total += len(file.Batches[i].GetEntries())
	}
	return total
}
