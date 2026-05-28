package op

import (
	"testing"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/model"
)

func TestNormalizeS3CredentialItemsRegeneratesPair(t *testing.T) {
	items := []model.SettingItem{
		{Key: conf.S3AccessKeyID, Value: "abc+123", Type: conf.TypeString, Group: model.S3, Flag: model.PRIVATE},
		{Key: conf.S3SecretAccessKey, Value: "def456", Type: conf.TypeString, Group: model.S3, Flag: model.PRIVATE},
	}
	normalized := normalizeS3CredentialItems(items)
	if normalized[0].Value != "abc+123" || normalized[1].Value != "def456" {
		t.Fatalf("expected credential pair to be preserved")
	}
	if !isValidS3AccessKeyID(normalized[0].Value) || !isValidS3SecretAccessKey(normalized[1].Value) {
		t.Fatalf("expected preserved credentials to stay valid")
	}
}
