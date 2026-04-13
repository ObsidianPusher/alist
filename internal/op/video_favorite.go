package op

import (
	"path"
	"strings"

	"github.com/alist-org/alist/v3/internal/db"
	"github.com/alist-org/alist/v3/internal/model"
)

func cleanFavoritePath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/"
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	if len(raw) > 1 && strings.HasSuffix(raw, "/") {
		raw = strings.TrimSuffix(raw, "/")
	}
	return raw
}

func ListVideoFavorites(userID uint, storageID uint, page, perPage int) (int64, []model.VideoFavorite, error) {
	return db.GetVideoFavorites(userID, storageID, page, perPage)
}

func IsVideoFavorite(userID uint, storageID uint, path string) (bool, *model.VideoFavorite, error) {
	path = cleanFavoritePath(path)
	fav, err := db.GetVideoFavoriteByPath(userID, storageID, path)
	if err != nil {
		return false, nil, err
	}
	return fav != nil, fav, nil
}

func UpsertVideoFavorite(userID uint, storageID uint, storageName string, fullPath string, fileID string, size int64, modTime int64) (*model.VideoFavorite, error) {
	fullPath = cleanFavoritePath(fullPath)
	fav, err := db.GetVideoFavoriteByPath(userID, storageID, fullPath)
	if err != nil {
		return nil, err
	}
	if fav == nil {
		fav = &model.VideoFavorite{
			UserID:      userID,
			StorageID:   storageID,
			StorageName: storageName,
			Path:        fullPath,
			Name:        path.Base(fullPath),
			FileID:      fileID,
			Size:        size,
			ModTime:     modTime,
		}
	} else {
		fav.StorageName = storageName
		fav.Name = path.Base(fullPath)
		if fileID != "" {
			fav.FileID = fileID
		}
		fav.Size = size
		fav.ModTime = modTime
	}
	if err := db.SaveVideoFavorite(fav); err != nil {
		return nil, err
	}
	return fav, nil
}

func DeleteVideoFavorite(userID uint, storageID uint, fullPath string) error {
	fullPath = cleanFavoritePath(fullPath)
	return db.DeleteVideoFavoriteByPath(userID, storageID, fullPath)
}

func DeleteVideoFavoritesByIDs(userID uint, ids []uint) error {
	return db.DeleteVideoFavoritesByIDs(userID, ids)
}

func GetVideoFavoritesByIDs(userID uint, ids []uint) ([]model.VideoFavorite, error) {
	return db.GetVideoFavoriteByIDs(userID, ids)
}
