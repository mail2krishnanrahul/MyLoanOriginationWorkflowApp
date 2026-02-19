package notification

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"workflow-engine/pkg/model"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestSMSChannelSend(t *testing.T) {
	notif := model.Notification{
		ID:        "11111111-1111-1111-1111-111111111111",
		Recipient: "+12025550123",
		Body:      strPtr("hello from sms"),
	}

	tests := []struct {
		name          string
		status        int
		wantErr       bool
		wantTransient bool
	}{
		{
			name:   "happy path",
			status: http.StatusCreated,
		},
		{
			name:   "edge case long body truncated",
			status: http.StatusCreated,
		},
		{
			name:          "failure mode 4xx permanent",
			status:        http.StatusBadRequest,
			wantErr:       true,
			wantTransient: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body, _ := io.ReadAll(req.Body)
				if tt.name == "edge case long body truncated" {
					assert.LessOrEqual(t, len(string(body)), 1024)
				}
				return &http.Response{
					StatusCode: tt.status,
					Body:       io.NopCloser(strings.NewReader(`{"sid":"abc"}`)),
					Header:     make(http.Header),
				}, nil
			})}

			channel := NewSMSChannel("sid", "token", "+1555000", client, nil)
			channel.baseURL = "https://example.test/messages"
			if tt.name == "edge case long body truncated" {
				long := strings.Repeat("a", 300)
				notif.Body = &long
			}

			err := channel.Send(context.Background(), notif)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, tt.wantTransient, channel.IsTransientError(err))
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestEmailChannelSend(t *testing.T) {
	notif := model.Notification{
		ID:        "22222222-2222-2222-2222-222222222222",
		Recipient: "borrower@example.com",
		Subject:   strPtr("Loan Update"),
		Body:      strPtr("<p>Your loan is approved.</p>"),
	}

	tests := []struct {
		name          string
		sendErr       error
		wantErr       bool
		wantTransient bool
	}{
		{
			name: "happy path",
		},
		{
			name:          "edge case smtp timeout transient",
			sendErr:       timeoutError{},
			wantErr:       true,
			wantTransient: true,
		},
		{
			name:          "failure mode smtp permanent",
			sendErr:       errors.New("550 mailbox unavailable"),
			wantErr:       true,
			wantTransient: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := NewEmailChannel("smtp.example.com", 587, "noreply@example.com", "user", "pass", nil)
			channel.sendFunc = func(ctx context.Context, notif Notification, plainBody, htmlBody string) error {
				if tt.sendErr == nil {
					return nil
				}
				if _, ok := tt.sendErr.(timeoutError); ok {
					return classifyNetworkError("timeout", "EMAIL_TIMEOUT", tt.sendErr)
				}
				return classifySMTPError("smtp", "EMAIL_SMTP", tt.sendErr)
			}

			err := channel.Send(context.Background(), notif)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, tt.wantTransient, channel.IsTransientError(err))
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestInAppChannelSend(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(sqlmock.Sqlmock)
		wantErr bool
	}{
		{
			name: "happy path",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectExec(`(?s)INSERT INTO user_notifications`).
					WithArgs(
						"33333333-3333-3333-3333-333333333333",
						"user-1",
						"Subject",
						"Message",
						sqlmock.AnyArg(),
					).
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			},
		},
		{
			name: "failure mode insert error",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectExec(`(?s)INSERT INTO user_notifications`).WillReturnError(errors.New("insert failed"))
				mock.ExpectRollback()
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			assert.NoError(t, err)
			db := sqlx.NewDb(sqlDB, "sqlmock")
			defer db.Close()

			tt.setup(mock)

			channel := NewInAppChannel(db, nil)
			notif := model.Notification{
				ID:        "33333333-3333-3333-3333-333333333333",
				Recipient: "user-1",
				Subject:   strPtr("Subject"),
				Body:      strPtr("Message"),
			}

			err = channel.Send(context.Background(), notif)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type timeoutError struct{}

func (e timeoutError) Error() string   { return "timeout" }
func (e timeoutError) Timeout() bool   { return true }
func (e timeoutError) Temporary() bool { return true }

func strPtr(v string) *string {
	return &v
}
