package op

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/pkg/utils"
)

func ResolveBucketLocalPath(rawPath string) (string, error) {
	cleaned := utils.FixAndCleanPath(rawPath)
	if cleaned == "/" {
		return "", errs.NewErr(errs.StorageNotFound, "rawPath: %s", rawPath)
	}

	if filepath.IsAbs(cleaned) {
		if stat, err := os.Stat(cleaned); err == nil && stat != nil {
			if resolved, err := filepath.EvalSymlinks(cleaned); err == nil && resolved != "" {
				return filepath.Clean(resolved), nil
			}
			return filepath.Clean(cleaned), nil
		}
	}

	if storage, mountPath, ok := getLongestMatchingStorage(cleaned); ok {
		if !storage.Config().OnlyLocal {
			return "", fmt.Errorf("storage %q must be local storage", mountPath)
		}
		resolver, ok := storage.(driver.BackendPathResolver)
		if !ok {
			return "", fmt.Errorf("storage %q does not support backend path resolution", mountPath)
		}
		suffix := strings.TrimPrefix(cleaned, mountPath)
		localPath := filepath.Clean(resolver.BackendPath(suffix))
		if localPath == "" {
			return "", fmt.Errorf("empty resolved local path")
		}
		return localPath, nil
	}
	return "", errs.NewErr(errs.StorageNotFound, "rawPath: %s", rawPath)
}

func NormalizeS3BucketMappings(raw string) (string, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "[]", true, nil
	}

	type bucketMapping struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}

	var buckets []bucketMapping
	if err := json.Unmarshal([]byte(raw), &buckets); err != nil {
		return "", false, err
	}

	normalized := make([]bucketMapping, 0, len(buckets))
	changed := false
	for _, bucket := range buckets {
		name := strings.TrimSpace(bucket.Name)
		path := strings.TrimSpace(bucket.Path)
		if name == "" || path == "" {
			changed = true
			continue
		}
		localPath, err := ResolveBucketLocalPath(path)
		if err != nil {
			if errors.Is(err, errs.StorageNotFound) {
				normalized = append(normalized, bucketMapping{
					Name: name,
					Path: path,
				})
				continue
			}
			return "", false, err
		}
		if localPath != path {
			changed = true
		}
		normalized = append(normalized, bucketMapping{
			Name: name,
			Path: localPath,
		})
	}

	out, err := json.Marshal(normalized)
	if err != nil {
		return "", false, err
	}
	return string(out), changed || string(out) != raw, nil
}

func getLongestMatchingStorage(path string) (driver.Driver, string, bool) {
	path = utils.FixAndCleanPath(path)
	var (
		chosen     driver.Driver
		chosenPath string
	)
	for _, storage := range GetAllStorages() {
		mountPath := utils.GetActualMountPath(storage.GetStorage().MountPath)
		if !utils.IsSubPath(mountPath, path) {
			continue
		}
		if chosen == nil || len(mountPath) > len(chosenPath) || (len(mountPath) == len(chosenPath) && mountPath < chosenPath) {
			chosen = storage
			chosenPath = mountPath
		}
	}
	return chosen, chosenPath, chosen != nil
}
