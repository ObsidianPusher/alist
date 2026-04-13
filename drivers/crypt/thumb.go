package crypt

import (
	"context"

	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
)

func (d *Crypt) Thumb(ctx context.Context, obj model.Obj, args model.LinkArgs) (string, error) {
	if obj == nil || obj.IsDir() {
		return "", nil
	}
	remoteActualPath, err := d.getActualPathForRemote(obj.GetPath(), obj.IsDir())
	if err != nil {
		return "", err
	}
	remoteObj, err := op.Get(ctx, d.remoteStorage, remoteActualPath)
	if err != nil {
		return "", err
	}
	if t, ok := model.GetThumb(remoteObj); ok && t != "" {
		return t, nil
	}
	if tp, ok := d.remoteStorage.(driver.ThumbProvider); ok {
		return tp.Thumb(ctx, remoteObj, args)
	}
	return "", nil
}
