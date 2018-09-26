package schema

import (
	"fmt"
	"time"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
)

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
