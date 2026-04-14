package handles

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/task"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/alist-org/alist/v3/server/common"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

type VideoFavoriteStatusReq struct {
	Path string `form:"path"`
}

type VideoFavoriteToggleReq struct {
	Path     string `json:"path"`
	Favorite *bool  `json:"favorite,omitempty"`
}

type VideoFavoriteListReq struct {
	model.PageReq
	StorageID uint `json:"storage_id" form:"storage_id"`
}

type VideoFavoriteBatchCopyReq struct {
	IDs    []uint `json:"ids"`
	DstDir string `json:"dst_dir"`
}

type VideoFavoriteBatchRemoveReq struct {
	IDs []uint `json:"ids"`
}

type VideoFavoriteStorageFilter struct {
	StorageID   uint   `json:"storage_id"`
	StorageName string `json:"storage_name"`
}

type VideoFavoriteItemResp struct {
	ID          uint      `json:"id"`
	Path        string    `json:"path"`
	Name        string    `json:"name"`
	Size        int64     `json:"size"`
	Modified    time.Time `json:"modified"`
	Type        int       `json:"type"`
	StorageID   uint      `json:"storage_id"`
	StorageName string    `json:"storage_name"`
	FavoritedAt time.Time `json:"favorited_at"`
	Progress    float64   `json:"progress,omitempty"`
	Watched     bool      `json:"watched,omitempty"`
	LastPlayed  time.Time `json:"last_played,omitempty"`
}

type VideoFavoriteListResp struct {
	Content         []VideoFavoriteItemResp      `json:"content"`
	Total           int64                        `json:"total"`
	StorageFilters  []VideoFavoriteStorageFilter `json:"storage_filters"`
	InvalidPruned   int                          `json:"invalid_pruned"`
	PermissionPrune int                          `json:"permission_prune"`
}

type VideoFavoriteStatusResp struct {
	Path     string    `json:"path"`
	Favorite bool      `json:"favorite"`
	ID       uint      `json:"id,omitempty"`
	At       time.Time `json:"at,omitempty"`
}

func toUserRelativePath(basePath, absPath string) string {
	basePath = utils.FixAndCleanPath(basePath)
	absPath = utils.FixAndCleanPath(absPath)
	if basePath == "/" || basePath == "" {
		return absPath
	}
	if !strings.HasPrefix(absPath, basePath) {
		return absPath
	}
	rel := strings.TrimPrefix(absPath, basePath)
	if rel == "" {
		return "/"
	}
	if !strings.HasPrefix(rel, "/") {
		rel = "/" + rel
	}
	return rel
}

func GetVideoFavoriteStatus(c *gin.Context) {
	var req VideoFavoriteStatusReq
	if err := c.ShouldBind(&req); err != nil || strings.TrimSpace(req.Path) == "" {
		common.ErrorStrResp(c, "path required", http.StatusBadRequest)
		return
	}
	access, ok := resolveVideoAccess(c, req.Path)
	if !ok {
		return
	}
	ok, fav, err := op.IsVideoFavorite(access.user.ID, access.storage.ID, access.reqPath)
	if err != nil {
		common.ErrorResp(c, err, http.StatusInternalServerError)
		return
	}
	resp := VideoFavoriteStatusResp{Path: req.Path, Favorite: ok}
	if fav != nil {
		resp.ID = fav.ID
		resp.At = fav.CreatedAt
	}
	common.SuccessResp(c, resp)
}

func ToggleVideoFavorite(c *gin.Context) {
	var req VideoFavoriteToggleReq
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Path) == "" {
		common.ErrorStrResp(c, "invalid payload", http.StatusBadRequest)
		return
	}
	access, ok := resolveVideoAccess(c, req.Path)
	if !ok {
		return
	}
	storage, actualPath, err := op.GetStorageAndActualPath(access.reqPath)
	if err != nil {
		common.ErrorResp(c, err, http.StatusNotFound)
		return
	}
	obj, err := op.Get(c, storage, actualPath)
	if err != nil {
		common.ErrorResp(c, err, http.StatusNotFound)
		return
	}
	if obj.IsDir() || utils.GetFileType(obj.GetName()) != conf.VIDEO {
		common.ErrorStrResp(c, "only video files can be favorited", http.StatusBadRequest)
		return
	}
	favState, _, err := op.IsVideoFavorite(access.user.ID, access.storage.ID, access.reqPath)
	if err != nil {
		common.ErrorResp(c, err, http.StatusInternalServerError)
		return
	}
	target := !favState
	if req.Favorite != nil {
		target = *req.Favorite
	}
	if target {
			_, err = op.UpsertVideoFavorite(
			access.user.ID,
			access.storage.ID,
			access.storage.MountPath,
			access.reqPath,
			obj.GetID(),
			obj.GetSize(),
			obj.ModTime().UnixMilli(),
		)
		if err != nil {
			common.ErrorResp(c, err, http.StatusInternalServerError)
			return
		}
	} else {
		if err := op.DeleteVideoFavorite(access.user.ID, access.storage.ID, access.reqPath); err != nil {
			common.ErrorResp(c, err, http.StatusInternalServerError)
			return
		}
	}
	common.SuccessResp(c, VideoFavoriteStatusResp{Path: req.Path, Favorite: target})
}

func ListVideoFavorites(c *gin.Context) {
	var req VideoFavoriteListReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, http.StatusBadRequest)
		return
	}
	req.Validate()
	user := c.MustGet("user").(*model.User)
	_, rows, err := op.ListVideoFavorites(user.ID, req.StorageID, req.Page, req.PerPage)
	if err != nil {
		common.ErrorResp(c, err, http.StatusInternalServerError)
		return
	}
	if len(rows) == 0 {
		common.SuccessResp(c, VideoFavoriteListResp{Content: []VideoFavoriteItemResp{}, Total: 0, StorageFilters: []VideoFavoriteStorageFilter{}})
		return
	}

	content := make([]VideoFavoriteItemResp, 0, len(rows))
	filtersMap := make(map[uint]string)
	invalidIDs := make([]uint, 0)
	permissionPruned := 0

	for _, row := range rows {
		filtersMap[row.StorageID] = row.StorageName
		fullPath := utils.FixAndCleanPath(row.Path)
		meta, metaErr := op.GetNearestMeta(fullPath)
		if metaErr != nil && !errors.Is(errors.Cause(metaErr), errs.MetaNotFound) {
			common.ErrorResp(c, metaErr, http.StatusInternalServerError)
			return
		}
		if !common.CanAccess(user, meta, fullPath, "") {
			permissionPruned++
			continue
		}
		storage, err := fs.GetStorage(fullPath, &fs.GetStoragesArgs{})
		if err != nil {
			if errs.IsNotFoundError(err) {
				invalidIDs = append(invalidIDs, row.ID)
				continue
			}
			common.ErrorResp(c, err, http.StatusInternalServerError)
			return
		}
		obj, err := fs.Get(c, fullPath, &fs.GetArgs{NoLog: true})
		if err != nil {
			if errs.IsObjectNotFound(err) {
				invalidIDs = append(invalidIDs, row.ID)
				continue
			}
			common.ErrorResp(c, err, http.StatusInternalServerError)
			return
		}
		if obj.IsDir() || utils.GetFileType(obj.GetName()) != conf.VIDEO {
			invalidIDs = append(invalidIDs, row.ID)
			continue
		}
		storageName := row.StorageName
		if storage != nil && storage.GetStorage() != nil {
			storageName = storage.GetStorage().MountPath
			filtersMap[row.StorageID] = storageName
		}
		item := VideoFavoriteItemResp{
			ID:          row.ID,
			Path:        toUserRelativePath(user.BasePath, fullPath),
			Name:        obj.GetName(),
			Size:        obj.GetSize(),
			Modified:    obj.ModTime(),
			Type:        utils.GetObjType(obj.GetName(), obj.IsDir()),
			StorageID:   row.StorageID,
			StorageName: storageName,
			FavoritedAt: row.CreatedAt,
		}
		if storage != nil && storage.GetStorage() != nil {
			if vp, err := op.GetVideoProgress(storage.GetStorage().ID, fullPath, obj.GetID()); err == nil && vp != nil {
				if vp.Duration > 0 {
					item.Progress = (vp.Position / vp.Duration) * 100
					if item.Progress < 0 {
						item.Progress = 0
					}
					if item.Progress > 100 {
						item.Progress = 100
					}
				}
				item.Watched = vp.Finished
				item.LastPlayed = vp.UpdatedAt
			}
		}
		content = append(content, item)
	}

	if len(invalidIDs) > 0 {
		_ = op.DeleteVideoFavoritesByIDs(user.ID, invalidIDs)
	}

	filters := make([]VideoFavoriteStorageFilter, 0, len(filtersMap))
	for storageID, storageName := range filtersMap {
		if storageName == "" {
			storageName = "storage"
		}
		filters = append(filters, VideoFavoriteStorageFilter{StorageID: storageID, StorageName: storageName})
	}

	common.SuccessResp(c, VideoFavoriteListResp{
		Content:         content,
		Total:           int64(len(content)),
		StorageFilters:  filters,
		InvalidPruned:   len(invalidIDs),
		PermissionPrune: permissionPruned,
	})
}

func BatchCopyVideoFavorites(c *gin.Context) {
	var req VideoFavoriteBatchCopyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResp(c, err, http.StatusBadRequest)
		return
	}
	if len(req.IDs) == 0 {
		common.ErrorStrResp(c, "ids required", http.StatusBadRequest)
		return
	}
	user := c.MustGet("user").(*model.User)
	if !user.CanCopy() {
		common.ErrorResp(c, errs.PermissionDenied, http.StatusForbidden)
		return
	}
	dstDir, err := user.JoinPath(req.DstDir)
	if err != nil {
		common.ErrorResp(c, err, http.StatusForbidden)
		return
	}
	favs, err := op.GetVideoFavoritesByIDs(user.ID, req.IDs)
	if err != nil {
		common.ErrorResp(c, err, http.StatusInternalServerError)
		return
	}
	if len(favs) != len(req.IDs) {
		common.ErrorStrResp(c, "some favorites not found", http.StatusBadRequest)
		return
	}

	storageID := favs[0].StorageID
	for _, fav := range favs[1:] {
		if fav.StorageID != storageID {
			common.ErrorStrResp(c, "batch copy only supports favorites in the same storage", http.StatusBadRequest)
			return
		}
	}

	groupID := ""
	if len(favs) > 1 {
		groupID = task.NewGroupID()
	}
	var ctx context.Context = c
	if groupID != "" {
		ctx = context.WithValue(ctx, conf.CopyGroupKey, groupID)
	}
	addedTasks := make([]task.TaskInfoWithCreator, 0, len(favs))
	for i, fav := range favs {
		srcPath := utils.FixAndCleanPath(fav.Path)
		t, copyErr := fs.Copy(ctx, srcPath, dstDir, len(favs) > i+1)
		if t != nil {
			if groupID != "" {
				t.SetGroupID(groupID)
			}
			addedTasks = append(addedTasks, t)
		}
		if copyErr != nil {
			common.ErrorResp(c, copyErr, http.StatusInternalServerError)
			return
		}
	}

	if len(addedTasks) == 0 {
		common.SuccessResp(c)
		return
	}
	if groupID != "" {
		common.SuccessResp(c, gin.H{
			"task": buildGroupTaskInfo[task.TaskInfoWithCreator](groupID, addedTasks, "favorite batch copy"),
		})
		return
	}
	common.SuccessResp(c, gin.H{
		"tasks": getTaskInfos(addedTasks),
	})
}

func BatchRemoveVideoFavorites(c *gin.Context) {
	var req VideoFavoriteBatchRemoveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResp(c, err, http.StatusBadRequest)
		return
	}
	if len(req.IDs) == 0 {
		common.ErrorStrResp(c, "ids required", http.StatusBadRequest)
		return
	}
	user := c.MustGet("user").(*model.User)
	if err := op.DeleteVideoFavoritesByIDs(user.ID, req.IDs); err != nil {
		common.ErrorResp(c, err, http.StatusInternalServerError)
		return
	}
	common.SuccessResp(c)
}

func CleanupVideoFavoriteByPath(c *gin.Context, user *model.User, reqPath string) {
	if user == nil || reqPath == "" {
		return
	}
	storage, _, err := op.GetStorageAndActualPath(reqPath)
	if err != nil || storage == nil || storage.GetStorage() == nil {
		return
	}
	_ = op.DeleteVideoFavorite(user.ID, storage.GetStorage().ID, reqPath)
}
