package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/dukfaar/goUtils/env"
	"github.com/dukfaar/goUtils/eventbus"

	"github.com/graphql-go/graphql"

	"github.com/dukfaar/apiGateway/schema"
	dukGraphql "github.com/dukfaar/goUtils/graphql"
)

var mergedSchemas schema.MergedSchemas
var currentSchema graphql.Schema

func ProcessServiceUp(serviceInfo eventbus.ServiceInfo) {
	jsonValue, _ := json.Marshal(dukGraphql.Request{
		Query: IntrospectionQuery,
	})
	resp, err := http.Post("http://"+serviceInfo.Hostname+":"+serviceInfo.Port+serviceInfo.GraphQLHttpEndpoint, "application/json", bytes.NewBuffer(jsonValue))

	if err != nil {
		fmt.Println(err)
		return
	}

	defer resp.Body.Close()

	var schemaResponse schema.Response
	json.NewDecoder(resp.Body).Decode(&schemaResponse)

	mergedSchemas.AddService(serviceInfo, schemaResponse)

	newCurrentSchema, err := mergedSchemas.BuildSchema()

	if err != nil {
		fmt.Println(err)
		return
	}

	currentSchema = newCurrentSchema
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
		Name:                "apigateway",
		Hostname:            env.GetDefaultEnvVar("PUBLISHED_HOSTNAME", "apigateway"),
		Port:                env.GetDefaultEnvVar("PUBLISHED_PORT", "8080"),
		GraphQLHttpEndpoint: "/graphql",
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

		var opts dukGraphql.Request
		err := json.Unmarshal(body, &opts)

		if err != nil {
			panic(err)
		}

		var authValue string
		authCookie, _ := r.Cookie("Authentication")

		ctx := context.Background()
		if authCookie != nil {
			authValue = authCookie.Value
		} else {
			authHeader := r.Header.Get("Authentication")

			if authHeader != "" {
				authValue = authHeader
			}
		}
		ctx = context.WithValue(ctx, "Authentication", authValue)

		params := graphql.Params{
			Schema:         currentSchema,
			RequestString:  opts.Query,
			VariableValues: opts.Variables,
			Context:        ctx,
		}
		result := graphql.Do(params)

		w.Header().Add("Content-Type", "application/json; charset=utf-8")
		buff, _ := json.Marshal(result)
		w.Write(buff)
	})

	log.Fatal(http.ListenAndServe(":"+env.GetDefaultEnvVar("PORT", "8090"), nil))
}
