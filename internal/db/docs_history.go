package db

import (
	"github.com/alist-org/alist/v3/internal/model"
	"gorm.io/gorm"
)

func CreateDocsHistory(item *model.DocsHistory, limit int) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(item).Error; err != nil {
			return err
		}
		if limit <= 0 {
			return nil
		}
		var stale []model.DocsHistory
		if err := tx.Where("path = ?", item.Path).Order("created_at desc, id desc").Offset(limit).Find(&stale).Error; err != nil {
			return err
		}
		for i := range stale {
			if err := tx.Delete(&stale[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func ListDocsHistory(path string) ([]model.DocsHistory, error) {
	var items []model.DocsHistory
	err := db.Where("path = ?", path).Order("created_at desc, id desc").Find(&items).Error
	return items, err
}

func GetDocsHistory(path, revision string) (*model.DocsHistory, error) {
	var item model.DocsHistory
	if err := db.Where("path = ? AND revision = ?", path, revision).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}
