package db

import (
	"errors"
	"time"

	"github.com/alist-org/alist/v3/internal/model"
	"gorm.io/gorm"
)

func GetVideoFavoriteByPath(userID uint, storageID uint, path string) (*model.VideoFavorite, error) {
	var fav model.VideoFavorite
	err := db.Where("user_id = ? AND storage_id = ? AND path = ?", userID, storageID, path).First(&fav).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &fav, nil
}

func GetVideoFavoriteByIDs(userID uint, ids []uint) ([]model.VideoFavorite, error) {
	if len(ids) == 0 {
		return []model.VideoFavorite{}, nil
	}
	var rows []model.VideoFavorite
	if err := db.Where("user_id = ? AND id IN ?", userID, ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func GetVideoFavorites(userID uint, storageID uint, page, perPage int) (total int64, rows []model.VideoFavorite, err error) {
	q := db.Model(&model.VideoFavorite{}).Where("user_id = ?", userID)
	if storageID > 0 {
		q = q.Where("storage_id = ?", storageID)
	}
	if err = q.Count(&total).Error; err != nil {
		return
	}
	if total == 0 {
		rows = []model.VideoFavorite{}
		return
	}
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 {
		perPage = 100
	}
	offset := (page - 1) * perPage
	err = q.Order("created_at DESC").Offset(offset).Limit(perPage).Find(&rows).Error
	return
}

func SaveVideoFavorite(fav *model.VideoFavorite) error {
	if fav == nil {
		return nil
	}
	now := time.Now()
	if fav.ID == 0 && fav.CreatedAt.IsZero() {
		fav.CreatedAt = now
	}
	fav.UpdatedAt = now
	return db.Save(fav).Error
}

func DeleteVideoFavoriteByID(userID uint, id uint) error {
	return db.Where("user_id = ? AND id = ?", userID, id).Delete(&model.VideoFavorite{}).Error
}

func DeleteVideoFavoriteByPath(userID uint, storageID uint, path string) error {
	return db.Where("user_id = ? AND storage_id = ? AND path = ?", userID, storageID, path).Delete(&model.VideoFavorite{}).Error
}

func DeleteVideoFavoritesByIDs(userID uint, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return db.Where("user_id = ? AND id IN ?", userID, ids).Delete(&model.VideoFavorite{}).Error
}
