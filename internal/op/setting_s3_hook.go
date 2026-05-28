package op

import (
	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/model"
)

func init() {
	RegisterSettingItemHook(conf.S3Buckets, func(item *model.SettingItem) error {
		normalized, _, err := NormalizeS3BucketMappings(item.Value)
		if err != nil {
			return err
		}
		item.Value = normalized
		return nil
	})
}
