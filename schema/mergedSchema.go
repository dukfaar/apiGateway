package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/gorilla/websocket"

	"github.com/dukfaar/goUtils/eventbus"
	dukGraphql "github.com/dukfaar/goUtils/graphql"
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
)

type MergedSchemas struct {
	serviceSchemas    map[string]RemoteSchema
	types             map[string]*graphql.Object
	inputTypes        map[string]*graphql.InputObject
	typeExtensions    map[string]map[string]bool
	serviceInfoByType map[string]eventbus.ServiceInfo
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

func (m *MergedSchemas) scanType(schemaType Type, serviceInfo eventbus.ServiceInfo) {
	if schemaType.Kind == "SCALAR" || strings.HasPrefix(schemaType.Name, "__") {
		return
	}

	switch schemaType.Kind {
	case "SCALAR":
		return
	case "INPUT_OBJECT":
		return
	case "INTERFACE":
		return
	case "OBJECT":
		newObject := graphql.NewObject(graphql.ObjectConfig{
			Name:   schemaType.Name,
			Fields: graphql.Fields{},
		})

		if m.types[schemaType.Name] == nil {
			m.types[schemaType.Name] = newObject
			m.serviceInfoByType[schemaType.Name] = serviceInfo
		}
	default:
		panic("Unknown kind " + schemaType.Kind)
	}
}

func (m *MergedSchemas) scanTypes(typeList []Type, serviceInfo eventbus.ServiceInfo) {
	for i := range typeList {
		m.scanType(typeList[i], serviceInfo)
	}
}

func (m *MergedSchemas) scanInputType(schemaType Type) {
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

func (m *MergedSchemas) scanInputTypes(typeList []Type) {
	for i := range typeList {
		m.scanInputType(typeList[i])
	}
}

func (m *MergedSchemas) getSourceBodyFromSelection(selection ast.Selection, parentTypename string) string {
	switch selection.(type) {
	case *ast.Field:
		field := selection.(*ast.Field)
		if m.typeExtensions[parentTypename] != nil {
			if m.typeExtensions[parentTypename][field.Name.Value] {
				//TODO return the required fields to resolve this instead
				return ""
			}
		}
		fieldType := m.types[parentTypename].Fields()[field.Name.Value]
		return m.getSourceBodyFromField(selection.(*ast.Field), getOutputTypeName(fieldType.Type))
	default:
		fmt.Printf("Unknown selection type: %+v\n", selection)
		return ""
	}
}

func (m *MergedSchemas) getSourceBodyFromSelectionSet(selectionSet *ast.SelectionSet, parentTypename string) string {
	results := make([]string, 0)

	for _, selection := range selectionSet.Selections {
		selectionString := m.getSourceBodyFromSelection(selection, parentTypename)
		if selectionString != "" {
			results = append(results, selectionString)
		}
	}

	return strings.Join(results, " ")
}

func (m *MergedSchemas) getSourceBodyFromField(field *ast.Field, returnType string) string {
	resultString := ""
	resultString += field.Name.Value

	argumentList := make([]string, 0)
	for _, argument := range field.Arguments {
		switch argument.Value.(type) {
		case *ast.StringValue:
			argumentList = append(argumentList, argument.Name.Value+": \""+argument.Value.GetValue().(string)+"\"")
		case *ast.IntValue:
			argumentList = append(argumentList, argument.Name.Value+": "+argument.Value.GetValue().(string))
		case *ast.FloatValue:
			argumentList = append(argumentList, argument.Name.Value+": "+argument.Value.GetValue().(string))
		case *ast.BooleanValue:
			argumentList = append(argumentList, argument.Name.Value+": "+argument.Value.GetValue().(string))
		default:
			fmt.Printf("Unsupported argument Type: %+v\n", argument.Value.GetKind())
		}

	}

	if len(argumentList) > 0 {
		resultString += "(" + strings.Join(argumentList, ",") + ")"
	}

	if field.SelectionSet != nil {
		resultString += "{" + m.getSourceBodyFromSelectionSet(field.SelectionSet, returnType) + "}"
	}
	return resultString
}

func getOutputTypeName(output graphql.Output) string {
	switch output.(type) {
	case *graphql.Scalar:
		return output.(*graphql.Scalar).Name()
	case *graphql.Object:
		return output.(*graphql.Object).Name()
	//case *graphql.Interface:
	//case *graphql.Union:
	//case *graphql.Enum:
	case *graphql.List:
		return getOutputTypeName(output.(*graphql.List).OfType)
	case *graphql.NonNull:
		return getOutputTypeName(output.(*graphql.NonNull).OfType)
	default:
		panic("Unsupported Outputtype " + fmt.Sprintf("%+v", reflect.TypeOf(output)))
	}
}

func (m *MergedSchemas) getSourceBody(p graphql.ResolveParams) string {
	if len(p.Info.FieldASTs) > 0 {
		return m.getSourceBodyFromField(p.Info.FieldASTs[0], getOutputTypeName(p.Info.ReturnType))
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

	argUsage := make(map[string]bool)

	for _, field := range p.Info.FieldASTs {
		for _, argument := range field.Arguments {
			if argument.Value.GetKind() == "Variable" {
				argUsage[argument.Value.GetValue().(*ast.Name).Value] = true
			}
		}
	}

	if len(variableDefs) > 0 {
		varStrings := make([]string, 0)
		for i := range variableDefs {
			varDef := variableDefs[i]
			varName := varDef.Variable.Name.Value

			if argUsage[varName] {
				varStrings = append(varStrings, string(varDef.Loc.Source.Body)[varDef.Loc.Start:varDef.Loc.End])
			}
		}

		if len(varStrings) > 0 {
			return "(" + strings.Join(varStrings, ",") + ")"
		}

		return ""
	}

	return ""
}

func getFragments(p graphql.ResolveParams) string {
	fragments := ""

	checker := &FragmentChecker{
		Fragments:     p.Info.Fragments,
		UsedFragments: make(map[string]bool),
	}
	checker.MarkFields(p)

	for fragmentName := range p.Info.Fragments {
		if checker.UsedFragments[fragmentName] {
			loc := p.Info.Fragments[fragmentName].GetLoc()
			fragments += string(loc.Source.Body[loc.Start:loc.End])
		}
	}

	return fragments
}

func performRequest(serviceInfo eventbus.ServiceInfo, p graphql.ResolveParams, query string) (*http.Response, error) {
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

	return client.Do(request)
}

func handleRequestResult(p graphql.ResolveParams, resp *http.Response, err error) (interface{}, error) {
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

func (m *MergedSchemas) createQueryResolver(serviceInfo eventbus.ServiceInfo) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		query := "query" + getQueryArgs(p) + " {" + m.getSourceBody(p) + "}" + getFragments(p)

		resp, err := performRequest(serviceInfo, p, query)

		return handleRequestResult(p, resp, err)
	}
}

func (m *MergedSchemas) createExtensionQueryResolver(serviceInfo eventbus.ServiceInfo, field eventbus.FieldType) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		query := "query {"

		query += field.Resolve.By

		if len(field.Resolve.FieldArguments) > 0 {
			arguments := make([]string, 0)

			source := p.Source.(map[string]interface{})

			for argument, resolveBy := range field.Resolve.FieldArguments {
				arguments = append(arguments, fmt.Sprintf("%v:%#v", argument, source[resolveBy]))
			}

			query += "(" + strings.Join(arguments, ",") + ")"
		}

		if p.Info.FieldASTs[0].SelectionSet != nil {
			query += "{" + m.getSourceBodyFromSelectionSet(p.Info.FieldASTs[0].SelectionSet, field.Type) + "}"
		}

		query += "}"

		resp, err := performRequest(serviceInfo, p, query)

		return handleRequestResult(p, resp, err)
	}
}

func (m *MergedSchemas) createMutationResolver(serviceInfo eventbus.ServiceInfo) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		mutation := "mutation " + getQueryArgs(p) + "{" + m.getSourceBody(p) + "}" + getFragments(p)

		resp, err := performRequest(serviceInfo, p, mutation)

		return handleRequestResult(p, resp, err)
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
					fieldDefinition.Resolve = m.createQueryResolver(serviceInfo)
				} else if schemaType.Name == "Mutation" {
					fieldDefinition.Resolve = m.createMutationResolver(serviceInfo)
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

func (m *MergedSchemas) markExtensionField(typeName string, fieldName string) {
	t := m.typeExtensions[typeName]
	if t == nil {
		t = make(map[string]bool)
		m.typeExtensions[typeName] = t
	}
	t[fieldName] = true
}

func (m *MergedSchemas) scanTypeExtensionField(extendingType *graphql.Object, field eventbus.FieldType) {
	targetType := m.types[field.Type]
	if targetType == nil {
		return
	}

	var fieldDefinition graphql.Field
	fieldDefinition.Name = field.Name
	fieldDefinition.Type = targetType
	fieldDefinition.Resolve = m.createExtensionQueryResolver(m.serviceInfoByType[targetType.Name()], field)

	extendingType.AddFieldConfig(field.Name, &fieldDefinition)

	m.markExtensionField(extendingType.Name(), field.Name)
}

func (m *MergedSchemas) scanTypeExtension(extension eventbus.SchemaExtension) {
	extendingType := m.types[extension.Type]

	if extendingType == nil {
		return
	}

	for _, field := range extension.Fields {
		m.scanTypeExtensionField(extendingType, field)
	}
}

func (m *MergedSchemas) scanTypeExtensions(serviceInfo eventbus.ServiceInfo) {
	for _, extension := range serviceInfo.SchemaExtensions {
		m.scanTypeExtension(extension)
	}
}

func (m *MergedSchemas) BuildSchema() (graphql.Schema, error) {
	m.types = make(map[string]*graphql.Object)
	m.inputTypes = make(map[string]*graphql.InputObject)
	m.typeExtensions = make(map[string]map[string]bool)
	m.serviceInfoByType = make(map[string]eventbus.ServiceInfo)

	for i := range m.serviceSchemas {
		remoteSchema := m.serviceSchemas[i]
		m.scanTypes(remoteSchema.SchemaResponse.Data.Schema.Types, remoteSchema.ServiceInfo)
		m.scanInputTypes(remoteSchema.SchemaResponse.Data.Schema.Types)
		m.scanTypeFields(remoteSchema.SchemaResponse.Data.Schema.Types, remoteSchema.ServiceInfo)
	}

	for i := range m.serviceSchemas {
		m.scanTypeExtensions(m.serviceSchemas[i].ServiceInfo)
	}

	schemaConfig := graphql.SchemaConfig{
		Query:        m.types["Query"],
		Mutation:     m.types["Mutation"],
		Subscription: m.types["Subscription"],
	}
	schema, err := graphql.NewSchema(schemaConfig)

	if err != nil {
		fmt.Printf("Error creating schema: %v\n", err)
		return graphql.Schema{}, err
	}

	return schema, nil
}

func (m *MergedSchemas) monitorService(serviceInfo eventbus.ServiceInfo, schemaResponse Response) {
	var websocketUrl = "ws://" + serviceInfo.Hostname + ":" + serviceInfo.Port + serviceInfo.GraphQLSocketEndpoint
	fmt.Println(websocketUrl)

	c, _, err := websocket.DefaultDialer.Dial(websocketUrl, nil)

	if err != nil {
		fmt.Println("Error connection to socket of service")
		fmt.Println(err)
		return
	}

	c.SetCloseHandler(func(code int, text string) error {
		fmt.Printf("Connection closed to service %s(%v)\n", serviceInfo.Name, code)
		fmt.Println(text)

		//todo remove service
		return nil
	})

	go func() {
		defer c.Close()

		for {
			msgType, msg, err := c.ReadMessage()
			fmt.Printf("msg from %s(%v): %s\n", serviceInfo.Name, msgType, string(msg))

			if err != nil {
				return
			}
		}
	}()
}

func (m *MergedSchemas) AddService(serviceInfo eventbus.ServiceInfo, schemaResponse Response) {
	if m.serviceSchemas == nil {
		m.serviceSchemas = make(map[string]RemoteSchema)
	}

	m.serviceSchemas[serviceInfo.Name] = RemoteSchema{
		ServiceInfo:    serviceInfo,
		SchemaResponse: schemaResponse,
	}

	//m.monitorService(serviceInfo, schemaResponse)
}
