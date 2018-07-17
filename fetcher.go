package main

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/gorilla/websocket"
)

type Request struct {
	Query         string                 `json:"query" url:"query" schema:"query"`
	Variables     map[string]interface{} `json:"variables" url:"variables" schema:"variables"`
	OperationName string                 `json:"operationName" url:"operationName" schema:"operationName"`
}

type Fetcher interface {
	Fetch(request Request, response interface{})
}

type WebSocketFetcher struct {
	Connection *websocket.Conn
}

func NewWebSocketFetcher(host string, path string) (*WebSocketFetcher, error) {
	u := url.URL{Scheme: "ws", Host: host, Path: path}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)

	if err != nil {
		fmt.Printf("Error connecting: %v\n", err)
		return nil, err
	}

	return &WebSocketFetcher{
		Connection: conn,
	}, nil
}

func (f *WebSocketFetcher) Fetch(request Request, response interface{}) error {
	err := f.Connection.WriteJSON(request)

	if err != nil {
		fmt.Printf("%v\n", err)
		return err
	}

	_, msg, err := f.Connection.ReadMessage()

	err = json.Unmarshal(msg, &response)
	if err != nil {
		fmt.Printf("%v\n", err)
		return err
	}

	return nil
}
