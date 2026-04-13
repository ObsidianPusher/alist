package cmd

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
)

func TestIsIgnorableServerErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: true},
		{name: "http server closed", err: http.ErrServerClosed, want: true},
		{name: "net closed", err: net.ErrClosed, want: true},
		{name: "ctx canceled", err: context.Canceled, want: true},
		{name: "ctx deadline", err: context.DeadlineExceeded, want: true},
		{name: "wrapped net closed", err: errors.Join(errors.New("wrapped"), net.ErrClosed), want: true},
		{name: "closed conn text", err: errors.New("accept tcp: use of closed network connection"), want: true},
		{name: "other", err: errors.New("boom"), want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isIgnorableServerErr(tc.err); got != tc.want {
				t.Fatalf("isIgnorableServerErr(%v)=%v, want=%v", tc.err, got, tc.want)
			}
		})
	}
}
