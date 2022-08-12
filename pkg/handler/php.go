package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/yookoala/gofast"
)

func setupPHP(cgiaddr *url.URL) gofast.ClientFactory {
	network := cgiaddr.Scheme
	address := cgiaddr.Host
	if len(address) == 0 {
		address = cgiaddr.Path
	}
	connFactory := gofast.SimpleConnFactory(network, address)
	return gofast.SimpleClientFactory(connFactory)
}

func newPHPHandler(phpClientFactory gofast.ClientFactory, hostname, hostPath, endpoint string) (http.Handler, error) {
	if phpClientFactory == nil {
		return nil, fmt.Errorf("PHP not configured")
	}
	fi, err := os.Stat(endpoint)
	if err != nil {
		return nil, err
	}
	endpoint, err = filepath.Abs(endpoint)
	if err != nil {
		return nil, err
	}
	var targetPath string
	var sessHandler gofast.SessionHandler
	if fi.IsDir() {
		sessHandler = gofast.NewPHPFS(endpoint)(gofast.BasicSession)
		targetPath = endpoint
	} else {
		sessHandler = gofast.NewFileEndpoint(endpoint)(gofast.BasicSession)
		targetPath = filepath.Dir(endpoint)
	}
	handler := gofast.NewHandler(sessHandler, phpClientFactory)
	return handlePathCombinations(handler, hostname, hostPath, targetPath), nil
}
