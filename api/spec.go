package api

import (
	"encoding/json"
	"sync"

	"gopkg.in/yaml.v3"
)

// BuildVersion is set via -ldflags at build time.
// Falls back to "dev" for local builds.
var BuildVersion = "dev"

// Spec is the root OpenAPI 3.1 document.
type Spec struct {
	OpenAPI    string               `json:"openapi" yaml:"openapi"`
	Info       Info                 `json:"info" yaml:"info"`
	Servers    []Server             `json:"servers,omitempty" yaml:"servers,omitempty"`
	Paths      map[string]*PathItem `json:"paths" yaml:"paths"`
	Components Components           `json:"components" yaml:"components"`
	Tags       []Tag                `json:"tags,omitempty" yaml:"tags,omitempty"`
}

// Info describes the API.
type Info struct {
	Title       string   `json:"title" yaml:"title"`
	Description string   `json:"description" yaml:"description"`
	Version     string   `json:"version" yaml:"version"`
	License     *License `json:"license,omitempty" yaml:"license,omitempty"`
}

// License identifies the license.
type License struct {
	Name string `json:"name" yaml:"name"`
	URL  string `json:"url,omitempty" yaml:"url,omitempty"`
}

// Server is a connectivity target.
type Server struct {
	URL         string `json:"url" yaml:"url"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Tag groups operations.
type Tag struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// PathItem holds operations for one path.
type PathItem struct {
	Get    *Operation `json:"get,omitempty" yaml:"get,omitempty"`
	Post   *Operation `json:"post,omitempty" yaml:"post,omitempty"`
	Delete *Operation `json:"delete,omitempty" yaml:"delete,omitempty"`
	Put    *Operation `json:"put,omitempty" yaml:"put,omitempty"`
}

// Operation is one API operation.
type Operation struct {
	Summary     string                `json:"summary" yaml:"summary"`
	Description string                `json:"description,omitempty" yaml:"description,omitempty"`
	OperationID string                `json:"operationId" yaml:"operationId"`
	Tags        []string              `json:"tags,omitempty" yaml:"tags,omitempty"`
	Parameters  []Parameter           `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	RequestBody *RequestBody          `json:"requestBody,omitempty" yaml:"requestBody,omitempty"`
	Responses   map[string]Response   `json:"responses" yaml:"responses"`
	Security    []SecurityRequirement `json:"security,omitempty" yaml:"security,omitempty"`
}

// Parameter is a single parameter.
type Parameter struct {
	Name     string  `json:"name" yaml:"name"`
	In       string  `json:"in" yaml:"in"`
	Required bool    `json:"required,omitempty" yaml:"required,omitempty"`
	Schema   *Schema `json:"schema,omitempty" yaml:"schema,omitempty"`
}

// RequestBody describes the request body.
type RequestBody struct {
	Required bool                 `json:"required,omitempty" yaml:"required,omitempty"`
	Content  map[string]MediaType `json:"content" yaml:"content"`
}

// Response is a single response.
type Response struct {
	Description string               `json:"description" yaml:"description"`
	Content     map[string]MediaType `json:"content,omitempty" yaml:"content,omitempty"`
}

// MediaType describes a single media type.
type MediaType struct {
	Schema   *Schema            `json:"schema,omitempty" yaml:"schema,omitempty"`
	Example  interface{}        `json:"example,omitempty" yaml:"example,omitempty"`
	Examples map[string]Example `json:"examples,omitempty" yaml:"examples,omitempty"`
}

// Example is a single example.
type Example struct {
	Summary     string      `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description string      `json:"description,omitempty" yaml:"description,omitempty"`
	Value       interface{} `json:"value,omitempty" yaml:"value,omitempty"`
}

// Schema is a JSON Schema.
type Schema struct {
	Type                 string             `json:"type,omitempty" yaml:"type,omitempty"`
	Description          string             `json:"description,omitempty" yaml:"description,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty" yaml:"properties,omitempty"`
	Required             []string           `json:"required,omitempty" yaml:"required,omitempty"`
	Items                *Schema            `json:"items,omitempty" yaml:"items,omitempty"`
	Ref                  string             `json:"$ref,omitempty" yaml:"$ref,omitempty"`
	Enum                 []string           `json:"enum,omitempty" yaml:"enum,omitempty"`
	Default              interface{}        `json:"default,omitempty" yaml:"default,omitempty"`
	Format               string             `json:"format,omitempty" yaml:"format,omitempty"`
	Minimum              *float64           `json:"minimum,omitempty" yaml:"minimum,omitempty"`
	Maximum              *float64           `json:"maximum,omitempty" yaml:"maximum,omitempty"`
	Deprecated           bool               `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`
	AdditionalProperties *Schema            `json:"additionalProperties,omitempty" yaml:"additionalProperties,omitempty"`
}

// Components holds reusable definitions.
type Components struct {
	Schemas         map[string]*Schema        `json:"schemas,omitempty" yaml:"schemas,omitempty"`
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty" yaml:"securitySchemes,omitempty"`
}

// SecurityScheme describes an authentication scheme.
type SecurityScheme struct {
	Type        string `json:"type" yaml:"type"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	In          string `json:"in,omitempty" yaml:"in,omitempty"`
	Name        string `json:"name,omitempty" yaml:"name,omitempty"`
	Scheme      string `json:"scheme,omitempty" yaml:"scheme,omitempty"`
}

// SecurityRequirement references security schemes.
type SecurityRequirement = map[string][]string

var (
	cachedSpec *Spec
	specOnce   sync.Once
)

// GenerateSpec builds the complete OpenAPI 3.1 specification.
// The result is cached — the spec is immutable during runtime.
func GenerateSpec() *Spec {
	specOnce.Do(func() {
		cachedSpec = NewSpecBuilder().
			Title("Hermem API").
			Description("Persistent graph memory for LLM agents. SQLite. Embeddings. Graph traversal. One binary.").
			Version(BuildVersion).
			License("MIT").
			Server("http://localhost:8420", "Local development").
			Tags(
				Tag{Name: "memory", Description: "Entity storage, search, and retrieval"},
				Tag{Name: "ingest", Description: "Dialog ingestion and entity extraction"},
				Tag{Name: "task", Description: "Task lifecycle management"},
				Tag{Name: "graph", Description: "Graph analytics and integrity"},
				Tag{Name: "temporal", Description: "Time-based queries and timeline"},
				Tag{Name: "admin", Description: "Administrative operations"},
				Tag{Name: "health", Description: "Health checks and metrics"},
			).
			SecurityScheme("ApiKeyAuth", SecurityScheme{
				Type: "apiKey",
				In:   "header",
				Name: "X-API-Key",
			}).
			Schemas(AllSchemas()).
			Paths(AllPaths()).
			Build()
	})
	return cachedSpec
}

// MarshalJSON returns the spec as JSON bytes.
func (s *Spec) MarshalJSON() ([]byte, error) {
	type specAlias Spec
	return json.MarshalIndent((*specAlias)(s), "", "  ")
}

// MarshalYAML returns the spec as YAML bytes.
func (s *Spec) MarshalYAML() ([]byte, error) {
	type specAlias Spec
	return yaml.Marshal((*specAlias)(s))
}

// JSON returns the spec as indented JSON.
func (s *Spec) JSON() []byte {
	b, _ := s.MarshalJSON()
	return b
}

// YAMLBytes returns the spec as YAML.
func (s *Spec) YAMLBytes() []byte {
	b, _ := s.MarshalYAML()
	return b
}
