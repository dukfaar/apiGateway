package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dukfaar/goUtils/eventbus"
	dukGraphql "github.com/dukfaar/goUtils/graphql"
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
)

type MergedSchemas struct {
	serviceSchemas map[string]RemoteSchema
	types          map[string]*graphql.Object
	inputTypes     map[string]*graphql.InputObject
}

func serializeDate(value interface{}) interface{} {
	switch value := value.(type) {
	case time.Time:
		buff, err := value.MarshalText()
		if err != nil {
			return nil
		}

		return string(buff)
	case *time.Time:
		return serializeDate(*value)
	case string:
		return value
	case *string:
		return *value
	default:
		return nil
	}
}

func deserializeDate(value interface{}) interface{} {
	fmt.Println(value)
	return nil
}

var dataScalar *graphql.Scalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "Date",
	Description: "The `Date` scalar type represents a Date.",
	Serialize:   serializeDate,
	ParseValue:  deserializeDate,
	ParseLiteral: func(valueAST ast.Value) interface{} {
		switch valueAST := valueAST.(type) {
		case *ast.StringValue:
			return valueAST.Value
		}
		return nil
	},
})

func getScalarTypeDefinition(fieldType *FieldType) graphql.Output {
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
	case "Date":
		return dataScalar
	default:
		panic("Unknown TypeName " + *fieldType.Name)
	}
}

func (m *MergedSchemas) getTypeDefinition(fieldType *FieldType) graphql.Output {
	switch fieldType.Kind {
	case "SCALAR":
		return getScalarTypeDefinition(fieldType)
	case "OBJECT":
		if m.types[*fieldType.Name] == nil {
			panic("Object Type not defined " + *fieldType.Name)
		}
		return m.types[*fieldType.Name]
	case "INPUT_OBJECT":
		if m.inputTypes[*fieldType.Name] == nil {
			panic("InputObject Type not defined " + *fieldType.Name)
		}
		return m.inputTypes[*fieldType.Name]
	case "NON_NULL":
		return graphql.NewNonNull(m.getTypeDefinition(fieldType.OfType))
	case "LIST":
		return graphql.NewList(m.getTypeDefinition(fieldType.OfType))
	default:
		panic("Unknown Kind " + fieldType.Kind)
	}
}

func (m *MergedSchemas) scanTypes(typeList []Type) {
	for i := range typeList {
		schemaType := typeList[i]

		if schemaType.Kind == "SCALAR" || strings.HasPrefix(schemaType.Name, "__") {
			continue
		}

		switch schemaType.Kind {
		case "SCALAR":
			continue
		case "INPUT_OBJECT":
			continue
		case "INTERFACE":
			continue
		case "OBJECT":
			newObject := graphql.NewObject(graphql.ObjectConfig{
				Name:   schemaType.Name,
				Fields: graphql.Fields{},
			})

			if m.types[schemaType.Name] == nil {
				m.types[schemaType.Name] = newObject
			}
		default:
			panic("Unknown kind " + schemaType.Kind)
		}
	}
}

func (m *MergedSchemas) scanInputTypes(typeList []Type) {
	for i := range typeList {
		schemaType := typeList[i]

		if schemaType.Kind == "INPUT_OBJECT" {
			fields := graphql.InputObjectConfigFieldMap{}

			for fieldIndex := range schemaType.InputFields {
				field := schemaType.InputFields[fieldIndex]

				fields[field.Name] = &graphql.InputObjectFieldConfig{
					Type: m.getTypeDefinition(&field.Type),
				}
			}

			newObject := graphql.NewInputObject(graphql.InputObjectConfig{
				Name:   schemaType.Name,
				Fields: fields,
			})

			if m.inputTypes[schemaType.Name] == nil {
				m.inputTypes[schemaType.Name] = newObject
			}
		}
	}
}

func getSourceBody(p graphql.ResolveParams) string {
	if len(p.Info.FieldASTs) > 0 {
		queryField := p.Info.FieldASTs[0]

		return string(queryField.Loc.Source.Body)[queryField.Loc.Start:queryField.Loc.End]
	}

	return ""
}

func setAuthHeaders(p *graphql.ResolveParams, request *http.Request) {
	authValue := p.Context.Value("Authentication").(string)

	if authValue != "" {
		request.Header.Add("Authentication", authValue)
		request.Header.Add("Authorization", authValue)
	}
}

func setJSONHeaders(request *http.Request) {
	request.Header.Add("Accept", "application/json")
	request.Header.Add("Content-Type", "application/json")
}

func getQueryArgs(p graphql.ResolveParams) string {
	variableDefs := p.Info.Operation.GetVariableDefinitions()

	//TODO, only add variables that are used for this query
	if len(variableDefs) > 0 {
		varStrings := make([]string, 0)
		for i := range variableDefs {
			varDef := variableDefs[i]

			varStrings = append(varStrings, string(varDef.Loc.Source.Body)[varDef.Loc.Start:varDef.Loc.End])
		}

		return "(" + strings.Join(varStrings, ",") + ")"
	}

	return ""
}

func getFragments(p graphql.ResolveParams) string {
	fragments := ""

	//TODO, only add fragments that are used for this query
	for fragmentName := range p.Info.Fragments {
		loc := p.Info.Fragments[fragmentName].GetLoc()
		fragments += string(loc.Source.Body[loc.Start:loc.End])
	}

	return fragments
}

func createQueryResolver(serviceInfo eventbus.ServiceInfo) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		query := "query" + getQueryArgs(p) + " {" + getSourceBody(p) + "}" + getFragments(p)

		jsonValue, _ := json.Marshal(dukGraphql.Request{
			Query:     query,
			Variables: p.Info.VariableValues,
		})

		client := &http.Client{}
		request, err := http.NewRequest("POST", "http://"+serviceInfo.Hostname+":"+serviceInfo.Port+serviceInfo.GraphQLHttpEndpoint, bytes.NewBuffer(jsonValue))
		if err != nil {
			panic(err)
		}

		setAuthHeaders(&p, request)
		setJSONHeaders(request)

		resp, err := client.Do(request)

		if err != nil {
			panic(err)
		}

		defer resp.Body.Close()
		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)

		if result["errors"] != nil {
			errorString, _ := json.Marshal(result["errors"])
			return nil, errors.New(string(errorString))
		}
		return result["data"].(map[string]interface{})[p.Info.FieldName], nil
	}
}

func createMutationResolver(serviceInfo eventbus.ServiceInfo) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		mutation := "mutation " + getQueryArgs(p) + "{" + getSourceBody(p) + "}" + getFragments(p)

		jsonValue, _ := json.Marshal(dukGraphql.Request{
			Query: mutation,
		})

		client := &http.Client{}
		request, err := http.NewRequest("POST", "http://"+serviceInfo.Hostname+":"+serviceInfo.Port+serviceInfo.GraphQLHttpEndpoint, bytes.NewBuffer(jsonValue))
		if err != nil {
			panic(err)
		}

		setAuthHeaders(&p, request)
		setJSONHeaders(request)

		resp, err := client.Do(request)

		if err != nil {
			panic(err)
		}

		defer resp.Body.Close()
		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)

		if result["errors"] != nil {
			errorString, _ := json.Marshal(result["errors"])
			return nil, errors.New(string(errorString))
		}

		return result["data"].(map[string]interface{})[p.Info.FieldName], nil
	}
}

func (m *MergedSchemas) getFieldArgs(args []FieldArg) graphql.FieldConfigArgument {
	result := graphql.FieldConfigArgument{}

	for i := range args {
		arg := args[i]

		result[arg.Name] = &graphql.ArgumentConfig{
			Type: m.getTypeDefinition(&arg.Type),
		}
	}

	return result
}

func (m *MergedSchemas) scanTypeFields(typeList []Type, serviceInfo eventbus.ServiceInfo) {
	for i := range typeList {
		schemaType := typeList[i]

		if schemaType.Kind == "SCALAR" || strings.HasPrefix(schemaType.Name, "__") {
			continue
		}

		switch schemaType.Kind {
		case "SCALAR":
			continue
		case "INPUT_OBJECT":
			continue
		case "INTERFACE":
			continue
		case "OBJECT":
			object := m.types[schemaType.Name]

			for fieldIndex := range schemaType.Fields {
				field := schemaType.Fields[fieldIndex]

				var fieldDefinition graphql.Field
				fieldDefinition.Name = field.Name
				fieldDefinition.Type = m.getTypeDefinition(&field.Type)
				if schemaType.Name == "Query" {
					fieldDefinition.Resolve = createQueryResolver(serviceInfo)
				} else if schemaType.Name == "Mutation" {
					fieldDefinition.Resolve = createMutationResolver(serviceInfo)
				} else {
					fieldDefinition.Resolve = func(p graphql.ResolveParams) (interface{}, error) {
						result := p.Source.(map[string]interface{})[p.Info.FieldName]
						return result, nil
					}
				}

				if len(field.Args) > 0 {
					fieldDefinition.Args = m.getFieldArgs(field.Args)
				}

				object.AddFieldConfig(field.Name, &fieldDefinition)
			}
		default:
			panic("Unknown kind " + schemaType.Kind)
		}
	}
}

func (m *MergedSchemas) BuildSchema() (graphql.Schema, error) {
	m.types = make(map[string]*graphql.Object)
	m.inputTypes = make(map[string]*graphql.InputObject)

	for i := range m.serviceSchemas {
		remoteSchema := m.serviceSchemas[i]
		m.scanTypes(remoteSchema.SchemaResponse.Data.Schema.Types)
		m.scanInputTypes(remoteSchema.SchemaResponse.Data.Schema.Types)
		m.scanTypeFields(remoteSchema.SchemaResponse.Data.Schema.Types, remoteSchema.ServiceInfo)
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

func (m *MergedSchemas) AddService(serviceInfo eventbus.ServiceInfo, schemaResponse Response) {
	if m.serviceSchemas == nil {
		m.serviceSchemas = make(map[string]RemoteSchema)
	}

	m.serviceSchemas[serviceInfo.Name] = RemoteSchema{
		ServiceInfo:    serviceInfo,
		SchemaResponse: schemaResponse,
	}
}
