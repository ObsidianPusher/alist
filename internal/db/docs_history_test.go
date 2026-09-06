package db

import (
	"testing"
	"time"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDocsHistoryPrunesOldestVersions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	conf.Conf = conf.DefaultConfig()
	conf.Conf.Database.Type = "sqlite3"
	Init(db)
	for i := 0; i < 3; i++ {
		err = CreateDocsHistory(&model.DocsHistory{
			Path:      "guide.md",
			Revision:  string(rune('a' + i)),
			CreatedAt: time.Unix(int64(i), 0),
		}, 2)
		if err != nil {
			t.Fatal(err)
		}
	}
	items, err := ListDocsHistory("guide.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Revision != "c" || items[1].Revision != "b" {
		t.Fatalf("unexpected retained history: %#v", items)
	}
}
