package main

import (
	"fmt"

	"github.com/docker-demo/myplugin/bridgedriver"
	"github.com/docker/go-plugins-helpers/network"
)

const (
	networkPluginName = "myplugin"
)

func main() {
	errChannel := make(chan error)
	networkHandler := network.NewHandler(bridgedriver.NewNetworkDriver())

	go func(c chan error) {
		fmt.Printf("%s is starting", networkPluginName)
		err := networkHandler.ServeUnix(networkPluginName, 0)
		fmt.Printf("%s has stopped", networkPluginName)
		c <- err
	}(errChannel)

	_ = <-errChannel
}
