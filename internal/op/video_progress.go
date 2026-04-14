package op

import (
	"math"
	"time"

	"github.com/alist-org/alist/v3/internal/db"
	"github.com/alist-org/alist/v3/internal/model"
)

const (
	videoFinishedThreshold = 0.9
)

type VideoProgressInfo struct {
	Position  float64
	Duration  float64
	Finished  bool
	UpdatedAt time.Time
}

func GetVideoProgress(storageID uint, path string, fileID string) (*model.VideoProgress, error) {
	path = normalizeVideoPath(path)
	if fileID != "" {
		if vp, err := db.GetVideoProgressByStorageAndFileID(storageID, fileID); err != nil || vp != nil {
			return vp, err
		}
	}
	return db.GetVideoProgressByStorageAndPath(storageID, path)
}

func GetVideoProgressMap(storageID uint, paths []string) (map[string]VideoProgressInfo, error) {
	if storageID == 0 || len(paths) == 0 {
		return map[string]VideoProgressInfo{}, nil
	}
	for i, p := range paths {
		paths[i] = normalizeVideoPath(p)
	}
	rows, err := db.GetVideoProgressMapByPaths(storageID, paths)
	if err != nil {
		return nil, err
	}
	result := make(map[string]VideoProgressInfo, len(rows))
	for path, row := range rows {
		result[path] = VideoProgressInfo{
			Position:  row.Position,
			Duration:  row.Duration,
			Finished:  row.Finished,
			UpdatedAt: row.UpdatedAt,
		}
	}
	return result, nil
}

func UpsertVideoProgress(storageID uint, path string, fileID string, size int64, modTime int64, position float64, duration float64) (*model.VideoProgress, error) {
	path = normalizeVideoPath(path)
	position = math.Max(0, position)
	if duration < 0 {
		duration = 0
	}
	if duration > 0 && position > duration {
		position = duration
	}
	finished := duration > 0 && position/duration >= videoFinishedThreshold

	var vp *model.VideoProgress
	var err error
	if fileID != "" {
		vp, err = db.GetVideoProgressByStorageAndFileID(storageID, fileID)
		if err != nil {
			return nil, err
		}
	}
	if vp == nil {
		vp, err = db.GetVideoProgressByStorageAndPath(storageID, path)
		if err != nil {
			return nil, err
		}
	}
	if vp == nil {
		vp = &model.VideoProgress{
			StorageID: storageID,
			Path:      path,
			FileID:    fileID,
		}
	}
	vp.Path = path
	if fileID != "" {
		vp.FileID = fileID
	}
	vp.Size = size
	vp.ModTime = modTime
	vp.Position = position
	vp.Duration = duration
	vp.Finished = finished
	if err := db.SaveVideoProgress(vp); err != nil {
		return nil, err
	}
	return vp, nil
}

func MarkVideoWatched(storageID uint, path string, fileID string, size int64, modTime int64) (*model.VideoProgress, error) {
	path = normalizeVideoPath(path)

	var vp *model.VideoProgress
	var err error
	if fileID != "" {
		vp, err = db.GetVideoProgressByStorageAndFileID(storageID, fileID)
		if err != nil {
			return nil, err
		}
	}
	if vp == nil {
		vp, err = db.GetVideoProgressByStorageAndPath(storageID, path)
		if err != nil {
			return nil, err
		}
	}
	if vp == nil {
		vp = &model.VideoProgress{
			StorageID: storageID,
			Path:      path,
			FileID:    fileID,
		}
	}
	vp.Path = path
	if fileID != "" {
		vp.FileID = fileID
	}
	vp.Size = size
	vp.ModTime = modTime
	vp.Finished = true
	if err := db.SaveVideoProgress(vp); err != nil {
		return nil, err
	}
	return vp, nil
}
