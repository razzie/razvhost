package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	docker "github.com/fsouza/go-dockerclient"
)

// DockerWatch ...
type DockerWatch struct {
	mtx     sync.Mutex
	client  *docker.Client
	proxies map[string]string
}

// NewDockerWatch ...
func NewDockerWatch() (*DockerWatch, error) {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		return nil, err
	}

	lis := make(chan *docker.APIEvents, 1)
	err = client.AddEventListener(lis)
	if err != nil {
		return nil, err
	}

	d := &DockerWatch{
		client:  client,
		proxies: make(map[string]string),
	}
	go d.watch(lis)
	return d, nil
}

func (d *DockerWatch) watch(lis <-chan *docker.APIEvents) {
	for e := range lis {
		if e.Type == "container" {
			switch e.Action {
			case "start":
				_, err := d.handleContainer(e.Actor.ID, true)
				if err != nil {
					log.Println(err)
				}
			case "stop":
				_, err := d.handleContainer(e.Actor.ID, false)
				if err != nil {
					log.Println(err)
				}
			}
		}
	}
}

func (d *DockerWatch) handleContainer(id string, start bool) (bool, error) {
	cont, err := d.client.InspectContainerWithOptions(docker.InspectContainerOptions{
		Context: context.Background(),
		ID:      id,
	})
	if err != nil {
		return false, err
	}

	env := docker.Env(cont.Config.Env)
	virtHost := env.Get("VIRTUAL_HOST")
	virtPort := env.Get("VIRTUAL_PORT")
	if len(virtHost) == 0 {
		return false, nil
	}
	if len(virtPort) == 0 {
		virtPort = "8080"
	}

	if start {
		var target *docker.PortBinding
		for port, bindings := range cont.NetworkSettings.Ports {
			if port.Port() == virtPort && len(bindings) > 0 {
				target = &bindings[0]
				break
			}
		}
		if target == nil {
			return false, fmt.Errorf("no host port?")
		}

		for _, virtHost := range strings.Fields(virtHost) {
			d.addProxy(virtHost, fmt.Sprintf("http://localhost:%s", target.HostPort))
		}
	} else {
		for _, virtHost := range strings.Fields(virtHost) {
			d.removeProxy(virtHost)
		}
	}

	return true, nil
}

func (d *DockerWatch) addProxy(hostname, target string) {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	d.proxies[hostname] = target
}

func (d *DockerWatch) removeProxy(hostname string) {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	delete(d.proxies, hostname)
}

// GetProxy ...
func (d *DockerWatch) GetProxy(hostname string) (string, bool) {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	target, ok := d.proxies[hostname]
	return target, ok
}

// GetProxies ...
func (d *DockerWatch) GetProxies() map[string]string {
	proxies := make(map[string]string)
	d.mtx.Lock()
	defer d.mtx.Unlock()
	for hostname, target := range d.proxies {
		proxies[hostname] = target
	}
	return proxies
}
