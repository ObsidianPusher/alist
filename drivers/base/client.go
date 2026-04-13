package base

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
)

var (
	NoRedirectClient *resty.Client
	RestyClient      *resty.Client
	HttpClient       *http.Client
)
var UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.88 Safari/537.36"
var DefaultTimeout = time.Second * 30

type filteredRestyLogger struct{}

func shouldSuppressRestyLog(format string, v ...interface{}) bool {
	msg := strings.ToLower(fmt.Sprintf(format, v...))
	return strings.Contains(msg, "context canceled") ||
		strings.Contains(msg, "context deadline exceeded")
}

func (l *filteredRestyLogger) Errorf(format string, v ...interface{}) {
	if shouldSuppressRestyLog(format, v...) {
		return
	}
	log.Errorf("RESTY "+format, v...)
}

func (l *filteredRestyLogger) Warnf(format string, v ...interface{}) {
	if shouldSuppressRestyLog(format, v...) {
		return
	}
	log.Warnf("RESTY "+format, v...)
}

func (l *filteredRestyLogger) Debugf(format string, v ...interface{}) {
	log.Debugf("RESTY "+format, v...)
}

func InitClient() {
	NoRedirectClient = resty.New().SetRedirectPolicy(
		resty.RedirectPolicyFunc(func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}),
	).SetTLSClientConfig(&tls.Config{InsecureSkipVerify: conf.Conf.TlsInsecureSkipVerify})
	NoRedirectClient.SetHeader("user-agent", UserAgent)

	RestyClient = NewRestyClient()
	HttpClient = NewHttpClient()
}

func NewRestyClient() *resty.Client {
	client := resty.New().
		SetHeader("user-agent", UserAgent).
		SetRetryCount(3).
		SetRetryResetReaders(true).
		SetTimeout(DefaultTimeout).
		SetTLSClientConfig(&tls.Config{InsecureSkipVerify: conf.Conf.TlsInsecureSkipVerify})
	client.SetLogger(&filteredRestyLogger{})
	return client
}

func NewHttpClient() *http.Client {
	return &http.Client{
		Timeout: time.Hour * 48,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:   true,
			MaxIdleConns:        256,
			MaxIdleConnsPerHost: 64,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
			// Keep Expect-Continue short to avoid blocking uploads/downloads on slow upstreams.
			ExpectContinueTimeout: time.Second,
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: conf.Conf.TlsInsecureSkipVerify},
		},
	}
}
