package notification

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/smtp"
	"net/textproto"
	"net/url"
	"regexp"
	"strings"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// Notification is an alias used by channel adapters.
type Notification = model.Notification

// NotificationChannel dispatches notifications through a concrete medium.
type NotificationChannel interface {
	// Name returns the channel identifier (EMAIL, SMS, etc.)
	Name() string

	// Send dispatches the notification via this channel.
	// Returns error on failure (transient or permanent).
	Send(ctx context.Context, notif Notification) error

	// IsTransientError determines if an error is retryable.
	IsTransientError(err error) bool
}

// EmailChannel sends notifications using SMTP.
type EmailChannel struct {
	smtpHost    string
	smtpPort    int
	fromAddress string
	username    string
	password    string
	logger      *slog.Logger

	timeout  time.Duration
	sendFunc func(ctx context.Context, notif Notification, plainBody, htmlBody string) error
}

func NewEmailChannel(host string, port int, fromAddress, username, password string, logger *slog.Logger) *EmailChannel {
	if logger == nil {
		logger = slog.Default()
	}
	ch := &EmailChannel{
		smtpHost:    host,
		smtpPort:    port,
		fromAddress: fromAddress,
		username:    username,
		password:    password,
		logger:      logger,
		timeout:     10 * time.Second,
	}
	ch.sendFunc = ch.sendSMTP
	return ch
}

func (c *EmailChannel) Name() string {
	return string(model.NotificationChannelEmail)
}

func (c *EmailChannel) Send(ctx context.Context, notif Notification) error {
	if c == nil {
		return newPermanentChannelError("email channel is nil", "EMAIL_CHANNEL_NIL", nil)
	}
	if strings.TrimSpace(notif.Recipient) == "" {
		return newPermanentChannelError("email recipient is required", "EMAIL_RECIPIENT_REQUIRED", nil)
	}
	if strings.TrimSpace(c.fromAddress) == "" {
		return newPermanentChannelError("email from address is required", "EMAIL_FROM_REQUIRED", nil)
	}

	body := ""
	if notif.Body != nil {
		body = *notif.Body
	}
	plain := stripHTML(body)
	if strings.TrimSpace(plain) == "" {
		plain = body
	}

	ctx, cancel := withDefaultTimeout(ctx, c.timeout)
	defer cancel()
	c.logger.Debug("email send request",
		"notification_id", notif.ID,
		"recipient", notif.Recipient,
		"smtp_host", c.smtpHost,
		"smtp_port", c.smtpPort,
	)

	sendFn := c.sendFunc
	if sendFn == nil {
		sendFn = c.sendSMTP
	}
	if err := sendFn(ctx, notif, plain, body); err != nil {
		c.logger.Debug("email send response",
			"notification_id", notif.ID,
			"recipient", notif.Recipient,
			"error", err,
		)
		return err
	}

	c.logger.Debug("email send response",
		"notification_id", notif.ID,
		"recipient", notif.Recipient,
		"result", "success",
	)
	return nil
}

func (c *EmailChannel) IsTransientError(err error) bool {
	return isTransientChannelError(err)
}

func (c *EmailChannel) sendSMTP(ctx context.Context, notif Notification, plainBody, htmlBody string) error {
	addr := fmt.Sprintf("%s:%d", c.smtpHost, c.smtpPort)
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return classifyNetworkError("email dial failed", "EMAIL_DIAL_ERROR", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, c.smtpHost)
	if err != nil {
		return classifyNetworkError("email smtp client init failed", "EMAIL_CLIENT_ERROR", err)
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsConfig := &tls.Config{ServerName: c.smtpHost, MinVersion: tls.VersionTLS12}
		if err := client.StartTLS(tlsConfig); err != nil {
			return classifySMTPError("email starttls failed", "EMAIL_TLS_ERROR", err)
		}
	}

	if strings.TrimSpace(c.username) != "" {
		auth := smtp.PlainAuth("", c.username, c.password, c.smtpHost)
		if err := client.Auth(auth); err != nil {
			return classifySMTPError("email auth failed", "EMAIL_AUTH_ERROR", err)
		}
	}

	if err := client.Mail(c.fromAddress); err != nil {
		return classifySMTPError("email MAIL FROM failed", "EMAIL_MAIL_FROM_ERROR", err)
	}
	if err := client.Rcpt(strings.TrimSpace(notif.Recipient)); err != nil {
		return classifySMTPError("email RCPT TO failed", "EMAIL_RCPT_TO_ERROR", err)
	}

	wc, err := client.Data()
	if err != nil {
		return classifySMTPError("email DATA command failed", "EMAIL_DATA_ERROR", err)
	}
	defer wc.Close()

	mimeMessage, err := buildMIMEMessage(notif, c.fromAddress, plainBody, htmlBody)
	if err != nil {
		return newPermanentChannelError("email message build failed", "EMAIL_BUILD_ERROR", err)
	}
	if _, err := wc.Write(mimeMessage); err != nil {
		return classifyNetworkError("email write failed", "EMAIL_WRITE_ERROR", err)
	}

	if err := wc.Close(); err != nil {
		return classifySMTPError("email data close failed", "EMAIL_DATA_CLOSE_ERROR", err)
	}
	if err := client.Quit(); err != nil {
		return classifySMTPError("email quit failed", "EMAIL_QUIT_ERROR", err)
	}

	return nil
}

func buildMIMEMessage(notif Notification, from, plainBody, htmlBody string) ([]byte, error) {
	boundary := "notif-boundary-" + notif.ID
	buf := &bytes.Buffer{}

	subject := ""
	if notif.Subject != nil {
		subject = *notif.Subject
	}
	if strings.TrimSpace(subject) == "" {
		subject = "Notification"
	}

	msgID := fmt.Sprintf("<%s@workflow-engine>", notif.ID)
	headers := []string{
		fmt.Sprintf("From: %s", from),
		fmt.Sprintf("To: %s", notif.Recipient),
		fmt.Sprintf("Subject: %s", subject),
		"MIME-Version: 1.0",
		fmt.Sprintf("Message-ID: %s", msgID),
		fmt.Sprintf("Date: %s", time.Now().UTC().Format(time.RFC1123Z)),
		fmt.Sprintf("Content-Type: multipart/alternative; boundary=%q", boundary),
	}
	for _, h := range headers {
		if _, err := buf.WriteString(h + "\r\n"); err != nil {
			return nil, err
		}
	}
	if _, err := buf.WriteString("\r\n"); err != nil {
		return nil, err
	}

	mw := multipart.NewWriter(buf)
	if err := mw.SetBoundary(boundary); err != nil {
		return nil, err
	}

	plainHeader := textproto.MIMEHeader{}
	plainHeader.Set("Content-Type", "text/plain; charset=UTF-8")
	plainHeader.Set("Content-Transfer-Encoding", "quoted-printable")
	plainPart, err := mw.CreatePart(plainHeader)
	if err != nil {
		return nil, err
	}
	qpPlain := quotedprintable.NewWriter(plainPart)
	if _, err := io.WriteString(qpPlain, plainBody); err != nil {
		return nil, err
	}
	if err := qpPlain.Close(); err != nil {
		return nil, err
	}

	htmlHeader := textproto.MIMEHeader{}
	htmlHeader.Set("Content-Type", "text/html; charset=UTF-8")
	htmlHeader.Set("Content-Transfer-Encoding", "quoted-printable")
	htmlPart, err := mw.CreatePart(htmlHeader)
	if err != nil {
		return nil, err
	}
	qpHTML := quotedprintable.NewWriter(htmlPart)
	if _, err := io.WriteString(qpHTML, htmlBody); err != nil {
		return nil, err
	}
	if err := qpHTML.Close(); err != nil {
		return nil, err
	}

	if err := mw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var stripTagsPattern = regexp.MustCompile("<[^>]*>")

func stripHTML(input string) string {
	stripped := stripTagsPattern.ReplaceAllString(input, "")
	return strings.TrimSpace(stripped)
}

// SMSChannel sends notifications using an HTTP SMS gateway API.
type SMSChannel struct {
	accountSID string
	authToken  string
	fromNumber string
	apiClient  *http.Client
	logger     *slog.Logger

	baseURL string
	timeout time.Duration
}

func NewSMSChannel(accountSID, authToken, fromNumber string, apiClient *http.Client, logger *slog.Logger) *SMSChannel {
	if logger == nil {
		logger = slog.Default()
	}
	if apiClient == nil {
		apiClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &SMSChannel{
		accountSID: accountSID,
		authToken:  authToken,
		fromNumber: fromNumber,
		apiClient:  apiClient,
		logger:     logger,
		timeout:    10 * time.Second,
	}
}

func (c *SMSChannel) Name() string {
	return string(model.NotificationChannelSMS)
}

func (c *SMSChannel) Send(ctx context.Context, notif Notification) error {
	if c == nil {
		return newPermanentChannelError("sms channel is nil", "SMS_CHANNEL_NIL", nil)
	}
	if strings.TrimSpace(notif.Recipient) == "" {
		return newPermanentChannelError("sms recipient is required", "SMS_RECIPIENT_REQUIRED", nil)
	}
	if strings.TrimSpace(c.fromNumber) == "" {
		return newPermanentChannelError("sms from number is required", "SMS_FROM_REQUIRED", nil)
	}

	message := ""
	if notif.Body != nil {
		message = *notif.Body
	}
	if strings.TrimSpace(message) == "" && notif.Subject != nil {
		message = *notif.Subject
	}
	message = truncate(message, 160)

	endpoint := c.baseURL
	if strings.TrimSpace(endpoint) == "" {
		endpoint = fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", c.accountSID)
	}

	ctx, cancel := withDefaultTimeout(ctx, c.timeout)
	defer cancel()
	values := url.Values{}
	values.Set("To", strings.TrimSpace(notif.Recipient))
	values.Set("From", strings.TrimSpace(c.fromNumber))
	values.Set("Body", message)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return newPermanentChannelError("sms request build failed", "SMS_REQUEST_BUILD_ERROR", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Idempotency-Key", notif.ID)
	if c.accountSID != "" {
		req.SetBasicAuth(c.accountSID, c.authToken)
	}

	c.logger.Debug("sms send request",
		"notification_id", notif.ID,
		"recipient", notif.Recipient,
		"endpoint", endpoint,
	)

	resp, err := c.apiClient.Do(req)
	if err != nil {
		return classifyNetworkError("sms api call failed", "SMS_HTTP_ERROR", err)
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return classifyNetworkError("sms response read failed", "SMS_RESPONSE_READ_ERROR", readErr)
	}

	c.logger.Debug("sms send response",
		"notification_id", notif.ID,
		"status_code", resp.StatusCode,
		"response", string(respBody),
	)

	if resp.StatusCode >= 500 {
		return newTransientChannelError("sms gateway server error", fmt.Sprintf("SMS_%d", resp.StatusCode), nil)
	}
	if resp.StatusCode >= 400 {
		return newPermanentChannelError("sms gateway client error", fmt.Sprintf("SMS_%d", resp.StatusCode), nil)
	}

	return nil
}

func (c *SMSChannel) IsTransientError(err error) bool {
	return isTransientChannelError(err)
}

// InAppChannel stores notifications in user_notifications for UI retrieval.
type InAppChannel struct {
	db     *sqlx.DB
	logger *slog.Logger
}

func NewInAppChannel(db *sqlx.DB, logger *slog.Logger) *InAppChannel {
	if logger == nil {
		logger = slog.Default()
	}
	return &InAppChannel{db: db, logger: logger}
}

func (c *InAppChannel) Name() string {
	return string(model.NotificationChannelInApp)
}

func (c *InAppChannel) Send(ctx context.Context, notif Notification) error {
	if c == nil {
		return newPermanentChannelError("in-app channel is nil", "IN_APP_CHANNEL_NIL", nil)
	}
	if c.db == nil {
		return newPermanentChannelError("in-app db is nil", "IN_APP_DB_NIL", nil)
	}
	if strings.TrimSpace(notif.Recipient) == "" {
		return newPermanentChannelError("in-app recipient is required", "IN_APP_RECIPIENT_REQUIRED", nil)
	}

	tx, err := c.db.BeginTxx(ctx, nil)
	if err != nil {
		return newTransientChannelError("in-app begin tx failed", "IN_APP_TX_BEGIN_ERROR", err)
	}
	defer tx.Rollback()

	if err := c.SendInTx(ctx, tx, notif); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return newTransientChannelError("in-app commit failed", "IN_APP_TX_COMMIT_ERROR", err)
	}
	return nil
}

func (c *InAppChannel) SendInTx(ctx context.Context, tx *sqlx.Tx, notif Notification) error {
	if tx == nil {
		return newPermanentChannelError("in-app tx is nil", "IN_APP_TX_NIL", nil)
	}
	if strings.TrimSpace(notif.Recipient) == "" {
		return newPermanentChannelError("in-app recipient is required", "IN_APP_RECIPIENT_REQUIRED", nil)
	}

	title := ""
	if notif.Subject != nil {
		title = *notif.Subject
	}
	message := ""
	if notif.Body != nil {
		message = *notif.Body
	}
	if strings.TrimSpace(message) == "" {
		message = title
	}

	payload, err := json.Marshal(map[string]interface{}{
		"notification_id": notif.ID,
		"trigger_code":    notif.TriggerCode,
		"case_id":         notif.CaseID,
		"task_id":         notif.TaskID,
	})
	if err != nil {
		return newPermanentChannelError("in-app payload marshal failed", "IN_APP_PAYLOAD_MARSHAL_ERROR", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO user_notifications (
			notification_id,
			user_id,
			channel,
			title,
			message,
			payload,
			is_read,
			created_at,
			updated_at
		)
		VALUES (
			$1::uuid,
			$2,
			'IN_APP',
			$3,
			$4,
			$5::jsonb,
			FALSE,
			now(),
			now()
		)
		ON CONFLICT (notification_id) DO NOTHING
	`, notif.ID, notif.Recipient, nullIfBlank(title), message, payload)
	if err != nil {
		return newTransientChannelError("in-app insert failed", "IN_APP_INSERT_ERROR", err)
	}

	c.logger.Debug("in-app send response",
		"notification_id", notif.ID,
		"recipient", notif.Recipient,
		"result", "inserted",
	)
	return nil
}

func (c *InAppChannel) IsTransientError(err error) bool {
	return isTransientChannelError(err)
}

func nullIfBlank(value string) interface{} {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func withDefaultTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func classifyNetworkError(message, code string, cause error) error {
	if cause == nil {
		return newTransientChannelError(message, code, nil)
	}
	if ne, ok := cause.(net.Error); ok {
		if ne.Timeout() || ne.Temporary() {
			return newTransientChannelError(message, code, cause)
		}
	}
	msg := strings.ToLower(cause.Error())
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "connection refused") || strings.Contains(msg, "eof") {
		return newTransientChannelError(message, code, cause)
	}
	return newPermanentChannelError(message, code, cause)
}

func classifySMTPError(message, code string, cause error) error {
	if cause == nil {
		return newPermanentChannelError(message, code, nil)
	}
	msg := strings.ToLower(cause.Error())
	if strings.Contains(msg, " 4") || strings.Contains(msg, "421") || strings.Contains(msg, "450") || strings.Contains(msg, "451") || strings.Contains(msg, "452") {
		return newTransientChannelError(message, code, cause)
	}
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "tempor") {
		return newTransientChannelError(message, code, cause)
	}
	if strings.Contains(msg, " 5") || strings.Contains(msg, "550") || strings.Contains(msg, "551") || strings.Contains(msg, "553") {
		return newPermanentChannelError(message, code, cause)
	}
	return newPermanentChannelError(message, code, cause)
}
