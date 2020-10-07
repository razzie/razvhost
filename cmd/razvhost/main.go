package main

import (
	"fmt"
	"time"
)

func main() {
	d, err := NewDockerWatch()
	if err != nil {
		panic(err)
	}

	for {
		time.Sleep(time.Second * 5)

		fmt.Println("entries:")
		fmt.Println("--------")
		for hostname, target := range d.GetProxies() {
			fmt.Println(hostname, "->", target)
		}
	}
}
