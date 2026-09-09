package utils

import "testing"

func TestParsePercentage_Golden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    float64
		wantErr bool
	}{
		{name: "plain number", in: "12.5", want: 12.5},
		{name: "with percent", in: "12.5%", want: 12.5},
		{name: "spaces", in: "  99% ", want: 99},
		{name: "invalid", in: "x%", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParsePercentage(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("got=%v want %v", got, tt.want)
			}
		})
	}
}
