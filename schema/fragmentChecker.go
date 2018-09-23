package schema

import (
	"fmt"
	"reflect"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
)

type FragmentChecker struct {
	Fragments     map[string]ast.Definition
	UsedFragments map[string]bool
}

func (c *FragmentChecker) MarkFragmentSpread(fragmentSpread *ast.FragmentSpread) {
	c.UsedFragments[fragmentSpread.Name.Value] = true

	c.MarkSelectionSet(c.Fragments[fragmentSpread.Name.Value].GetSelectionSet())
}

func (c *FragmentChecker) MarkSelection(selection ast.Selection) {
	switch selection.(type) {
	case *ast.FragmentSpread:
		c.MarkFragmentSpread(selection.(*ast.FragmentSpread))
	case *ast.Field:
		c.MarkField(selection.(*ast.Field))
	default:
		fmt.Println("Unknown type: ")
		fmt.Println(reflect.TypeOf(selection))
		panic(1)
	}
}

func (c *FragmentChecker) MarkSelectionSet(selectionSet *ast.SelectionSet) {
	for _, selection := range selectionSet.Selections {
		c.MarkSelection(selection)
	}
}

func (c *FragmentChecker) MarkField(field *ast.Field) {
	if field.SelectionSet != nil {
		c.MarkSelectionSet(field.SelectionSet)
	}
}

func (c *FragmentChecker) MarkFields(p graphql.ResolveParams) {
	for _, field := range p.Info.FieldASTs {
		c.MarkField(field)
	}
}
