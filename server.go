package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/dukfaar/goUtils/env"
	"github.com/dukfaar/goUtils/eventbus"
	"github.com/gorilla/websocket"

	"github.com/graphql-go/graphql"

	"github.com/dukfaar/apiGateway/schema"
	dukGraphql "github.com/dukfaar/goUtils/graphql"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type ServiceProcessor struct {
	MergedSchemas schema.MergedSchemas
	CurrentSchema graphql.Schema

	ServiceChannel chan eventbus.ServiceInfo
}

func (p *ServiceProcessor) processResponse(serviceInfo eventbus.ServiceInfo, response schema.Response) {
	p.MergedSchemas.AddService(serviceInfo, response)

	newCurrentSchema, err := p.MergedSchemas.BuildSchema()

	if err != nil {
		fmt.Println(err)
		return
	}

	p.CurrentSchema = newCurrentSchema
}

func (p *ServiceProcessor) serviceUp(serviceInfo eventbus.ServiceInfo) {
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

	p.processResponse(serviceInfo, schemaResponse)
}

func (p *ServiceProcessor) StartChannelWatcher() {
	go func() {
		for {
			p.serviceUp(<-p.ServiceChannel)
		}
	}()
}

func NewServiceProcessor() *ServiceProcessor {
	var newProcessor = &ServiceProcessor{
		ServiceChannel: make(chan eventbus.ServiceInfo),
	}

	newProcessor.StartChannelWatcher()

	return newProcessor
}

func GetAuthValue(r *http.Request) string {
	authCookie, _ := r.Cookie("Authentication")
	if authCookie != nil {
		result, err := url.QueryUnescape(authCookie.Value)
		if err != nil {
			return ""
		}
		return result
	}

	authCookie, _ = r.Cookie("Authorization")
	if authCookie != nil {
		result, err := url.QueryUnescape(authCookie.Value)
		if err != nil {
			return ""
		}
		return result
	}

	authHeader := r.Header.Get("Authentication")
	if authHeader != "" {
		result, err := url.QueryUnescape(authHeader)
		if err != nil {
			return ""
		}
		return result
	}

	return ""
}

type SimpleResponse struct {
	Id   string `json:"id,omitempty"`
	Type string `json:"type,omitempty"`
}

type PayloadResponse struct {
	SimpleResponse
	Payload interface{} `json:"payload,omitempty"`
}

func send(r interface{}, connection *websocket.Conn, msgType int) error {
	responseJSON, err := json.Marshal(r)
	if err != nil {
		errorResponse, _ := json.Marshal(err)
		connection.WriteMessage(msgType, errorResponse)
		return err
	}

	if err = connection.WriteMessage(msgType, responseJSON); err != nil {
		errorResponse, _ := json.Marshal(err)
		connection.WriteMessage(msgType, errorResponse)
		return err
	}

	return nil
}

func (r *SimpleResponse) Send(connection *websocket.Conn, msgType int) error {
	return send(r, connection, msgType)
}

func (r *PayloadResponse) Send(connection *websocket.Conn, msgType int) error {
	return send(r, connection, msgType)
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

	newServiceProcessor := NewServiceProcessor()

	nsqEventbus.On("service.up", "apigateway_"+hostname, func(msg []byte) error {
		newService := eventbus.ServiceInfo{}
		err := json.Unmarshal(msg, &newService)
		if err != nil {
			fmt.Printf("Error unmarshalling serviceInfo: %+v\n", string(msg))
			return nil
		}

		if newService.Name != "apigateway" && len(newService.GraphQLHttpEndpoint) > 0 {
			newServiceProcessor.ServiceChannel <- newService
		}

		return nil
	})

	nsqEventbus.Emit("service.up", serviceInfo)

	//send a refreshing call every 5 minutes until i have solution in my infrastructure
	emissionTicker := time.NewTicker(time.Minute * 5)
	go func() {
		for {
			<-emissionTicker.C
			nsqEventbus.Emit("service.up", serviceInfo)
		}
	}()

	http.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		body, _ := ioutil.ReadAll(r.Body)
		defer r.Body.Close()

		var opts dukGraphql.Request
		err := json.Unmarshal(body, &opts)

		if err != nil {
			fmt.Printf("Error unmarshaling request; body:\"%v\"\n", string(body))
			return
		}

		ctx := context.Background()
		ctx = context.WithValue(ctx, "Authentication", GetAuthValue(r))

		params := graphql.Params{
			Schema:         newServiceProcessor.CurrentSchema,
			RequestString:  opts.Query,
			VariableValues: opts.Variables,
			OperationName:  opts.OperationName,
			Context:        ctx,
		}
		result := graphql.Do(params)
		w.Header().Add("Content-Type", "application/json; charset=utf-8")
		buff, _ := json.Marshal(result)

		w.Write(buff)
	})

	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	http.HandleFunc("/socket", func(w http.ResponseWriter, r *http.Request) {
		protocols := websocket.Subprotocols(r)
		var header http.Header = make(http.Header)
		header["Sec-WebSocket-Protocol"] = protocols
		connection, upgradeError := upgrader.Upgrade(w, r, header)

		if upgradeError != nil {
			log.Println(upgradeError)
			return
		}

		fmt.Println("opened new socket-connection")
		defer fmt.Println("closed socket-connection")

		ctx := context.Background()
		ctx = context.WithValue(ctx, "Authentication", GetAuthValue(r))

		for {
			msgType, message, err := connection.ReadMessage()

			if err != nil {
				fmt.Println(err)
				return
			}

			var socketRequest struct {
				Id      string          `json:"id,omitempty"`
				Type    string          `json:"type,omitempty"`
				Payload json.RawMessage `json:"payload,omitempty"`
			}

			if err = json.Unmarshal(message, &socketRequest); err != nil {
				errorResponse, _ := json.Marshal(err)
				connection.WriteMessage(msgType, errorResponse)
			}

			switch socketRequest.Type {
			case "connection_init":
				var connectionParams map[string]interface{}
				err := json.Unmarshal(socketRequest.Payload, &connectionParams)
				if err != nil {
					fmt.Printf("Error parsing payload %v: %v\n", string(socketRequest.Payload), err)
					continue
				}

				var authToken = connectionParams["Authentication"]
				if authToken != nil {
					ctx = context.WithValue(ctx, "Authentication", authToken.(string))
				}

				var socketResponse PayloadResponse
				socketResponse.Id = socketRequest.Id
				socketResponse.Type = "connection_ack"
				socketResponse.Payload = "ACK"
				socketResponse.Send(connection, msgType)
			case "connection_terminate":
				return
			case "start":
				var payload dukGraphql.Request
				err := json.Unmarshal(socketRequest.Payload, &payload)
				if err != nil {
					fmt.Printf("Error parsing payload %v: %v\n", string(socketRequest.Payload), err)
					continue
				}
				params := graphql.Params{
					Schema:         newServiceProcessor.CurrentSchema,
					RequestString:  payload.Query,
					VariableValues: payload.Variables,
					OperationName:  payload.OperationName,
					Context:        ctx,
				}
				var socketResponse PayloadResponse
				socketResponse.Id = socketRequest.Id
				socketResponse.Type = "data"
				socketResponse.Payload = graphql.Do(params)
				socketResponse.Send(connection, msgType)

				var completeResponse SimpleResponse
				completeResponse.Id = socketRequest.Id
				completeResponse.Type = "complete"
				completeResponse.Send(connection, msgType)
			case "stop":
				continue
			default:
				panic("Unknown socket-request-type: " + socketRequest.Type)
			}
		}
	})

	http.Handle("/metrics", promhttp.Handler())

	log.Fatal(http.ListenAndServe(":"+env.GetDefaultEnvVar("PORT", "8090"), nil))
}
