package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/dukfaar/goUtils/env"
	"github.com/dukfaar/goUtils/eventbus"

	"github.com/graphql-go/graphql"
)

type RemoteSchema struct {
	schemaResponse SchemaResponse
	serviceInfo    eventbus.ServiceInfo
}

type MergedSchemas struct {
	serviceSchemas map[string]RemoteSchema
	types          map[string]*graphql.Object
}

func (m *MergedSchemas) GetTypeDefinition(fieldType *SchemaFieldType) graphql.Output {
	switch fieldType.Kind {
	case "SCALAR":
		switch *fieldType.Name {
		case "String":
			return graphql.String
		case "ID":
			return graphql.ID
		case "Boolean":
			return graphql.Boolean
		case "Float":
			return graphql.Float
		case "Int":
			return graphql.Int
		case "DateTime":
			return graphql.DateTime
		default:
			panic("Unknown TypeName " + *fieldType.Name)
		}
	case "OBJECT":
		if m.types[*fieldType.Name] == nil {
			panic("Object Type not defined " + *fieldType.Name)
		}
		return m.types[*fieldType.Name]
	case "NON_NULL":
		return graphql.NewNonNull(m.GetTypeDefinition(fieldType.OfType))
	case "LIST":
		return graphql.NewList(m.GetTypeDefinition(fieldType.OfType))
	default:
		panic("Unknown Kind " + fieldType.Kind)
	}
}

func (m *MergedSchemas) ScanTypes(typeList []SchemaType) {
	for i := range typeList {
		schemaType := typeList[i]

		if schemaType.Kind == "SCALAR" || strings.HasPrefix(schemaType.Name, "__") {
			continue
		}

		switch schemaType.Kind {
		case "SCALAR":
			continue
		case "OBJECT":
			newObject := graphql.NewObject(graphql.ObjectConfig{
				Name:   schemaType.Name,
				Fields: graphql.Fields{},
			})

			if m.types[schemaType.Name] == nil {
				m.types[schemaType.Name] = newObject
			}
		}
	}
}

func (m *MergedSchemas) ScanTypeFields(typeList []SchemaType, serviceInfo eventbus.ServiceInfo) {
	for i := range typeList {
		schemaType := typeList[i]

		if schemaType.Kind == "SCALAR" || strings.HasPrefix(schemaType.Name, "__") {
			continue
		}

		switch schemaType.Kind {
		case "SCALAR":
			continue
		case "OBJECT":
			object := m.types[schemaType.Name]

			for fieldIndex := range schemaType.Fields {
				field := schemaType.Fields[fieldIndex]

				var fieldDefinition graphql.Field
				fieldDefinition.Name = field.Name
				fieldDefinition.Type = m.GetTypeDefinition(&field.Type)
				if schemaType.Name == "Query" {
					fieldDefinition.Resolve = func(p graphql.ResolveParams) (interface{}, error) {
						queryContent := ""
						if len(p.Info.FieldASTs) > 0 {
							queryField := p.Info.FieldASTs[0]
							start := queryField.Loc.Start
							end := queryField.Loc.End
							source := queryField.Loc.Source

							queryContent += string(source.Body)[start:end]
						}

						query := "query {" + queryContent + "}"

						jsonValue, _ := json.Marshal(Request{
							Query: query,
						})
						resp, err := http.Post("http://"+serviceInfo.Hostname+":"+serviceInfo.Port+serviceInfo.GraphQLHttpEndpoint, "application/json", bytes.NewBuffer(jsonValue))

						if err != nil {
							panic(err)
						}

						defer resp.Body.Close()
						var result map[string]interface{}
						json.NewDecoder(resp.Body).Decode(&result)

						return result["data"].(map[string]interface{})[p.Info.FieldName], nil
					}
				} else {
					fieldDefinition.Resolve = func(p graphql.ResolveParams) (interface{}, error) {
						result := p.Source.(map[string]interface{})[p.Info.FieldName]
						return result, nil
					}
				}

				object.AddFieldConfig(field.Name, &fieldDefinition)
			}
		}
	}
}

func (m *MergedSchemas) BuildSchema() (graphql.Schema, error) {
	m.types = make(map[string]*graphql.Object)

	for i := range m.serviceSchemas {
		remoteSchema := m.serviceSchemas[i]
		m.ScanTypes(remoteSchema.schemaResponse.Data.Schema.Types)
		m.ScanTypeFields(remoteSchema.schemaResponse.Data.Schema.Types, remoteSchema.serviceInfo)
	}

	schemaConfig := graphql.SchemaConfig{
		Query:        m.types["Query"],
		Mutation:     m.types["Mutation"],
		Subscription: m.types["Subscription"],
	}
	schema, err := graphql.NewSchema(schemaConfig)

	if err != nil {
		return graphql.Schema{}, err
	}

	return schema, nil
}

func (m *MergedSchemas) AddService(serviceInfo eventbus.ServiceInfo, schemaResponse SchemaResponse) {
	if m.serviceSchemas == nil {
		m.serviceSchemas = make(map[string]RemoteSchema)
	}

	m.serviceSchemas[serviceInfo.Name] = RemoteSchema{
		serviceInfo:    serviceInfo,
		schemaResponse: schemaResponse,
	}
}

var mergedSchemas MergedSchemas
var currentSchema graphql.Schema

func ProcessServiceUp(serviceInfo eventbus.ServiceInfo) {
	jsonValue, _ := json.Marshal(Request{
		Query: IntrospectionQuery,
	})
	resp, err := http.Post("http://"+serviceInfo.Hostname+":"+serviceInfo.Port+serviceInfo.GraphQLHttpEndpoint, "application/json", bytes.NewBuffer(jsonValue))

	if err != nil {
		panic(err)
	}

	defer resp.Body.Close()

	var schemaResponse SchemaResponse
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
	endChan := make(chan int)

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

		var opts Request
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

	os.Exit(<-endChan)
}
