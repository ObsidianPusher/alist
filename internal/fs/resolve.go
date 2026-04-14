package fs

import (
	"time"

	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/pkg/errors"
)

func resolveStoragePath(path string) (driver.Driver, string, error) {
	storage, actualPath, err := op.GetStorageAndActualPath(path)
	if err != nil {
		return nil, "", errors.WithMessage(err, "failed get storage")
	}
	return storage, actualPath, nil
}

func resolveRootOrStoragePath(path string) (driver.Driver, string, model.Obj, error) {
	cleanPath := utils.FixAndCleanPath(path)
	if cleanPath == "/" {
		return nil, "/", &model.Object{
			Name:     "root",
			Size:     0,
			Modified: time.Time{},
			IsFolder: true,
		}, nil
	}
	storage, actualPath, err := resolveStoragePath(cleanPath)
	if err != nil {
		return nil, "", nil, err
	}
	return storage, actualPath, nil, nil
}
