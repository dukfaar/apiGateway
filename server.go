package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/dukfaar/goUtils/env"
	"github.com/dukfaar/goUtils/eventbus"

	"github.com/graphql-go/graphql"

	"github.com/dukfaar/apiGateway/schema"
)

var mergedSchemas schema.MergedSchemas
var currentSchema graphql.Schema

func ProcessServiceUp(serviceInfo eventbus.ServiceInfo) {
	jsonValue, _ := json.Marshal(schema.Request{
		Query: IntrospectionQuery,
	})
	resp, err := http.Post("http://"+serviceInfo.Hostname+":"+serviceInfo.Port+serviceInfo.GraphQLHttpEndpoint, "application/json", bytes.NewBuffer(jsonValue))

	if err != nil {
		panic(err)
	}

	defer resp.Body.Close()

	var schemaResponse schema.Response
	json.NewDecoder(resp.Body).Decode(&schemaResponse)

	mergedSchemas.AddService(serviceInfo, schemaResponse)

	currentSchema, err = mergedSchemas.BuildSchema()

	if err != nil {
		panic(err)
	}
}

func NewServiceProcessor() chan eventbus.ServiceInfo {
	newServiceChannel := make(chan eventbus.ServiceInfo)

	go func() {
		for {
			ProcessServiceUp(<-newServiceChannel)
		}
	}()

	return newServiceChannel
}

func main() {
	nsqEventbus := eventbus.NewNsqEventBus(env.GetDefaultEnvVar("NSQD_TCP_URL", "localhost:4150"), env.GetDefaultEnvVar("NSQLOOKUP_HTTP_URL", "localhost:4161"))

	serviceInfo := eventbus.ServiceInfo{
		Name:     "apigateway",
		Hostname: env.GetDefaultEnvVar("PUBLISHED_HOSTNAME", "apigateway"),
		Port:     env.GetDefaultEnvVar("PUBLISHED_PORT", "8080"),
	}

	hostname, _ := os.Hostname()

	newServiceChannel := NewServiceProcessor()

	nsqEventbus.On("service.up", "apigateway_"+hostname, func(msg []byte) error {
		newService := eventbus.ServiceInfo{}
		json.Unmarshal(msg, &newService)

		if newService.Name != "apigateway" && len(newService.GraphQLHttpEndpoint) > 0 {
			newServiceChannel <- newService
		}

		return nil
	})

	nsqEventbus.Emit("service.up", serviceInfo)

	http.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		body, _ := ioutil.ReadAll(r.Body)
		defer r.Body.Close()

		var opts schema.Request
		err := json.Unmarshal(body, &opts)

		if err != nil {
			panic(err)
		}

		params := graphql.Params{
			Schema:         currentSchema,
			RequestString:  opts.Query,
			VariableValues: opts.Variables,
		}
		result := graphql.Do(params)

		w.Header().Add("Content-Type", "application/json; charset=utf-8")
		buff, _ := json.Marshal(result)
		w.Write(buff)
	})

	log.Fatal(http.ListenAndServe(":"+env.GetDefaultEnvVar("PORT", "8090"), nil))
}
