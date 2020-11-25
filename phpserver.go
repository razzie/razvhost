package razvhost

import (
	"net/http"
	"net/url"
	"os"

	"github.com/yookoala/gofast"
)

// PHPServer ...
type PHPServer struct {
	clientFactory gofast.ClientFactory
}

// NewPHPServer returns a new PHPServer
func NewPHPServer(cgiaddr string) (*PHPServer, error) {
	addr, err := url.Parse(cgiaddr)
	if err != nil {
		return nil, err
	}

	network := addr.Scheme
	address := addr.Host
	if len(address) == 0 {
		address = addr.Path
	}

	connFactory := gofast.SimpleConnFactory(network, address)

	return &PHPServer{
		clientFactory: gofast.SimpleClientFactory(connFactory, 0),
	}, nil
}

func (s *PHPServer) handler(endpoint string) (gofast.SessionHandler, error) {
	fi, err := os.Stat(endpoint)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return gofast.NewPHPFS(endpoint)(gofast.BasicSession), nil
	}
	return gofast.NewFileEndpoint(endpoint)(gofast.BasicSession), nil
}

// Handler returns a handler for a given endpoint
func (s *PHPServer) Handler(path, endpoint string) (http.Handler, error) {
	handler, err := s.handler(endpoint)
	if err != nil {
		return nil, err
	}
	return gofast.NewHandler(handler, s.clientFactory), nil
}
