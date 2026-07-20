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
	"github.com/shirou/gopsutil/v3/disk"
)

const localDriverName = "Local"

var localDiskUsage = disk.Usage

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

func ensureCrossStorageMoveAllowed(ctx context.Context, srcStorage, dstStorage driver.Driver, srcActualPath, dstDirActualPath string) error {
	if resolveMoveStrategy(srcStorage, dstStorage) != moveStrategyCrossLocalStorages {
		return errors.New("cross-storage move only supports local storages")
	}
	requiredBytes, err := estimateMoveRequiredBytes(ctx, srcStorage, srcActualPath)
	if err != nil {
		return err
	}
	return ensureLocalMoveCapacity(dstStorage, dstDirActualPath, requiredBytes)
}

func estimateMoveRequiredBytes(ctx context.Context, srcStorage driver.Driver, srcActualPath string) (int64, error) {
	srcObj, err := op.Get(ctx, srcStorage, srcActualPath)
	if err != nil {
		return 0, errors.WithMessagef(err, "failed get src [%s] file", srcActualPath)
	}
	return estimateObjBytes(ctx, srcStorage, srcObj, srcActualPath)
}

func ensureLocalMoveCapacity(dstStorage driver.Driver, dstDirActualPath string, requiredBytes int64) error {
	if requiredBytes <= 0 {
		return nil
	}
	dstRoot, err := getLocalRoot(dstStorage)
	if err != nil {
		return err
	}
	usagePath, err := findExistingLocalUsagePath(dstRoot, dstDirActualPath)
	if err != nil {
		return err
	}
	usage, err := localDiskUsage(usagePath)
	if err != nil {
		return err
	}
	if usage == nil {
		return errors.New("failed to get destination disk usage")
	}
	if usage.Free < uint64(requiredBytes) {
		return errors.Errorf("insufficient free space in destination local storage: need %d bytes, have %d bytes", requiredBytes, usage.Free)
	}
	return nil
}

func findExistingLocalUsagePath(root, actualPath string) (string, error) {
	cur := filepath.Clean(filepath.Join(root, actualPath))
	root = filepath.Clean(root)
	for {
		if _, err := os.Stat(cur); err == nil {
			return cur, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		if cur == root {
			return root, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return root, nil
		}
		cur = parent
	}
}
