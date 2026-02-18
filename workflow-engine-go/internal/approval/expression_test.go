package approval

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExpressionEvaluatorEvaluate(t *testing.T) {
	eval := &ExpressionEvaluator{}

	tests := []struct {
		name    string
		expr    string
		ctx     map[string]interface{}
		want    bool
		wantErr bool
	}{
		{
			name: "happy path numeric comparison",
			expr: "amount >= 100000",
			ctx: map[string]interface{}{
				"amount": 150000,
			},
			want: true,
		},
		{
			name: "string equality",
			expr: "risk_rating == 'HIGH'",
			ctx: map[string]interface{}{
				"risk_rating": "HIGH",
			},
			want: true,
		},
		{
			name: "boolean and or",
			expr: "amount >= 100000 && (risk == 'HIGH' || score > 700)",
			ctx: map[string]interface{}{
				"amount": 120000,
				"risk":   "LOW",
				"score":  750,
			},
			want: true,
		},
		{
			name: "nested field access",
			expr: "borrower.credit_score > 700",
			ctx: map[string]interface{}{
				"borrower": map[string]interface{}{
					"credit_score": 735,
				},
			},
			want: true,
		},
		{
			name: "failure mode invalid syntax",
			expr: "amount >=",
			ctx: map[string]interface{}{
				"amount": 1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := eval.Evaluate(context.Background(), tt.expr, tt.ctx)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
