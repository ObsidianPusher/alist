package fs

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/stretchr/testify/require"
)

type fakeMoveStorageDriver struct {
	storage  model.Storage
	addition driver.Additional
}

func (d *fakeMoveStorageDriver) Config() driver.Config {
	return driver.Config{Name: d.storage.Driver}
}

func (d *fakeMoveStorageDriver) GetStorage() *model.Storage {
	return &d.storage
}

func (d *fakeMoveStorageDriver) SetStorage(storage model.Storage) {
	d.storage = storage
}

func (d *fakeMoveStorageDriver) GetAddition() driver.Additional {
	return d.addition
}

func (d *fakeMoveStorageDriver) Init(context.Context) error {
	return nil
}

func (d *fakeMoveStorageDriver) Drop(context.Context) error {
	return nil
}

func (d *fakeMoveStorageDriver) List(context.Context, model.Obj, model.ListArgs) ([]model.Obj, error) {
	return nil, nil
}

func (d *fakeMoveStorageDriver) Link(context.Context, model.Obj, model.LinkArgs) (*model.Link, error) {
	return nil, nil
}

func TestResolveMoveStrategy(t *testing.T) {
	t.Parallel()

	newLocal := func(id uint, mountPath, root string) driver.Driver {
		return &fakeMoveStorageDriver{
			storage: model.Storage{
				ID:        id,
				MountPath: mountPath,
				Driver:    "Local",
			},
			addition: driver.RootPath{RootFolderPath: root},
		}
	}
	newRemote := func(id uint, mountPath string) driver.Driver {
		return &fakeMoveStorageDriver{
			storage: model.Storage{
				ID:        id,
				MountPath: mountPath,
				Driver:    "Crypt",
			},
		}
	}

	require.Equal(t, moveStrategySameLocalStorage, resolveMoveStrategy(
		newLocal(1, "/local", "/tmp/local"),
		newLocal(1, "/local", "/tmp/local"),
	))
	require.Equal(t, moveStrategyCrossLocalStorages, resolveMoveStrategy(
		newLocal(1, "/local-a", "/tmp/local-a"),
		newLocal(2, "/local-b", "/tmp/local-b"),
	))
	require.Equal(t, moveStrategyUnsupported, resolveMoveStrategy(
		newLocal(1, "/local", "/tmp/local"),
		newRemote(2, "/remote"),
	))
	require.Equal(t, moveStrategyUnsupported, resolveMoveStrategy(
		newRemote(1, "/remote"),
		newRemote(1, "/remote"),
	))
}

func TestIsSameStorageSupportsRemoteStorage(t *testing.T) {
	t.Parallel()

	left := &fakeMoveStorageDriver{
		storage: model.Storage{
			ID:        10,
			MountPath: "/crypt",
			Driver:    "Crypt",
		},
	}
	right := &fakeMoveStorageDriver{
		storage: model.Storage{
			ID:        10,
			MountPath: "/crypt",
			Driver:    "Crypt",
		},
	}
	other := &fakeMoveStorageDriver{
		storage: model.Storage{
			ID:        11,
			MountPath: "/crypt-b",
			Driver:    "Crypt",
		},
	}

	require.True(t, isSameStorage(left, right))
	require.False(t, isSameStorage(left, other))
}

func TestMoveBetweenLocalStoragesSkipsCapacityCheckWhenRenameSucceeds(t *testing.T) {
	srcRoot := t.TempDir()
	dstRoot := t.TempDir()

	srcMount := "/src-" + t.Name()
	dstMount := "/dst-" + t.Name()

	require.NoError(t, os.MkdirAll(filepath.Join(srcRoot, "folder"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dstRoot, "target"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcRoot, "folder", "a.txt"), []byte("1234"), 0o644))

	srcStorageID, err := op.CreateStorage(context.Background(), model.Storage{
		MountPath: srcMount,
		Driver:    "Local",
		Addition:  `{"root_folder_path":"` + srcRoot + `"}`,
	})
	require.NoError(t, err)
	dstStorageID, err := op.CreateStorage(context.Background(), model.Storage{
		MountPath: dstMount,
		Driver:    "Local",
		Addition:  `{"root_folder_path":"` + dstRoot + `"}`,
	})
	require.NoError(t, err)
	require.NotZero(t, srcStorageID)
	require.NotZero(t, dstStorageID)
	srcStorage, err := op.GetStorageByMountPath(srcMount)
	require.NoError(t, err)
	dstStorage, err := op.GetStorageByMountPath(dstMount)
	require.NoError(t, err)

	originalDiskUsage := localDiskUsage
	localDiskUsage = func(path string) (*disk.UsageStat, error) {
		return &disk.UsageStat{Free: 1}, nil
	}
	defer func() {
		localDiskUsage = originalDiskUsage
	}()

	err = moveBetweenLocalStorages(context.Background(), srcStorage, dstStorage, "/folder", "/target", false)
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(srcRoot, "folder"))
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))
	data, err := os.ReadFile(filepath.Join(dstRoot, "target", "folder", "a.txt"))
	require.NoError(t, err)
	require.Equal(t, "1234", string(data))
}

func TestMoveBetweenLocalStoragesChecksDestinationFreeSpaceAfterCrossDeviceFallback(t *testing.T) {
	srcRoot := t.TempDir()
	dstRoot := t.TempDir()

	srcMount := "/src-" + t.Name()
	dstMount := "/dst-" + t.Name()

	require.NoError(t, os.MkdirAll(filepath.Join(srcRoot, "folder"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dstRoot, "target"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcRoot, "folder", "a.txt"), []byte("1234"), 0o644))

	srcStorageID, err := op.CreateStorage(context.Background(), model.Storage{
		MountPath: srcMount,
		Driver:    "Local",
		Addition:  `{"root_folder_path":"` + srcRoot + `"}`,
	})
	require.NoError(t, err)
	dstStorageID, err := op.CreateStorage(context.Background(), model.Storage{
		MountPath: dstMount,
		Driver:    "Local",
		Addition:  `{"root_folder_path":"` + dstRoot + `"}`,
	})
	require.NoError(t, err)
	require.NotZero(t, srcStorageID)
	require.NotZero(t, dstStorageID)
	srcStorage, err := op.GetStorageByMountPath(srcMount)
	require.NoError(t, err)
	dstStorage, err := op.GetStorageByMountPath(dstMount)
	require.NoError(t, err)

	originalDiskUsage := localDiskUsage
	originalRename := localFileRename
	localDiskUsage = func(path string) (*disk.UsageStat, error) {
		return &disk.UsageStat{Free: 1}, nil
	}
	localFileRename = func(oldpath, newpath string) error {
		return syscall.EXDEV
	}
	defer func() {
		localDiskUsage = originalDiskUsage
		localFileRename = originalRename
	}()

	err = moveBetweenLocalStorages(context.Background(), srcStorage, dstStorage, "/folder", "/target", false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient free space")
	_, err = os.Stat(filepath.Join(srcRoot, "folder", "a.txt"))
	require.NoError(t, err)
}
