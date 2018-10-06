package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	dukGraphql "github.com/dukfaar/goUtils/graphql"
	"github.com/gorilla/websocket"
	"github.com/graphql-go/graphql"
)

type SocketConnection struct {
	ctx            context.Context
	connection     *websocket.Conn
	schemaProvider SchemaProvider
	closed         bool
}

type socketBaseMessage struct {
	Id   string `json:"id,omitempty"`
	Type string `json:"type,omitempty"`
}

type socketConnectionRequest struct {
	socketBaseMessage
	Payload json.RawMessage `json:"payload,omitempty"`
}

type simpleResponse struct {
	socketBaseMessage
}

type payloadResponse struct {
	simpleResponse
	Payload interface{} `json:"payload,omitempty"`
}

func NewSocketConnection(connection *websocket.Conn, r *http.Request, schemaProvider SchemaProvider) *SocketConnection {
	sockConn := &SocketConnection{}

	ctx := context.Background()
	ctx = context.WithValue(ctx, "Authentication", GetAuthValue(r))
	sockConn.ctx = ctx

	sockConn.connection = connection
	sockConn.schemaProvider = schemaProvider
	sockConn.closed = false

	return sockConn
}

func (s *SocketConnection) send(r interface{}, msgType int) error {
	responseJSON, err := json.Marshal(r)
	if err != nil {
		errorResponse, _ := json.Marshal(err)
		s.connection.WriteMessage(msgType, errorResponse)
		return err
	}

	if err = s.connection.WriteMessage(msgType, responseJSON); err != nil {
		errorResponse, _ := json.Marshal(err)
		s.connection.WriteMessage(msgType, errorResponse)
		return err
	}

	return nil
}

func (s *SocketConnection) handleConnectionInit(request *socketConnectionRequest, msgType int) {
	var connectionParams map[string]interface{}
	err := json.Unmarshal(request.Payload, &connectionParams)
	if err != nil {
		fmt.Printf("Error parsing payload %v: %v\n", string(request.Payload), err)
		return
	}

	var authToken = connectionParams["Authentication"]
	if authToken != nil {
		s.ctx = context.WithValue(s.ctx, "Authentication", authToken.(string))
	}

	var socketResponse payloadResponse
	socketResponse.Id = request.Id
	socketResponse.Type = "connection_ack"
	socketResponse.Payload = "ACK"
	s.send(socketResponse, msgType)
}

func (s *SocketConnection) handleConnectionTerminate(request *socketConnectionRequest, msgType int) {
	s.closed = true
}

func (s *SocketConnection) handleStart(request *socketConnectionRequest, msgType int) {
	var payload dukGraphql.Request
	err := json.Unmarshal(request.Payload, &payload)
	if err != nil {
		fmt.Printf("Error parsing payload %v: %v\n", string(request.Payload), err)
		return
	}

	params := graphql.Params{
		Schema:         s.schemaProvider.GetSchema(),
		RequestString:  payload.Query,
		VariableValues: payload.Variables,
		OperationName:  payload.OperationName,
		Context:        s.ctx,
	}

	var socketResponse payloadResponse
	socketResponse.Id = request.Id
	socketResponse.Type = "data"
	socketResponse.Payload = graphql.Do(params)
	s.send(socketResponse, msgType)

	var completeResponse simpleResponse
	completeResponse.Id = request.Id
	completeResponse.Type = "complete"
	s.send(completeResponse, msgType)
}

func (s *SocketConnection) handleStop(request *socketConnectionRequest, msgType int) {
}

func (s *SocketConnection) processMessage(request *socketConnectionRequest, msgType int) {
	switch request.Type {
	case "connection_init":
		s.handleConnectionInit(request, msgType)
	case "connection_terminate":
		s.handleConnectionTerminate(request, msgType)
	case "start":
		s.handleStart(request, msgType)
	case "stop":
		s.handleStop(request, msgType)
	default:
		panic("Unknown socket-request-type: " + request.Type)
		s.closed = true
	}
}

func (s *SocketConnection) ProcessMessages() {
	fmt.Println("Start processing messages")
	defer fmt.Println("Stop processing messages")
	for {
		if s.closed {
			break
		}

		msgType, message, err := s.connection.ReadMessage()

		if err != nil {
			fmt.Println(err)
			s.closed = true
			break
		}

		request := &socketConnectionRequest{}

		if err = json.Unmarshal(message, &request); err != nil {
			errorResponse, _ := json.Marshal(err)
			s.connection.WriteMessage(msgType, errorResponse)
		}

		s.processMessage(request, msgType)
	}

	s.connection.Close()
}
