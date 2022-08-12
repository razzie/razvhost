package config

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"

	docker "github.com/fsouza/go-dockerclient"
)

type DockerWatch struct {
	client *docker.Client
}

// NewDockerWatch returns a new DockerWatch
func NewDockerWatch() (*DockerWatch, error) {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		return nil, err
	}

	d := &DockerWatch{
		client: client,
	}
	return d, nil
}

// GetActiveContainers returns up config events for active containers
func (d *DockerWatch) GetActiveContainers() ([]ConfigEvent, error) {
	containers, err := d.client.ListContainers(docker.ListContainersOptions{})
	if err != nil {
		return nil, err
	}

	var results []ConfigEvent
	for _, container := range containers {
		events, err := d.getContainerEvents(container.ID, true)
		if err != nil {
			log.Println(err)
			continue
		}
		results = append(results, events...)
	}
	return results, nil
}

// GetConfigEvents returns a channel of config events
func (d *DockerWatch) GetConfigEvents() (<-chan []ConfigEvent, error) {
	lis := make(chan *docker.APIEvents, 1)
	err := d.client.AddEventListener(lis)
	if err != nil {
		return nil, err
	}

	events := make(chan []ConfigEvent, 1)
	go d.convertEvents(lis, events)
	return events, nil
}

func (d *DockerWatch) convertEvents(in <-chan *docker.APIEvents, out chan<- []ConfigEvent) {
	for e := range in {
		if e.Type != "container" {
			continue
		}
		var events []ConfigEvent
		var err error
		switch e.Action {
		case "start":
			events, err = d.getContainerEvents(e.Actor.ID, true)
		case "stop":
			events, err = d.getContainerEvents(e.Actor.ID, false)
		}
		if err != nil {
			log.Println(err)
		}
		for _, event := range events {
			out <- []ConfigEvent{event}
		}
	}
	close(out)
}

func (d *DockerWatch) getContainerEvents(id string, start bool) ([]ConfigEvent, error) {
	cont, err := d.client.InspectContainerWithOptions(docker.InspectContainerOptions{
		Context: context.Background(),
		ID:      id,
	})
	if err != nil {
		return nil, err
	}

	env := docker.Env(cont.Config.Env)
	virtHost := env.Get("VIRTUAL_HOST")
	virtPort := env.Get("VIRTUAL_PORT")
	if len(virtHost) == 0 {
		return nil, nil
	}
	if len(virtPort) == 0 {
		virtPort = "8080"
	}

	var target *docker.PortBinding
	for port, bindings := range cont.NetworkSettings.Ports {
		if port.Port() == virtPort && len(bindings) > 0 {
			target = &bindings[0]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("no %q port bindings in container %q", virtPort, id)
	}
	targetURL, err := url.Parse("http://localhost:" + target.HostPort)
	if err != nil {
		return nil, err
	}

	var events []ConfigEvent
	for _, virtHost := range strings.Fields(virtHost) {
		events = append(events, ConfigEvent{
			ConfigEntry: ConfigEntry{
				Hostname: virtHost,
				Target:   *targetURL,
			},
			Up: start,
		})
	}

	return events, nil
}
