package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/setting"
	"github.com/alist-org/alist/v3/pkg/utils"
	log "github.com/sirupsen/logrus"
)

const (
	defaultTempFileCleanupRetentionHours = 24
	tempCleanupScanInterval              = 5 * time.Minute
)

var aListTempFilePrefixes = []string{
	// Keep legacy prefixes so upgrading instances still purge old temp files.
	"file-",
	"thumb-video-",
	"thumb-video-seg-",
}

func isAListTempFile(name string) bool {
	for _, prefix := range aListTempFilePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func getTempFileCleanupRetention() time.Duration {
	retentionHours := setting.GetInt(conf.TempFileCleanupRetentionHours, defaultTempFileCleanupRetentionHours)
	if retentionHours <= 0 {
		retentionHours = defaultTempFileCleanupRetentionHours
	}
	return time.Duration(retentionHours) * time.Hour
}

func cleanupAListTempFiles(dir string, retention time.Duration, force bool) {
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Errorf("failed list temp files in %s: %v", dir, err)
		return
	}
	now := time.Now()
	for _, entry := range entries {
		name := entry.Name()
		if !isAListTempFile(name) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			log.Warnf("failed stat temp file %s: %v", filepath.Join(dir, name), err)
			continue
		}
		if !force {
			if retention <= 0 {
				continue
			}
			if now.Sub(info.ModTime()) < retention {
				continue
			}
		}
		if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
			log.Warnf("failed delete temp file %s: %v", filepath.Join(dir, name), err)
		}
	}
}

func CleanTempDir() {
	if conf.Conf == nil {
		return
	}
	cleanupAListTempFiles(conf.Conf.TempDir, 0, true)
}

func InitTempCleanup() {
	if conf.Conf == nil {
		return
	}
	cleanupAListTempFiles(conf.Conf.TempDir, 0, true)
	utils.GoSafe("temp-file-cleanup", func() {
		ticker := time.NewTicker(tempCleanupScanInterval)
		defer ticker.Stop()
		for range ticker.C {
			cleanupAListTempFiles(conf.Conf.TempDir, getTempFileCleanupRetention(), false)
		}
	})
}
