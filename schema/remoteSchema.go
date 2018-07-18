package schema

import "github.com/dukfaar/goUtils/eventbus"

type RemoteSchema struct {
	SchemaResponse Response
	ServiceInfo    eventbus.ServiceInfo
}
