package main

import (
	"fmt"
	docker "github.com/fsouza/go-dockerclient"
)

func main() {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		panic(err)
	}

	lis := make(chan *docker.APIEvents, 1)
	err = client.AddEventListener(lis)
	if err != nil {
		panic(err)
	}

	for e := range lis {
		fmt.Println(e)
	}

	fmt.Println("end")
}
