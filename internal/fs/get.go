package fs

import (
	"context"
	stdpath "path"

	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/pkg/utils"
)

func get(ctx context.Context, path string) (model.Obj, error) {
	path = utils.FixAndCleanPath(path)
	// maybe a virtual file
	if path != "/" {
		virtualFiles := op.GetStorageVirtualFilesByPath(stdpath.Dir(path))
		for _, f := range virtualFiles {
			if f.GetName() == stdpath.Base(path) {
				return f, nil
			}
		}
	}
	storage, actualPath, rootObj, err := resolveRootOrStoragePath(path)
	if err != nil {
		return nil, err
	}
	if rootObj != nil {
		return rootObj, nil
	}
	return op.Get(ctx, storage, actualPath)
}
