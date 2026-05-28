package s3server

import (
	"time"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
)

func init() {
	registerReloadHook(conf.S3Endpoint)
	registerReloadHook(conf.S3Region)
	registerReloadHook(conf.S3Port)
	registerReloadHook(conf.S3Buckets)
}

func registerReloadHook(key string) {
	op.RegisterSettingItemHook(key, func(item *model.SettingItem) error {
		time.AfterFunc(200*time.Millisecond, Refresh)
		return nil
	})
}
