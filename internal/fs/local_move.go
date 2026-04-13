package fs

import (
	"context"
	"os"
	stdpath "path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/pkg/utils"
	cp "github.com/otiai10/copy"
	"github.com/pkg/errors"
)

const localDriverName = "Local"

type moveStrategy uint8

const (
	moveStrategyUnsupported moveStrategy = iota
	moveStrategySameLocalStorage
	moveStrategyCrossLocalStorages
)

func isLocalStorage(storage driver.Driver) bool {
	if storage == nil || storage.GetStorage() == nil {
		return false
	}
	return strings.EqualFold(storage.GetStorage().Driver, localDriverName)
}

func getLocalRoot(storage driver.Driver) (string, error) {
	rooter, ok := storage.GetAddition().(driver.IRootPath)
	if !ok {
		return "", errors.New("local storage root path not available")
	}
	root := rooter.GetRootPath()
	if root == "" {
		return "", errors.New("local storage root path is empty")
	}
	return root, nil
}

func isSameStorage(a, b driver.Driver) bool {
	if a == nil || b == nil || a.GetStorage() == nil || b.GetStorage() == nil {
		return false
	}
	if a.GetStorage().ID != 0 && b.GetStorage().ID != 0 {
		return a.GetStorage().ID == b.GetStorage().ID
	}
	if a.GetStorage().MountPath != "" && b.GetStorage().MountPath != "" {
		return utils.PathEqual(a.GetStorage().MountPath, b.GetStorage().MountPath)
	}
	return a.GetStorage() == b.GetStorage()
}

func resolveMoveStrategy(srcStorage, dstStorage driver.Driver) moveStrategy {
	if !isLocalStorage(srcStorage) || !isLocalStorage(dstStorage) {
		return moveStrategyUnsupported
	}
	if isSameStorage(srcStorage, dstStorage) {
		return moveStrategySameLocalStorage
	}
	return moveStrategyCrossLocalStorages
}

func moveBetweenLocalStorages(ctx context.Context, srcStorage, dstStorage driver.Driver, srcActualPath, dstDirActualPath string, lazyCache bool) error {
	if utils.IsCanceled(ctx) {
		return ctx.Err()
	}
	srcRoot, err := getLocalRoot(srcStorage)
	if err != nil {
		return err
	}
	dstRoot, err := getLocalRoot(dstStorage)
	if err != nil {
		return err
	}
	srcFull := filepath.Join(srcRoot, srcActualPath)
	dstDirFull := filepath.Join(dstRoot, dstDirActualPath)
	dstFull := filepath.Join(dstDirFull, stdpath.Base(srcActualPath))

	if utils.IsSubPath(filepath.ToSlash(srcFull), filepath.ToSlash(dstFull)) {
		return errors.New("the destination folder is a subfolder of the source folder")
	}

	err = os.Rename(srcFull, dstFull)
	if err != nil && (errors.Is(err, syscall.EXDEV) || strings.Contains(err.Error(), "invalid cross-device link")) {
		if utils.IsCanceled(ctx) {
			return ctx.Err()
		}
		err = cp.Copy(srcFull, dstFull, cp.Options{
			Sync:          true,
			PreserveTimes: true,
			PreserveOwner: true,
		})
		if err != nil {
			return err
		}
		err = os.RemoveAll(srcFull)
	}
	if err != nil {
		return err
	}

	if !lazyCache {
		srcDir := stdpath.Dir(srcActualPath)
		op.ClearCache(srcStorage, srcDir)
		op.ClearCache(dstStorage, dstDirActualPath)
	}
	return nil
}
