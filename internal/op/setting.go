package op

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alist-org/alist/v3/internal/db"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/pkg/cachex"
	"github.com/alist-org/alist/v3/pkg/singleflight"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/pkg/errors"
)

const (
	settingCacheTTL          = time.Hour
	settingItemCacheMax      = 2048
	settingGroupCacheMax     = 512
	allSettingItemsCacheKey  = "ALL_SETTING_ITEMS"
	allPublicSettingCacheKey = "ALL_PUBLIC_SETTING_ITEMS"
)

var settingCache = cachex.NewBoundedTTLCache[*model.SettingItem](settingItemCacheMax, 8)
var settingG singleflight.Group[*model.SettingItem]
var settingCacheF = func(item *model.SettingItem) {
	settingCache.Set(item.Key, item, settingCacheTTL)
}

var settingGroupCache = cachex.NewBoundedTTLCache[[]model.SettingItem](settingGroupCacheMax, 8)
var settingGroupG singleflight.Group[[]model.SettingItem]
var settingGroupCacheF = func(key string, item []model.SettingItem) {
	settingGroupCache.Set(key, item, settingCacheTTL)
}

func init() {
	RegisterMemoryPressureHook(func(snapshot MemoryPressureSnapshot) {
		switch snapshot.Level {
		case MemoryPressureCritical:
			settingCache.Clear()
			settingGroupCache.Clear()
		case MemoryPressureHigh:
			settingCache.TrimTo(settingItemCacheMax / 4)
			settingGroupCache.TrimTo(settingGroupCacheMax / 4)
		case MemoryPressureModerate:
			settingCache.TrimTo(settingItemCacheMax / 2)
			settingGroupCache.TrimTo(settingGroupCacheMax / 2)
		}
	})
}

func settingCacheUpdate() {
	settingCache.Clear()
	settingGroupCache.Clear()
}

func GetPublicSettingsMap() map[string]string {
	items, _ := GetPublicSettingItems()
	pSettings := make(map[string]string, len(items))
	for _, item := range items {
		pSettings[item.Key] = item.Value
	}
	return pSettings
}

func GetSettingsMap() map[string]string {
	items, _ := GetSettingItems()
	settings := make(map[string]string, len(items))
	for _, item := range items {
		settings[item.Key] = item.Value
	}
	return settings
}

func GetSettingItems() ([]model.SettingItem, error) {
	if items, ok := settingGroupCache.Get(allSettingItemsCacheKey); ok {
		return items, nil
	}
	items, err, _ := settingGroupG.Do(allSettingItemsCacheKey, func() ([]model.SettingItem, error) {
		_items, err := db.GetSettingItems()
		if err != nil {
			return nil, err
		}
		settingGroupCacheF(allSettingItemsCacheKey, _items)
		return _items, nil
	})
	return items, err
}

func GetPublicSettingItems() ([]model.SettingItem, error) {
	if items, ok := settingGroupCache.Get(allPublicSettingCacheKey); ok {
		return items, nil
	}
	items, err, _ := settingGroupG.Do(allPublicSettingCacheKey, func() ([]model.SettingItem, error) {
		_items, err := db.GetPublicSettingItems()
		if err != nil {
			return nil, err
		}
		settingGroupCacheF(allPublicSettingCacheKey, _items)
		return _items, nil
	})
	return items, err
}

func GetSettingItemByKey(key string) (*model.SettingItem, error) {
	if item, ok := settingCache.Get(key); ok {
		return item, nil
	}

	item, err, _ := settingG.Do(key, func() (*model.SettingItem, error) {
		_item, err := db.GetSettingItemByKey(key)
		if err != nil {
			return nil, err
		}
		settingCacheF(_item)
		return _item, nil
	})
	return item, err
}

func GetSettingItemInKeys(keys []string) ([]model.SettingItem, error) {
	items := make([]model.SettingItem, 0, len(keys))
	for _, key := range keys {
		item, err := GetSettingItemByKey(key)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func GetSettingItemsByGroup(group int) ([]model.SettingItem, error) {
	key := strconv.Itoa(group)
	if items, ok := settingGroupCache.Get(key); ok {
		return items, nil
	}
	items, err, _ := settingGroupG.Do(key, func() ([]model.SettingItem, error) {
		_items, err := db.GetSettingItemsByGroup(group)
		if err != nil {
			return nil, err
		}
		settingGroupCacheF(key, _items)
		return _items, nil
	})
	return items, err
}

func GetSettingItemsInGroups(groups []int) ([]model.SettingItem, error) {
	sortedGroups := append([]int(nil), groups...)
	sort.Ints(sortedGroups)
	key := strings.Join(utils.MustSliceConvert(sortedGroups, func(i int) string {
		return strconv.Itoa(i)
	}), ",")

	if items, ok := settingGroupCache.Get(key); ok {
		return items, nil
	}
	items, err, _ := settingGroupG.Do(key, func() ([]model.SettingItem, error) {
		_items, err := db.GetSettingItemsInGroups(sortedGroups)
		if err != nil {
			return nil, err
		}
		settingGroupCacheF(key, _items)
		return _items, nil
	})
	return items, err
}

func SaveSettingItems(items []model.SettingItem) error {
	noHookItems := make([]model.SettingItem, 0)
	errs := make([]error, 0)
	for i := range items {
		if ok, err := HandleSettingItemHook(&items[i]); ok {
			if err != nil {
				errs = append(errs, err)
			} else {
				err = db.SaveSettingItem(&items[i])
				if err != nil {
					errs = append(errs, err)
				}
			}
		} else {
			noHookItems = append(noHookItems, items[i])
		}
	}
	if len(noHookItems) > 0 {
		err := db.SaveSettingItems(noHookItems)
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) < len(items)-len(noHookItems)+1 {
		settingCacheUpdate()
	}
	return utils.MergeErrors(errs...)
}

func SaveSettingItem(item *model.SettingItem) (err error) {
	// hook
	if _, err := HandleSettingItemHook(item); err != nil {
		return err
	}
	// update
	if err = db.SaveSettingItem(item); err != nil {
		return err
	}
	settingCacheUpdate()
	return nil
}

func DeleteSettingItemByKey(key string) error {
	old, err := GetSettingItemByKey(key)
	if err != nil {
		return errors.WithMessage(err, "failed to get settingItem")
	}
	if !old.IsDeprecated() {
		return errors.Errorf("setting [%s] is not deprecated", key)
	}
	settingCacheUpdate()
	return db.DeleteSettingItemByKey(key)
}
