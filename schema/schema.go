package schema

type FieldType struct {
	Kind   string     `json:"kind,omitempty"`
	Name   *string    `json:"name,omitempty"`
	OfType *FieldType `json:"ofType,omitempty"`
}

type FieldArg struct {
	DefaultValue *string   `json:"defaultValue,omitempty"`
	Description  *string   `json:"description,omitempty"`
	Name         string    `json:"name,omitempty"`
	Type         FieldType `json:"type,omitempty"`
}

type TypeField struct {
	Name string     `json:"name",omitempty`
	Type FieldType  `json:"type,omitempty"`
	Args []FieldArg `json:"args",omitempty`
}

type TypeInputField struct {
	Name string     `json:"name",omitempty`
	Type FieldType  `json:"type,omitempty"`
	Args []FieldArg `json:"args",omitempty`
}

type Type struct {
	Name        string      `json:"name",omitempty`
	Kind        string      `json:"kind,omitempty"`
	Fields      []TypeField `json:"fields",omitempty`
	InputFields []FieldArg  `json:"inputFields",omitempty`
}

type RootType struct {
	Name string `json:"name",omitempty`
}

type Definition struct {
	QueryType        RootType `json:"queryType",omitempty`
	MutationType     RootType `json:"mutationType",omitempty`
	SubscriptionType RootType `json:"subscriptionType",omitempty`
	Types            []Type   `json:"types",omitempty`
}

type ResponseData struct {
	Schema Definition `json:"__schema,omitempty"`
}

type Response struct {
	Data ResponseData
}
