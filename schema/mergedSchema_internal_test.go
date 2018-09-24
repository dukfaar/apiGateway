package schema

import (
	"testing"
)

func BenchmarkMarkExtensionField(b *testing.B) {
	mergedSchema := &MergedSchemas{}
	mergedSchema.typeExtensions = make(map[string]map[string]bool)
	for i := 0; i < b.N; i++ {
		mergedSchema.markExtensionField("testType", "testField")
	}
}

func TestMarkExtensionField(t *testing.T) {
	mergedSchema := &MergedSchemas{}
	mergedSchema.typeExtensions = make(map[string]map[string]bool)
	mergedSchema.markExtensionField("testType", "testField")
	mergedSchema.markExtensionField("testType", "testField5")
	mergedSchema.markExtensionField("testType4", "testField2")
	mergedSchema.markExtensionField("testType2", "testField5")

	if mergedSchema.typeExtensions["testType"]["testField"] != true {
		t.Error("field is not correctly set as extend")
	}
	if mergedSchema.typeExtensions["testType"]["testField5"] != true {
		t.Error("field is not correctly set as extend")
	}
	if mergedSchema.typeExtensions["testType4"]["testField2"] != true {
		t.Error("field is not correctly set as extend")
	}
	if mergedSchema.typeExtensions["testType2"]["testField5"] != true {
		t.Error("field is not correctly set as extend")
	}
}
