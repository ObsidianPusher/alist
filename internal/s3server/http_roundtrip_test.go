package s3server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/alist-org/alist/v3/drivers/local"
	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/db"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
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

func TestS3HandlerPutHeadGetRangeRoundTrip(t *testing.T) {
	mountPath := "/data-" + strings.ToLower(strings.ReplaceAll(t.Name(), "_", "-"))
	root := t.TempDir()
	storage := model.Storage{
		Driver:    "Local",
		MountPath: mountPath,
		Addition:  `{"root_folder_path":"` + root + `","thumbnail":false,"show_hidden":true,"mkdir_perm":"777","thumb_concurrency":"1","recycle_bin_path":"delete permanently"}`,
	}
	if _, err := op.CreateStorage(t.Context(), storage); err != nil {
		t.Fatalf("create storage: %v", err)
	}

	buckets, _ := json.Marshal([]map[string]string{{"name": "bucket", "path": mountPath}})
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
	signer := awsv4.NewSigner(credentials.NewStaticCredentials(cfg.AccessKeyID, cfg.SecretAccessKey, ""))
	signer.DisableURIPathEscaping = true

	putReq, err := http.NewRequest(http.MethodPut, "http://127.0.0.1:9000/bucket/folder/note.txt", bytes.NewReader([]byte("hello world")))
	if err != nil {
		t.Fatalf("new put request: %v", err)
	}
	putReq.Host = "127.0.0.1:9000"
	putReq.Header.Set("Content-Type", "text/plain")
	if _, err := signer.Sign(putReq, bytes.NewReader([]byte("hello world")), "s3", cfg.Region, time.Now().UTC()); err != nil {
		t.Fatalf("sign put request: %v", err)
	}

	putRec := httptest.NewRecorder()
	(&handler{}).ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("put failed: status=%d body=%s", putRec.Code, putRec.Body.String())
	}
	if got := strings.TrimSpace(putRec.Header().Get("ETag")); got == "" {
		t.Fatalf("put missing etag")
	}

	headReq, err := http.NewRequest(http.MethodHead, "http://127.0.0.1:9000/bucket/folder/note.txt", nil)
	if err != nil {
		t.Fatalf("new head request: %v", err)
	}
	headReq.Host = "127.0.0.1:9000"
	if _, err := signer.Sign(headReq, nil, "s3", cfg.Region, time.Now().UTC()); err != nil {
		t.Fatalf("sign head request: %v", err)
	}
	headRec := httptest.NewRecorder()
	(&handler{}).ServeHTTP(headRec, headReq)
	if headRec.Code != http.StatusOK {
		t.Fatalf("head failed: status=%d body=%s", headRec.Code, headRec.Body.String())
	}
	if headRec.Header().Get("ETag") == "" || headRec.Header().Get("Content-Length") == "" || headRec.Header().Get("Last-Modified") == "" {
		t.Fatalf("head headers incomplete: %#v", headRec.Header())
	}

	getReq, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:9000/bucket/folder/note.txt", nil)
	if err != nil {
		t.Fatalf("new get request: %v", err)
	}
	getReq.Host = "127.0.0.1:9000"
	getReq.Header.Set("Range", "bytes=0-4")
	if _, err := signer.Sign(getReq, nil, "s3", cfg.Region, time.Now().UTC()); err != nil {
		t.Fatalf("sign get request: %v", err)
	}
	getRec := httptest.NewRecorder()
	(&handler{}).ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusPartialContent {
		t.Fatalf("get range failed: status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	if getRec.Header().Get("Content-Range") == "" {
		t.Fatalf("missing content-range header: %#v", getRec.Header())
	}
	if body := getRec.Body.String(); body != "hello" {
		t.Fatalf("unexpected range body: %q", body)
	}
}

func TestS3HandlerMultipartRoundTrip(t *testing.T) {
	mountPath := "/data-" + strings.ToLower(strings.ReplaceAll(t.Name(), "_", "-"))
	root := t.TempDir()
	storage := model.Storage{
		Driver:    "Local",
		MountPath: mountPath,
		Addition:  `{"root_folder_path":"` + root + `","thumbnail":false,"show_hidden":true,"mkdir_perm":"777","thumb_concurrency":"1","recycle_bin_path":"delete permanently"}`,
	}
	if _, err := op.CreateStorage(t.Context(), storage); err != nil {
		t.Fatalf("create storage: %v", err)
	}

	buckets, _ := json.Marshal([]map[string]string{{"name": "bucket", "path": mountPath}})
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
	signer := awsv4.NewSigner(credentials.NewStaticCredentials(cfg.AccessKeyID, cfg.SecretAccessKey, ""))
	signer.DisableURIPathEscaping = true

	initReq, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:9000/bucket/big.bin?uploads", nil)
	if err != nil {
		t.Fatalf("new initiate request: %v", err)
	}
	initReq.Host = "127.0.0.1:9000"
	if _, err := signer.Sign(initReq, nil, "s3", cfg.Region, time.Now().UTC()); err != nil {
		t.Fatalf("sign initiate request: %v", err)
	}
	initRec := httptest.NewRecorder()
	(&handler{}).ServeHTTP(initRec, initReq)
	if initRec.Code != http.StatusOK {
		t.Fatalf("initiate multipart failed: status=%d body=%s", initRec.Code, initRec.Body.String())
	}
	uploadID := extractXMLValue(initRec.Body.String(), "UploadId")
	if uploadID == "" {
		t.Fatalf("missing upload id: %s", initRec.Body.String())
	}

	partReq, err := http.NewRequest(http.MethodPut, "http://127.0.0.1:9000/bucket/big.bin?partNumber=1&uploadId="+uploadID, bytes.NewReader([]byte("part-one")))
	if err != nil {
		t.Fatalf("new upload part request: %v", err)
	}
	partReq.Host = "127.0.0.1:9000"
	if _, err := signer.Sign(partReq, bytes.NewReader([]byte("part-one")), "s3", cfg.Region, time.Now().UTC()); err != nil {
		t.Fatalf("sign upload part request: %v", err)
	}
	partRec := httptest.NewRecorder()
	(&handler{}).ServeHTTP(partRec, partReq)
	if partRec.Code != http.StatusOK {
		t.Fatalf("upload part failed: status=%d body=%s", partRec.Code, partRec.Body.String())
	}
	etag := strings.TrimSpace(partRec.Header().Get("ETag"))
	if etag == "" {
		t.Fatalf("missing part etag")
	}

	completeBody := `<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>` + etag + `</ETag></Part></CompleteMultipartUpload>`
	completeReq, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:9000/bucket/big.bin?uploadId="+uploadID, strings.NewReader(completeBody))
	if err != nil {
		t.Fatalf("new complete request: %v", err)
	}
	completeReq.Host = "127.0.0.1:9000"
	completeReq.Header.Set("Content-Type", "application/xml")
	if _, err := signer.Sign(completeReq, strings.NewReader(completeBody), "s3", cfg.Region, time.Now().UTC()); err != nil {
		t.Fatalf("sign complete request: %v", err)
	}
	completeRec := httptest.NewRecorder()
	(&handler{}).ServeHTTP(completeRec, completeReq)
	if completeRec.Code != http.StatusOK {
		t.Fatalf("complete multipart failed: status=%d body=%s", completeRec.Code, completeRec.Body.String())
	}

	body, err := os.ReadFile(filepath.Join(root, "big.bin"))
	if err != nil {
		t.Fatalf("read completed file: %v", err)
	}
	if string(body) != "part-one" {
		t.Fatalf("unexpected completed body: %q", string(body))
	}
}

func TestS3HandlerDeepAbsoluteBucketPathRoundTrip(t *testing.T) {
	root := t.TempDir()
	bucketRoot := filepath.Join(root, ".ugreen_nas", "830402", "图片")
	if err := os.MkdirAll(bucketRoot, 0o755); err != nil {
		t.Fatalf("mkdir bucket root: %v", err)
	}

	buckets, _ := json.Marshal([]map[string]string{{"name": "deep", "path": bucketRoot}})
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
	signer := awsv4.NewSigner(credentials.NewStaticCredentials(cfg.AccessKeyID, cfg.SecretAccessKey, ""))
	signer.DisableURIPathEscaping = true

	putBody := []byte("deep-path-content")
	putReq, err := http.NewRequest(http.MethodPut, "http://127.0.0.1:9000/deep/nested/note.txt", bytes.NewReader(putBody))
	if err != nil {
		t.Fatalf("new put request: %v", err)
	}
	putReq.Host = "127.0.0.1:9000"
	putReq.Header.Set("Content-Type", "text/plain")
	if _, err := signer.Sign(putReq, bytes.NewReader(putBody), "s3", cfg.Region, time.Now().UTC()); err != nil {
		t.Fatalf("sign put request: %v", err)
	}
	putRec := httptest.NewRecorder()
	(&handler{}).ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("put failed: status=%d body=%s", putRec.Code, putRec.Body.String())
	}

	headReq, err := http.NewRequest(http.MethodHead, "http://127.0.0.1:9000/deep/nested/note.txt", nil)
	if err != nil {
		t.Fatalf("new head request: %v", err)
	}
	headReq.Host = "127.0.0.1:9000"
	if _, err := signer.Sign(headReq, nil, "s3", cfg.Region, time.Now().UTC()); err != nil {
		t.Fatalf("sign head request: %v", err)
	}
	headRec := httptest.NewRecorder()
	(&handler{}).ServeHTTP(headRec, headReq)
	if headRec.Code != http.StatusOK {
		t.Fatalf("head failed: status=%d body=%s", headRec.Code, headRec.Body.String())
	}

	getReq, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:9000/deep/nested/note.txt", nil)
	if err != nil {
		t.Fatalf("new get request: %v", err)
	}
	getReq.Host = "127.0.0.1:9000"
	if _, err := signer.Sign(getReq, nil, "s3", cfg.Region, time.Now().UTC()); err != nil {
		t.Fatalf("sign get request: %v", err)
	}
	getRec := httptest.NewRecorder()
	(&handler{}).ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get failed: status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	if body := getRec.Body.String(); body != string(putBody) {
		t.Fatalf("unexpected body: %q", body)
	}

	listReq, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:9000/deep/?list-type=2&prefix=nested/", nil)
	if err != nil {
		t.Fatalf("new list request: %v", err)
	}
	listReq.Host = "127.0.0.1:9000"
	if _, err := signer.Sign(listReq, nil, "s3", cfg.Region, time.Now().UTC()); err != nil {
		t.Fatalf("sign list request: %v", err)
	}
	listRec := httptest.NewRecorder()
	(&handler{}).ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list failed: status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	if !strings.Contains(listRec.Body.String(), "<Key>nested/note.txt</Key>") {
		t.Fatalf("list missing object: %s", listRec.Body.String())
	}
}

func extractXMLValue(body, key string) string {
	open := "<" + key + ">"
	close := "</" + key + ">"
	start := strings.Index(body, open)
	if start < 0 {
		return ""
	}
	start += len(open)
	end := strings.Index(body[start:], close)
	if end < 0 {
		return ""
	}
	return body[start : start+end]
}
