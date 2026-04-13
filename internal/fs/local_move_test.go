package fs

import (
	"context"
	"testing"

	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/model"
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
