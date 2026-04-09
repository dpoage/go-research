package experiment

import (
	"math"
	"testing"
)

func TestLastNumberExtractor_Extract(t *testing.T) {
	e := &LastNumberExtractor{}

	tests := []struct {
		name    string
		output  string
		want    float64
		wantErr bool
	}{
		{
			name:   "single integer",
			output: "score 100",
			want:   100,
		},
		{
			name:   "single decimal",
			output: "loss: 0.42",
			want:   0.42,
		},
		{
			name:   "multiple numbers picks last",
			output: "training done\nfinal loss: 0.42",
			want:   0.42,
		},
		{
			name:   "scientific notation",
			output: "result: 1.23e-4",
			want:   1.23e-4,
		},
		{
			name:   "scientific notation uppercase E",
			output: "result: 5.67E+3",
			want:   5.67e+3,
		},
		{
			name:   "multiple numbers in line",
			output: "epoch 10 loss 0.5 acc 0.95",
			want:   0.95,
		},
		{
			name:   "negative number",
			output: "delta: -3.14",
			want:   -3.14,
		},
		{
			name:   "leading decimal point",
			output: "value .75",
			want:   0.75,
		},
		{
			name:    "no numbers",
			output:  "no metrics here",
			wantErr: true,
		},
		{
			name:    "empty string",
			output:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.Extract(tt.output)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if math.Abs(got-tt.want) > 1e-12 {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
