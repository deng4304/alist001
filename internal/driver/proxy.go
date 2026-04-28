package driver

// ProxyDriver lets a driver override the default "must proxy" download behavior
// on a per-storage basis.
type ProxyDriver interface {
	ShouldProxyDownloads() bool
}
