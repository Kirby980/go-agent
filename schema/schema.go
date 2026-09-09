package schema

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

// Schema 是工具/函数参数的 JSON Schema 定义
type Schema struct {
	Type                 string             `json:"type,omitempty"`
	Description          string             `json:"description,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	Required             []string           `json:"required,omitempty"`
	AdditionalProperties *Schema            `json:"additionalProperties,omitempty"` // 用于 Map 的值类型
}

// Generate 为任意值的类型生成 JSON Schema。
// 实现是一段 reflect 递归：遍历字段、按 kind 映射、读取 json/desc 标签。
func Generate(v any) *Schema {
	if v == nil {
		return &Schema{Type: "null"}
	}
	return generateSchema(reflect.TypeOf(v))
}

func generateSchema(t reflect.Type) *Schema {
	if t.Kind() == reflect.Ptr {
		return generateSchema(t.Elem())
	}
	switch t.Kind() {
	case reflect.String:
		return &Schema{Type: "string"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return &Schema{Type: "number"}
	case reflect.Bool:
		return &Schema{Type: "boolean"}
	case reflect.Slice, reflect.Array:
		return &Schema{
			Type:  "array",
			Items: generateSchema(t.Elem()),
		}
	case reflect.Map:
		// 修复：使用 AdditionalProperties 描述 Map 的值类型
		return &Schema{
			Type:                 "object",
			AdditionalProperties: generateSchema(t.Elem()),
		}
	case reflect.Struct:
		if t == reflect.TypeOf(time.Time{}) {
			return &Schema{Type: "string", Description: "ISO 8601 date-time"}
		}
		s := &Schema{
			Type:       "object",
			Properties: map[string]*Schema{},
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" { // 忽略 unexported fields
				continue
			}

			// 字段名：优先用 json 标签，其次用字段名
			name := f.Name
			if jsonTag := f.Tag.Get("json"); jsonTag != "" {
				if idx := strings.Index(jsonTag, ","); idx > 0 {
					name = jsonTag[:idx]
				} else {
					name = jsonTag
				}
			}

			// 是否必须：优先用 optional 标签，其次看 omitempty
			required := !isOptional(f.Tag.Get("optional")) && !isOptional(f.Tag.Get("omitempty"))

			prop := generateSchema(f.Type)
			if desc := f.Tag.Get("desc"); desc != "" {
				prop.Description = desc
			}
			s.Properties[name] = prop
			if required {
				s.Required = append(s.Required, name)
			}
		}
		return s
	default:
		return &Schema{Type: "string"} // 其他类型 fallback
	}
}

// isOptional 判断标签是否表示可选
func isOptional(tag string) bool {
	tag = strings.ToLower(strings.TrimSpace(tag))
	return tag == "true"
}

func MustJson(s *Schema) json.RawMessage {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		panic(err)
	}
	return b
}
