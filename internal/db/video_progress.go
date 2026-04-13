package db

import (
	"errors"
	"time"

	"github.com/alist-org/alist/v3/internal/model"
	"gorm.io/gorm"
)

func GetVideoProgressByStorageAndPath(storageID uint, path string) (*model.VideoProgress, error) {
	var vp model.VideoProgress
	err := db.Where("storage_id = ? AND path = ?", storageID, path).First(&vp).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &vp, nil
}

func GetVideoProgressByStorageAndFileID(storageID uint, fileID string) (*model.VideoProgress, error) {
	if fileID == "" {
		return nil, nil
	}
	var vp model.VideoProgress
	err := db.Where("storage_id = ? AND file_id = ?", storageID, fileID).First(&vp).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &vp, nil
}

func GetVideoProgressMapByPaths(storageID uint, paths []string) (map[string]model.VideoProgress, error) {
	result := map[string]model.VideoProgress{}
	if storageID == 0 || len(paths) == 0 {
		return result, nil
	}
	var rows []model.VideoProgress
	if err := db.Where("storage_id = ? AND path IN ?", storageID, paths).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.Path] = row
	}
	return result, nil
}

func SaveVideoProgress(vp *model.VideoProgress) error {
	if vp == nil {
		return nil
	}
	vp.UpdatedAt = time.Now()
	return db.Save(vp).Error
}
