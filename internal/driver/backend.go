package driver

// BackendIdentifier identifies storages that share the same backend/account.
// Drivers can implement this to enable optimized cross-mount operations.
type BackendIdentifier interface {
	BackendID() string
}

// BackendPathResolver maps a storage-relative path to a backend-absolute path.
// Drivers should implement this when cross-mount operations require absolute paths.
type BackendPathResolver interface {
	BackendPath(path string) string
}
