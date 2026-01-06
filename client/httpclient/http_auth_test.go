package httpclient

import "testing"

func Test_normalizeAuthType(t *testing.T) {
	type tc struct {
		in   string
		want string
	}

	cases := []tc{
		{in: "bearer", want: "Bearer"},
		{in: "Bearer", want: "Bearer"},
		{in: " basic ", want: "Basic"},
		{in: "BASIC", want: "Basic"},
		{in: "", want: "Bearer"},
		{in: "Token", want: "Token"},
	}

	for _, c := range cases {
		got := normalizeAuthType(c.in)
		if got != c.want {
			t.Fatalf("normalizeAuthType(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}
