package desk

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
)

// Operation arguments/results are normally opaque exact bytes. The finite
// Graph and Policy receipt DTOs are consumed as host custody, so their finite object
// members must be exact before encoding/json can apply case aliases/defaults.
// Opaque tool-result JSON and Graph/pack content remain encoded byte strings.
func readAssistantHostReceipt(raw []byte, into any) error {
	shape := reflect.TypeOf(into)
	if shape.Kind() != reflect.Pointer || shape.Elem().Kind() != reflect.Struct {
		return errAssistantRunInvalid
	}
	shape = shape.Elem()
	members, err := configMembers(raw)
	if err != nil {
		return errAssistantRunInvalid
	}
	fields := map[string]reflect.Type{}
	required := map[string]bool{}
	for i := 0; i < shape.NumField(); i++ {
		field := shape.Field(i)
		tag := field.Tag.Get("json")
		name, options, _ := strings.Cut(tag, ",")
		fields[name] = field.Type
		if options != "omitempty" {
			required[name] = true
		}
	}
	for _, member := range members {
		field, ok := fields[member.name]
		value := raw[member.start:member.end]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errAssistantRunInvalid
		}
		delete(required, member.name)
		if field.Kind() == reflect.Pointer && field.Elem().Kind() == reflect.Struct {
			if err := readAssistantHostReceipt(value, reflect.New(field.Elem()).Interface()); err != nil {
				return err
			}
		}
		if field.Kind() == reflect.Struct {
			if err := readAssistantHostReceipt(value, reflect.New(field).Interface()); err != nil {
				return err
			}
		}
	}
	if len(required) != 0 || json.Unmarshal(raw, into) != nil {
		return errAssistantRunInvalid
	}
	return nil
}
