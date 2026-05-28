package s3server

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/alist-org/alist/v3/pkg/utils/random"
)

type BucketMapping struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type bucketMappingTarget struct {
	localRootPath string
}

type Config struct {
	Endpoint        string
	Region          string
	Port            int
	AccessKeyID     string
	SecretAccessKey string
	Buckets         map[string]bucketMappingTarget
}

func isAlnumOnly(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

func ensureS3CredentialSettings(settings map[string]string) (string, string, error) {
	accessKeyID := strings.TrimSpace(settings[conf.S3AccessKeyID])
	secretAccessKey := strings.TrimSpace(settings[conf.S3SecretAccessKey])
	if isValidS3AccessKeyID(accessKeyID) && isValidS3SecretAccessKey(secretAccessKey) {
		return accessKeyID, secretAccessKey, nil
	}

	accessKeyID = random.String(20)
	secretAccessKey = random.String(40)
	items := []model.SettingItem{
		{
			Key:   conf.S3AccessKeyID,
			Value: accessKeyID,
			Type:  conf.TypeString,
			Group: model.S3,
			Flag:  model.PRIVATE,
		},
		{
			Key:   conf.S3SecretAccessKey,
			Value: secretAccessKey,
			Type:  conf.TypeString,
			Group: model.S3,
			Flag:  model.PRIVATE,
		},
	}
	if err := op.SaveSettingItems(items); err != nil {
		return "", "", err
	}
	return accessKeyID, secretAccessKey, nil
}

func isValidS3AccessKeyID(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.Contains(value, "/")
}

func isValidS3SecretAccessKey(value string) bool {
	return strings.TrimSpace(value) != ""
}

func loadConfig() (*Config, error) {
	items, err := op.GetSettingItemsByGroup(model.S3)
	if err != nil {
		return nil, err
	}
	settings := make(map[string]string, len(items))
	for _, item := range items {
		settings[item.Key] = strings.TrimSpace(item.Value)
	}

	port := 0
	if raw := strings.TrimSpace(settings[conf.S3Port]); raw != "" {
		port, err = strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid s3 port: %w", err)
		}
	}

	cfg := &Config{
		Endpoint: strings.TrimSpace(settings[conf.S3Endpoint]),
		Region:   strings.TrimSpace(settings[conf.S3Region]),
		Port:     port,
		Buckets:  make(map[string]bucketMappingTarget),
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}

	if cfg.AccessKeyID, cfg.SecretAccessKey, err = ensureS3CredentialSettings(settings); err != nil {
		return nil, err
	}

	var buckets []BucketMapping
	if raw := strings.TrimSpace(settings[conf.S3Buckets]); raw != "" {
		if err := utils.Json.UnmarshalFromString(raw, &buckets); err != nil {
			return nil, fmt.Errorf("invalid s3 bucket mapping: %w", err)
		}
	}
	for _, bucket := range buckets {
		name := strings.TrimSpace(bucket.Name)
		rawPath := strings.TrimSpace(bucket.Path)
		if name == "" || rawPath == "" {
			continue
		}
		localRootPath, err := op.ResolveBucketLocalPath(rawPath)
		if err != nil {
			return nil, fmt.Errorf("invalid s3 bucket mapping %q: %w", name, err)
		}
		cfg.Buckets[name] = bucketMappingTarget{
			localRootPath: localRootPath,
		}
	}
	return cfg, nil
}

func (c *Config) enabled() bool {
	if c == nil {
		return false
	}
	if c.Port <= 0 {
		return false
	}
	if c.AccessKeyID == "" || c.SecretAccessKey == "" {
		return false
	}
	return len(c.Buckets) > 0
}

func (c *Config) listenAddr() string {
	if c == nil || c.Port <= 0 {
		return ""
	}
	return fmt.Sprintf(":%d", c.Port)
}

func (c *Config) bucketPath(bucket string) (bucketMappingTarget, bool) {
	if c == nil {
		return bucketMappingTarget{}, false
	}
	p, ok := c.Buckets[bucket]
	return p, ok
}
