package razvhost

// ProxyList ...
type ProxyList interface {
	GetProxy(hostname string) (string, bool)
	GetProxies() map[string]string
}
