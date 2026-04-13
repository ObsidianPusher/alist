package op

import (
	stdpath "path"
	"time"

	"github.com/alist-org/alist/v3/internal/db"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/pkg/cachex"
	"github.com/alist-org/alist/v3/pkg/singleflight"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

const metaCacheMaxEntries = 16384

var metaCache = cachex.NewBoundedTTLCache[*model.Meta](metaCacheMaxEntries, 8)

const (
	metaPositiveCacheTTL = time.Hour
	metaNegativeCacheTTL = 5 * time.Minute
)

// metaG maybe not needed
var metaG singleflight.Group[*model.Meta]

func init() {
	RegisterMemoryPressureHook(func(snapshot MemoryPressureSnapshot) {
		switch snapshot.Level {
		case MemoryPressureCritical:
			metaCache.Clear()
		case MemoryPressureHigh:
			metaCache.TrimTo(metaCacheMaxEntries / 4)
		case MemoryPressureModerate:
			metaCache.TrimTo(metaCacheMaxEntries / 2)
		}
	})
}

func GetNearestMeta(path string) (*model.Meta, error) {
	return getNearestMeta(utils.FixAndCleanPath(path))
}
func getNearestMeta(path string) (*model.Meta, error) {
	meta, err := GetMetaByPath(path)
	if err == nil {
		return meta, nil
	}
	if errors.Cause(err) != errs.MetaNotFound {
		return nil, err
	}
	if path == "/" {
		return nil, errs.MetaNotFound
	}
	return getNearestMeta(stdpath.Dir(path))
}

func GetMetaByPath(path string) (*model.Meta, error) {
	return getMetaByPath(utils.FixAndCleanPath(path))
}
func getMetaByPath(path string) (*model.Meta, error) {
	meta, ok := metaCache.Get(path)
	if ok {
		if meta == nil {
			return meta, errs.MetaNotFound
		}
		return meta, nil
	}
	meta, err, _ := metaG.Do(path, func() (*model.Meta, error) {
		_meta, err := db.GetMetaByPath(path)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// Cache "not found" briefly to avoid repeated DB hits on hot-miss paths.
				metaCache.Set(path, nil, metaNegativeCacheTTL)
				return nil, errs.MetaNotFound
			}
			return nil, err
		}
		metaCache.Set(path, _meta, metaPositiveCacheTTL)
		return _meta, nil
	})
	return meta, err
}

func DeleteMetaById(id uint) error {
	old, err := db.GetMetaById(id)
	if err != nil {
		return err
	}
	metaCache.Del(old.Path)
	return db.DeleteMetaById(id)
}

func UpdateMeta(u *model.Meta) error {
	u.Path = utils.FixAndCleanPath(u.Path)
	old, err := db.GetMetaById(u.ID)
	if err != nil {
		return err
	}
	metaCache.Del(old.Path)
	return db.UpdateMeta(u)
}

func CreateMeta(u *model.Meta) error {
	u.Path = utils.FixAndCleanPath(u.Path)
	metaCache.Del(u.Path)
	return db.CreateMeta(u)
}

func GetMetaById(id uint) (*model.Meta, error) {
	return db.GetMetaById(id)
}

func GetMetas(pageIndex, pageSize int) (metas []model.Meta, count int64, err error) {
	return db.GetMetas(pageIndex, pageSize)
}
