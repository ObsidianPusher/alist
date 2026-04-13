package crypt

import (
	stdpath "path"
	"strings"

	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/pkg/utils"
)

type cryptNameDecodeResult struct {
	Value string
}

// will give the best guessing based on the path
func guessPath(path string) (isFolder, secondTry bool) {
	if path == "" || path == "/" || strings.HasSuffix(path, "/") {
		//confirmed a folder
		return true, false
	}
	base := stdpath.Base(path)
	if base == "." || base == "/" || !strings.Contains(base, ".") {
		//no dot, try folder then try file
		return true, true
	}
	return false, true
}

func (d *Crypt) getPathForRemote(path string, isFolder bool) (remoteFullPath string) {
	if d == nil || d.cipher == nil {
		return ""
	}
	d.initCaches()
	path = utils.FixAndCleanPath(path)
	if isFolder && !strings.HasSuffix(path, "/") {
		path = path + "/"
	}
	cacheKey := "f:" + path
	if isFolder {
		cacheKey = "d:" + path
	}
	if d.pathCache != nil {
		if cached, ok := d.pathCache.Get(cacheKey); ok {
			return cached
		}
	}
	dir, fileName := stdpath.Split(path)

	remoteDir := d.encryptDirName(dir)
	remoteFileName := ""
	if len(strings.TrimSpace(fileName)) > 0 {
		remoteFileName = d.encryptFileName(fileName)
	}
	remoteFullPath = stdpath.Join(d.RemotePath, remoteDir, remoteFileName)
	if d.pathCache != nil {
		d.pathCache.Set(cacheKey, remoteFullPath, cryptCacheTTL)
	}
	return remoteFullPath

}

// actual path is used for internal only. any link for user should come from remoteFullPath
func (d *Crypt) getActualPathForRemote(path string, isFolder bool) (string, error) {
	_, remoteActualPath, err := op.GetStorageAndActualPath(d.getPathForRemote(path, isFolder))
	return remoteActualPath, err
}

func (d *Crypt) encryptDirName(name string) string {
	return d.encryptName("d:", name, d.cipher.EncryptDirName)
}

func (d *Crypt) encryptFileName(name string) string {
	return d.encryptName("f:", name, d.cipher.EncryptFileName)
}

func (d *Crypt) encryptName(prefix, name string, fn func(string) string) string {
	if d == nil || d.cipher == nil {
		return ""
	}
	d.initCaches()
	key := prefix + name
	if d.nameEncCache != nil {
		if cached, ok := d.nameEncCache.Get(key); ok {
			return cached
		}
	}
	encrypted := fn(name)
	if d.nameEncCache != nil {
		d.nameEncCache.Set(key, encrypted, cryptCacheTTL)
	}
	return encrypted
}

func (d *Crypt) decryptDirName(name string) (string, error) {
	return d.decryptName("d:", name, d.cipher.DecryptDirName)
}

func (d *Crypt) decryptFileName(name string) (string, error) {
	return d.decryptName("f:", name, d.cipher.DecryptFileName)
}

func (d *Crypt) decryptName(prefix, name string, fn func(string) (string, error)) (string, error) {
	if d == nil || d.cipher == nil {
		return "", nil
	}
	d.initCaches()
	key := prefix + name
	if d.nameDecCache != nil {
		if cached, ok := d.nameDecCache.Get(key); ok {
			return cached.Value, nil
		}
	}
	decrypted, err := fn(name)
	if err != nil {
		return "", err
	}
	if d.nameDecCache != nil {
		d.nameDecCache.Set(key, cryptNameDecodeResult{Value: decrypted}, cryptCacheTTL)
	}
	return decrypted, nil
}
