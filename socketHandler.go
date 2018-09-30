package main

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

type SocketHandler struct {
	upgrader       websocket.Upgrader
	schemaProvider SchemaProvider
}

func NewSocketHandler(schemaProvider SchemaProvider) *SocketHandler {
	result := &SocketHandler{}

	result.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	result.schemaProvider = schemaProvider

	return result
}

func (s *SocketHandler) createConnection(w http.ResponseWriter, r *http.Request) (*SocketConnection, error) {
	protocols := websocket.Subprotocols(r)
	header := make(http.Header)
	header["Sec-WebSocket-Protocol"] = protocols
	connection, upgradeError := s.upgrader.Upgrade(w, r, header)

	if upgradeError != nil {
		log.Println(upgradeError)
		return nil, upgradeError
	}

	return NewSocketConnection(connection, r, s.schemaProvider), nil
}

func (s *SocketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	socketConnection, error := s.createConnection(w, r)

	if error != nil {
		go socketConnection.ProcessMessages()
	}
}
