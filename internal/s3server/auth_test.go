package s3server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/db"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	_ "github.com/alist-org/alist/v3/drivers/local"
	"github.com/aws/aws-sdk-go/aws/credentials"
	awsv4 "github.com/aws/aws-sdk-go/aws/signer/v4"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func init() {
	dB, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	conf.Conf = conf.DefaultConfig()
	conf.Conf.Database.Type = "sqlite3"
	db.Init(dB)
}

func TestParseCredentialScopeKeepsPlus(t *testing.T) {
	scope, err := parseCredentialScope("LxhRnZpxmP+zoSVTZToF/20260526/us-east-1/s3/aws4_request")
	if err != nil {
		t.Fatalf("parseCredentialScope error: %v", err)
	}
	if scope.AccessKeyID != "LxhRnZpxmP+zoSVTZToF" {
		t.Fatalf("unexpected access key id: %q", scope.AccessKeyID)
	}
}

func TestVerifyRequestSignatureAcceptsHeaderSignedRequest(t *testing.T) {
	cfg := &Config{
		AccessKeyID:     "LxhRnZpxmP+zoSVTZToF",
		SecretAccessKey: "V3PtKffK7Uk2HeT5hjf9EcjntpQEyuetS+54nYIs",
		Region:          "us-east-1",
	}

	req, err := http.NewRequest(http.MethodGet, "http://example.com/b/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = "example.com"
	signer := awsv4.NewSigner(credentials.NewStaticCredentials(cfg.AccessKeyID, cfg.SecretAccessKey, ""))
	signer.DisableURIPathEscaping = true
	signedTime := time.Now().UTC()
	if _, err := signer.Sign(req, nil, "s3", cfg.Region, signedTime); err != nil {
		t.Fatalf("sign request: %v", err)
	}

	if err := verifyRequestSignature(req, nil, cfg); err != nil {
		t.Fatalf("verifyRequestSignature error: %v", err)
	}
}

func TestVerifyRequestSignatureAcceptsPresignedRequest(t *testing.T) {
	cfg := &Config{
		AccessKeyID:     "LxhRnZpxmP+zoSVTZToF",
		SecretAccessKey: "V3PtKffK7Uk2HeT5hjf9EcjntpQEyuetS+54nYIs",
		Region:          "us-east-1",
	}

	req, err := http.NewRequest(http.MethodGet, "http://example.com/b/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = "example.com"
	signer := awsv4.NewSigner(credentials.NewStaticCredentials(cfg.AccessKeyID, cfg.SecretAccessKey, ""))
	signer.DisableURIPathEscaping = true
	signedTime := time.Now().UTC()
	if _, err := signer.Presign(req, nil, "s3", cfg.Region, time.Minute, signedTime); err != nil {
		t.Fatalf("presign request: %v", err)
	}

	if err := verifyRequestSignature(req, nil, cfg); err != nil {
		t.Fatalf("verifyRequestSignature error: %v", err)
	}
}

func TestParseS3AuthInfoKeepsPlusInCredential(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://example.com/b/?X-Amz-Credential=LxhRnZpxmP%2BzoSVTZToF%2F20260526%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Signature=abc&X-Amz-SignedHeaders=host&X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Date=20260526T120000Z&X-Amz-Expires=60", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	info, err := parseS3AuthInfo(req)
	if err != nil {
		t.Fatalf("parseS3AuthInfo error: %v", err)
	}
	if info.Credential != "LxhRnZpxmP+zoSVTZToF/20260526/us-east-1/s3/aws4_request" {
		t.Fatalf("unexpected credential: %q", info.Credential)
	}
	if !info.IsPresign {
		t.Fatalf("expected presign request")
	}
}

func TestS3HandlerEndToEndSignedListLocation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	storage := model.Storage{
		Driver:    "Local",
		MountPath: "/data",
		Addition:   `{"root_folder_path":"` + root + `","thumbnail":false,"show_hidden":true,"mkdir_perm":"777","thumb_concurrency":"1","recycle_bin_path":"delete permanently"}`,
	}
	if _, err := op.CreateStorage(t.Context(), storage); err != nil {
		t.Fatalf("create storage: %v", err)
	}

	buckets, _ := json.Marshal([]map[string]string{{"name": "bucket", "path": "/data"}})
	if err := op.SaveSettingItems([]model.SettingItem{
		{Key: conf.S3Endpoint, Value: "http://127.0.0.1:9000", Type: conf.TypeString, Group: model.S3, Flag: model.PRIVATE},
		{Key: conf.S3Region, Value: "us-east-1", Type: conf.TypeString, Group: model.S3, Flag: model.PRIVATE},
		{Key: conf.S3Port, Value: "9000", Type: conf.TypeNumber, Group: model.S3, Flag: model.PRIVATE},
		{Key: conf.S3AccessKeyID, Value: "LxhRnZpxmP+zoSVTZToF", Type: conf.TypeString, Group: model.S3, Flag: model.PRIVATE},
		{Key: conf.S3SecretAccessKey, Value: "V3PtKffK7Uk2HeT5hjf9EcjntpQEyuetS+54nYIs", Type: conf.TypeString, Group: model.S3, Flag: model.PRIVATE},
		{Key: conf.S3Buckets, Value: string(buckets), Type: conf.TypeText, Group: model.S3, Flag: model.PRIVATE},
	}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.AccessKeyID != "LxhRnZpxmP+zoSVTZToF" {
		t.Fatalf("access key was not preserved: %q", cfg.AccessKeyID)
	}

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:9000/bucket?location", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = "127.0.0.1:9000"
	signer := awsv4.NewSigner(credentials.NewStaticCredentials(cfg.AccessKeyID, cfg.SecretAccessKey, ""))
	signer.DisableURIPathEscaping = true
	if _, err := signer.Sign(req, nil, "s3", cfg.Region, time.Now().UTC()); err != nil {
		t.Fatalf("sign request: %v", err)
	}

	rec := httptest.NewRecorder()
	(&handler{}).ServeHTTP(rec, req)
	if rec.Code == http.StatusForbidden {
		t.Fatalf("unexpected 403: %s", rec.Body.String())
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("LocationConstraint")) {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}
