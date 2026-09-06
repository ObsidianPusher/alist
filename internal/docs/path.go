package docs

import (
	"fmt"
	"path"
	"strings"
)

var allowedExtensions = map[string]struct{}{".md": {}}

func ResolvePath(root, relative string) (string, error) {
	if root == "" || !path.IsAbs(root) || relative == "" || path.IsAbs(relative) {
		return "", fmt.Errorf("invalid document path")
	}
	root = path.Clean(root)
	resolved := path.Clean(path.Join(root, relative))
	if resolved == root || !strings.HasPrefix(resolved, root+"/") {
		return "", fmt.Errorf("document path escapes root")
	}
	if _, ok := allowedExtensions[path.Ext(resolved)]; !ok {
		return "", fmt.Errorf("unsupported document type")
	}
	return resolved, nil
}
