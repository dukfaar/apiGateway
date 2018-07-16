package main

import (
	"encoding/json"
	"os"

	"github.com/dukfaar/goUtils/env"
	"github.com/dukfaar/goUtils/eventbus"
)

func main() {
	endChan := make(chan int)

	nsqEventbus := eventbus.NewNsqEventBus(env.GetDefaultEnvVar("NSQD_TCP_URL", "localhost:4150"), env.GetDefaultEnvVar("NSQLOOKUP_HTTP_URL", "localhost:4161"))

	serviceInfo := eventbus.ServiceInfo{
		Name:     "apigateway",
		Hostname: env.GetDefaultEnvVar("PUBLISHED_HOSTNAME", "apigateway"),
		Port:     env.GetDefaultEnvVar("PUBLISHED_PORT", "8080"),
	}

	hostname, _ := os.Hostname()

	nsqEventbus.On("service.up", "apigateway_"+hostname, func(msg []byte) error {
		newService := eventbus.ServiceInfo{}
		json.Unmarshal(msg, &newService)

		if len(newService.GraphQLHttpEndpoint) > 0 {

		}

		return nil
	})

	nsqEventbus.Emit("service.up", serviceInfo)

	os.Exit(<-endChan)
}
