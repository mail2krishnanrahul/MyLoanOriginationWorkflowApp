package notification

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTemplateRendererRender(t *testing.T) {
	renderer := NewTemplateRenderer()

	tests := []struct {
		name       string
		template   string
		ctx        map[string]interface{}
		want       string
		wantErr    bool
		htmlRender bool
	}{
		{
			name:     "simple variable substitution",
			template: "Hello {{.borrower_name}}",
			ctx: map[string]interface{}{
				"borrower_name": "Rahul",
			},
			want: "Hello Rahul",
		},
		{
			name:     "conditional block",
			template: "{{if .approved}}Approved{{else}}Denied{{end}}",
			ctx: map[string]interface{}{
				"approved": true,
			},
			want: "Approved",
		},
		{
			name:     "loop block",
			template: "{{range .documents}}{{.name}} {{end}}",
			ctx: map[string]interface{}{
				"documents": []map[string]interface{}{
					{"name": "ID"},
					{"name": "Income"},
				},
			},
			want: "ID Income ",
		},
		{
			name:     "custom function formatCurrency",
			template: "{{formatCurrency .amount}}",
			ctx: map[string]interface{}{
				"amount": 15234.5,
			},
			want: "$15,234.50",
		},
		{
			name:     "custom function formatDate and toUpper",
			template: "{{toUpper .status}} {{formatDate .created_at \"2006-01-02\"}}",
			ctx: map[string]interface{}{
				"status":     "approved",
				"created_at": "2026-02-19T12:00:00Z",
			},
			want: "APPROVED 2026-02-19",
		},
		{
			name:     "custom function truncate",
			template: "{{truncate .message 5}}",
			ctx: map[string]interface{}{
				"message": "notification",
			},
			want: "notif",
		},
		{
			name:       "html escaping for email body",
			template:   "<p>{{.unsafe}}</p>",
			htmlRender: true,
			ctx: map[string]interface{}{
				"unsafe": "<script>alert(1)</script>",
			},
			want: "<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>",
		},
		{
			name:     "missing variable",
			template: "Hello {{.name}}",
			ctx:      map[string]interface{}{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				got string
				err error
			)
			if tt.htmlRender {
				got, err = renderer.renderHTML(context.Background(), tt.template, tt.ctx)
			} else {
				got, err = renderer.Render(context.Background(), tt.template, tt.ctx)
			}
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTemplateRendererValidateTemplate(t *testing.T) {
	renderer := NewTemplateRenderer()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:  "valid syntax",
			input: "{{if .approved}}Approved{{else}}Denied{{end}}",
		},
		{
			name:    "invalid syntax",
			input:   "{{if .approved}}Approved",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := renderer.ValidateTemplate(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
