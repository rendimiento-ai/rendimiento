package ruta

import (
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

// What a tool answers grows: app_estado gained its libreta, say. A client
// keeps the tools' schemas it listed when it connected, and this endpoint
// is stateless (each request is answered on its own, with no stream back),
// so it cannot tell a connected client that the list changed
// (notifications/tools/list_changed): a session opened before a release
// keeps the schemas of the release before. The SDK infers each output
// schema from its Go type and closes every object («additionalProperties:
// false»), so an old session would reject an answer with one field more.
// Output schemas are therefore left open: an answer may carry fields the
// schema does not name, and a new field is always optional to whoever
// knew the schema without it.

// outputSchemaFor is the output schema the SDK would infer for Out, with
// its objects open to fields it does not name (nil when it cannot be
// inferred: the SDK then infers it itself).
func outputSchemaFor[Out any]() *jsonschema.Schema {
	rt := reflect.TypeFor[Out]()
	if rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Struct {
		return nil
	}
	s, err := jsonschema.ForType(rt, &jsonschema.ForOptions{})
	if err != nil || s == nil {
		return nil
	}
	abrir(s, map[*jsonschema.Schema]bool{})
	return s
}

// closed is the schema «false»: no value is valid.
var closed = &jsonschema.Schema{Not: &jsonschema.Schema{}}

// abrir opens every object of a schema to fields it does not name, keeping
// what a map's values must be.
func abrir(s *jsonschema.Schema, seen map[*jsonschema.Schema]bool) {
	if s == nil || seen[s] {
		return
	}
	seen[s] = true
	if reflect.DeepEqual(s.AdditionalProperties, closed) {
		s.AdditionalProperties = nil
	} else {
		abrir(s.AdditionalProperties, seen)
	}
	for _, p := range s.Properties {
		abrir(p, seen)
	}
	for _, d := range s.Defs {
		abrir(d, seen)
	}
	abrir(s.Items, seen)
	for _, list := range [][]*jsonschema.Schema{s.PrefixItems, s.ItemsArray, s.AnyOf, s.OneOf, s.AllOf} {
		for _, x := range list {
			abrir(x, seen)
		}
	}
}
