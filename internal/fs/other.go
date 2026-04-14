package fs

import (
	"context"

	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/pkg/utils"
)

func makeDir(ctx context.Context, path string, lazyCache ...bool) error {
	storage, actualPath, err := resolveStoragePath(path)
	if err != nil {
		return err
	}
	return op.MakeDir(ctx, storage, actualPath, lazyCache...)
}

func move(ctx context.Context, srcPath, dstDirPath string, lazyCache ...bool) error {
	srcStorage, srcActualPath, err := resolveStoragePath(srcPath)
	if err != nil {
		return err
	}
	dstStorage, dstDirActualPath, err := resolveStoragePath(dstDirPath)
	if err != nil {
		return err
	}
	if isSameStorage(srcStorage, dstStorage) {
		return op.Move(ctx, srcStorage, srcActualPath, dstDirActualPath, lazyCache...)
	}
	switch resolveMoveStrategy(srcStorage, dstStorage) {
	case moveStrategyCrossLocalStorages:
		return moveBetweenLocalStorages(ctx, srcStorage, dstStorage, srcActualPath, dstDirActualPath, utils.IsBool(lazyCache...))
	}
	return runCrossStorageDirectly(ctx, srcStorage, dstStorage, srcActualPath, dstDirActualPath, FileOperationMove)
}

func rename(ctx context.Context, srcPath, dstName string, lazyCache ...bool) error {
	storage, srcActualPath, err := resolveStoragePath(srcPath)
	if err != nil {
		return err
	}
	return op.Rename(ctx, storage, srcActualPath, dstName, lazyCache...)
}

func remove(ctx context.Context, path string) error {
	storage, actualPath, err := resolveStoragePath(path)
	if err != nil {
		return err
	}
	return op.Remove(ctx, storage, actualPath)
}

func other(ctx context.Context, args model.FsOtherArgs) (interface{}, error) {
	storage, actualPath, err := resolveStoragePath(args.Path)
	if err != nil {
		return nil, err
	}
	args.Path = actualPath
	return op.Other(ctx, storage, args)
}
