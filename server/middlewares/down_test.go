package middlewares

import "testing"

func TestParsePath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty path fallback to root",
			in:   "",
			want: "/",
		},
		{
			name: "plain path",
			in:   "/foo/bar",
			want: "/foo/bar",
		},
		{
			name: "decode encoded characters",
			in:   "/foo%20bar/%23file",
			want: "/foo bar/#file",
		},
		{
			name: "decode encoded slash",
			in:   "/foo%2Fbar",
			want: "/foo/bar",
		},
		{
			name: "invalid escape keeps raw content",
			in:   "/foo%ZZbar",
			want: "/foo%ZZbar",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parsePath(tc.in)
			if got != tc.want {
				t.Fatalf("parsePath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
