// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webdav

import (
	"context"
	stderrors "errors"
	"net/http"
	"path"
	"path/filepath"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
)

var errDestinationExists = stderrors.New("destination exists")

// slashClean is equivalent to but slightly more efficient than
// path.Clean("/" + name).
func slashClean(name string) string {
	if name == "" || name[0] != '/' {
		name = "/" + name
	}
	return path.Clean(name)
}

func ensureDestination(ctx context.Context, dst string, overwrite bool) error {
	_, err := fs.Get(ctx, dst, &fs.GetArgs{NoLog: true})
	if err == nil {
		if !overwrite {
			return errDestinationExists
		}
		if err := fs.Remove(ctx, dst); err != nil {
			return err
		}
		return nil
	}
	if errs.IsObjectNotFound(err) {
		return nil
	}
	return err
}

func ensureDestinationInStorage(ctx context.Context, storage driver.Driver, dstActualPath string, overwrite bool) error {
	_, err := op.Get(ctx, storage, dstActualPath)
	if err == nil {
		if !overwrite {
			return errDestinationExists
		}
		if err := op.Remove(ctx, storage, dstActualPath); err != nil {
			return err
		}
		return nil
	}
	if errs.IsObjectNotFound(err) {
		return nil
	}
	return err
}

func sameStorage(a, b driver.Driver) bool {
	if a == nil || b == nil || a.GetStorage() == nil || b.GetStorage() == nil {
		return false
	}
	if a.GetStorage().ID != 0 && b.GetStorage().ID != 0 {
		return a.GetStorage().ID == b.GetStorage().ID
	}
	return a.GetStorage().MountPath == b.GetStorage().MountPath
}

func cloneObjWithName(obj model.Obj, name string) model.Obj {
	if obj == nil {
		return nil
	}
	if raw := model.GetRawObject(obj); raw != nil {
		cloned := *raw
		cloned.Name = name
		return &cloned
	}
	return &model.Object{
		Path:     obj.GetPath(),
		Name:     name,
		Size:     obj.GetSize(),
		Modified: obj.ModTime(),
		Ctime:    obj.CreateTime(),
		IsFolder: obj.IsDir(),
		HashInfo: obj.GetHash(),
	}
}

func copyOrMoveWithinStorage(ctx context.Context, storage driver.Driver, srcActualPath, dstActualPath string, move bool, overwrite bool) (int, error) {
	srcObj, err := op.Get(ctx, storage, srcActualPath)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	dstDirActualPath := path.Dir(dstActualPath)
	dstName := path.Base(dstActualPath)
	if err := ensureDestinationInStorage(ctx, storage, dstActualPath, overwrite); err != nil {
		if stderrors.Is(err, errDestinationExists) {
			return http.StatusPreconditionFailed, err
		}
		return http.StatusInternalServerError, err
	}
	dstDirObj, err := op.GetUnwrap(ctx, storage, dstDirActualPath)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	srcObj = cloneObjWithName(model.UnwrapObj(srcObj), dstName)
	if move {
		switch s := storage.(type) {
		case driver.MoveResult:
			_, err = s.Move(ctx, srcObj, dstDirObj)
		case driver.Move:
			err = s.Move(ctx, srcObj, dstDirObj)
		default:
			err = errs.NotImplement
		}
	} else {
		switch s := storage.(type) {
		case driver.CopyResult:
			_, err = s.Copy(ctx, srcObj, dstDirObj)
		case driver.Copy:
			err = s.Copy(ctx, srcObj, dstDirObj)
		default:
			err = errs.NotImplement
		}
	}
	if err != nil {
		return http.StatusInternalServerError, err
	}
	return http.StatusCreated, nil
}

// moveFiles moves files and/or directories from src to dst.
//
// See section 9.9.4 for when various HTTP status codes apply.
func moveFiles(ctx context.Context, src, dst string, overwrite bool) (status int, err error) {
	if slashClean(src) == slashClean(dst) {
		return http.StatusNoContent, nil
	}
	srcDir := path.Dir(src)
	dstDir := path.Dir(dst)
	srcName := path.Base(src)
	dstName := path.Base(dst)
	srcStorage, srcActualPath, err := op.GetStorageAndActualPath(src)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	dstStorage, dstActualPath, err := op.GetStorageAndActualPath(dst)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	if sameStorage(srcStorage, dstStorage) {
		return copyOrMoveWithinStorage(ctx, srcStorage, srcActualPath, dstActualPath, true, overwrite)
	}
	if err := ensureDestination(ctx, dst, overwrite); err != nil {
		if stderrors.Is(err, errDestinationExists) {
			return http.StatusPreconditionFailed, err
		}
		return http.StatusInternalServerError, err
	}
	if srcDir == dstDir {
		err = fs.Rename(ctx, src, dstName)
	} else {
		err = fs.Move(ctx, src, dstDir)
		if err != nil {
			return http.StatusInternalServerError, err
		}
		if srcName != dstName {
			err = fs.Rename(ctx, path.Join(dstDir, srcName), dstName)
		}
	}
	if err != nil {
		return http.StatusInternalServerError, err
	}
	return http.StatusCreated, nil
}

// copyFiles copies files and/or directories from src to dst.
//
// See section 9.8.5 for when various HTTP status codes apply.
func copyFiles(ctx context.Context, src, dst string, overwrite bool) (status int, err error) {
	if slashClean(src) == slashClean(dst) {
		return http.StatusNoContent, nil
	}
	srcStorage, srcActualPath, err := op.GetStorageAndActualPath(src)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	dstStorage, dstActualPath, err := op.GetStorageAndActualPath(dst)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	if sameStorage(srcStorage, dstStorage) {
		return copyOrMoveWithinStorage(ctx, srcStorage, srcActualPath, dstActualPath, false, overwrite)
	}
	if err := ensureDestination(ctx, dst, overwrite); err != nil {
		if stderrors.Is(err, errDestinationExists) {
			return http.StatusPreconditionFailed, err
		}
		return http.StatusInternalServerError, err
	}
	dstDir := path.Dir(dst)
	copyCtx := context.WithValue(ctx, conf.NoTaskKey, struct{}{})
	_, err = fs.Copy(copyCtx, src, dstDir)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	if path.Base(src) != path.Base(dst) {
		if err := fs.Rename(copyCtx, path.Join(dstDir, path.Base(src)), path.Base(dst)); err != nil {
			return http.StatusInternalServerError, err
		}
	}
	return http.StatusCreated, nil
}

// walkFS traverses filesystem fs starting at name up to depth levels.
//
// Allowed values for depth are 0, 1 or infiniteDepth. For each visited node,
// walkFS calls walkFn. If a visited file system node is a directory and
// walkFn returns path.SkipDir, walkFS will skip traversal of this node.
func walkFS(ctx context.Context, depth int, name string, info model.Obj, walkFn func(reqPath string, info model.Obj, err error) error) error {
	// This implementation is based on Walk's code in the standard path/path package.
	err := walkFn(name, info, nil)
	if err != nil {
		if info.IsDir() && err == filepath.SkipDir {
			return nil
		}
		return err
	}
	if !info.IsDir() || depth == 0 {
		return nil
	}
	if depth == 1 {
		depth = 0
	}
	meta, _ := op.GetNearestMeta(name)
	// Read directory names.
	objs, err := fs.List(context.WithValue(ctx, "meta", meta), name, &fs.ListArgs{})
	//f, err := fs.OpenFile(ctx, name, os.O_RDONLY, 0)
	//if err != nil {
	//	return walkFn(name, info, err)
	//}
	//fileInfos, err := f.Readdir(0)
	//f.Close()
	if err != nil {
		return walkFn(name, info, err)
	}

	for _, fileInfo := range objs {
		filename := path.Join(name, fileInfo.GetName())
		if err != nil {
			if err := walkFn(filename, fileInfo, err); err != nil && err != filepath.SkipDir {
				return err
			}
		} else {
			err = walkFS(ctx, depth, filename, fileInfo, walkFn)
			if err != nil {
				if !fileInfo.IsDir() || err != filepath.SkipDir {
					return err
				}
			}
		}
	}
	return nil
}
