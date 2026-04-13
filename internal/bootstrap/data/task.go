package data

import (
	"github.com/alist-org/alist/v3/internal/db"
	"github.com/alist-org/alist/v3/internal/model"
)

var initialTaskItems []model.TaskItem

func initTasks() {
	InitialTasks()

	for i := range initialTaskItems {
		item := &initialTaskItems[i]
		taskitem, _ := db.GetTaskDataByType(item.Key)
		if taskitem == nil {
			db.CreateTaskData(item)
		}
	}
}

func InitialTasks() []model.TaskItem {
	initialTaskItems = []model.TaskItem{
		{Key: "upload", PersistData: "[]"},
		{Key: "baidu_upload", PersistData: "[]"},
		{Key: "copy", PersistData: "[]"},
		{Key: "baidu_copy_download", PersistData: "[]"},
		{Key: "baidu_copy_upload", PersistData: "[]"},
		{Key: "baidu_copy", PersistData: "[]"},
		{Key: "download", PersistData: "[]"},
		{Key: "transfer", PersistData: "[]"},
		{Key: "local_browser_extract", PersistData: "[]"},
	}
	return initialTaskItems
}
