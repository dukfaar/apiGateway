package main

type SchemaFieldType struct {
	Kind   string           `json:"kind,omitempty"`
	Name   *string          `json:"name,omitempty"`
	OfType *SchemaFieldType `json:"ofType,omitempty"`
}

type SchemaTypeField struct {
	Name string          `json:"name",omitempty`
	Type SchemaFieldType `json:"type,omitempty"`
}

type SchemaType struct {
	Name   string            `json:"name",omitempty`
	Kind   string            `json:"kind,omitempty"`
	Fields []SchemaTypeField `json:"fields",omitempty`
}

type SchemaRootType struct {
	Name string `json:"name",omitempty`
}

type SchemaDefinition struct {
	QueryType        SchemaRootType `json:"queryType",omitempty`
	MutationType     SchemaRootType `json:"mutationType",omitempty`
	SubscriptionType SchemaRootType `json:"subscriptionType",omitempty`
	Types            []SchemaType   `json:"types",omitempty`
}

type SchemaResponseData struct {
	Schema SchemaDefinition `json:"__schema,omitempty"`
}

type SchemaResponse struct {
	Data SchemaResponseData
}
