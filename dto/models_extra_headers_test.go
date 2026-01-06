package dto

import "testing"

func TestExtraHeaders_SetAndString_Golden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want map[string]string
	}{
		{
			name: "single header",
			in:   "A=1",
			want: map[string]string{"A": "1"},
		},
		{
			name: "multiple headers",
			in:   "A=1,B=two",
			want: map[string]string{"A": "1", "B": "two"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			eh := make(ExtraHeaders)
			if err := eh.Set(tt.in); err != nil {
				t.Fatalf("Set err: %v", err)
			}
			for k, v := range tt.want {
				if eh[k] != v {
					t.Fatalf("eh[%q]=%q want %q", k, eh[k], v)
				}
			}
			// String() should be valid JSON
			if s := eh.String(); len(s) == 0 || s[0] != '{' {
				t.Fatalf("String()=%q not json object", s)
			}
		})
	}
}
