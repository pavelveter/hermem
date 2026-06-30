package api

// SpecBuilder builds a Spec using a fluent API.
type SpecBuilder struct {
	spec *Spec
}

// NewSpecBuilder returns a builder with OpenAPI version set to 3.1.0.
func NewSpecBuilder() *SpecBuilder {
	return &SpecBuilder{
		spec: &Spec{
			OpenAPI: "3.1.0",
			Components: Components{
				SecuritySchemes: map[string]SecurityScheme{},
				Schemas:         map[string]*Schema{},
			},
			Paths: map[string]*PathItem{},
			Tags:  []Tag{},
		},
	}
}

func (b *SpecBuilder) Title(t string) *SpecBuilder {
	b.spec.Info.Title = t
	return b
}

func (b *SpecBuilder) Description(d string) *SpecBuilder {
	b.spec.Info.Description = d
	return b
}

func (b *SpecBuilder) Version(v string) *SpecBuilder {
	b.spec.Info.Version = v
	return b
}

func (b *SpecBuilder) License(name string) *SpecBuilder {
	b.spec.Info.License = &License{Name: name}
	return b
}

func (b *SpecBuilder) Server(url, desc string) *SpecBuilder {
	b.spec.Servers = append(b.spec.Servers, Server{URL: url, Description: desc})
	return b
}

func (b *SpecBuilder) Tags(tags ...Tag) *SpecBuilder {
	b.spec.Tags = append(b.spec.Tags, tags...)
	return b
}

func (b *SpecBuilder) SecurityScheme(name string, scheme SecurityScheme) *SpecBuilder {
	b.spec.Components.SecuritySchemes[name] = scheme
	return b
}

// GlobalResponseHeader adds a header to every response in every operation.
func (b *SpecBuilder) GlobalResponseHeader(name string, desc string, typ string) *SpecBuilder {
	if b.spec.GlobalResponseHeaders == nil {
		b.spec.GlobalResponseHeaders = map[string]ResponseHeader{}
	}
	b.spec.GlobalResponseHeaders[name] = ResponseHeader{
		Description: desc,
		Schema:      &Schema{Type: typ},
	}
	return b
}

func (b *SpecBuilder) Schemas(s map[string]*Schema) *SpecBuilder {
	for k, v := range s {
		b.spec.Components.Schemas[k] = v
	}
	return b
}

func (b *SpecBuilder) Paths(p map[string]*PathItem) *SpecBuilder {
	for k, v := range p {
		b.spec.Paths[k] = v
	}
	return b
}

func (b *SpecBuilder) Build() *Spec {
	b.injectGlobalHeaders()
	return b.spec
}

// injectGlobalHeaders adds GlobalResponseHeaders to every operation's
// every response in the spec.
func (b *SpecBuilder) injectGlobalHeaders() {
	if len(b.spec.GlobalResponseHeaders) == 0 {
		return
	}
	for _, item := range b.spec.Paths {
		for _, op := range []*Operation{item.Get, item.Post, item.Delete, item.Put} {
			if op == nil {
				continue
			}
			for code, resp := range op.Responses {
				if resp.Headers == nil {
					resp.Headers = map[string]ResponseHeader{}
				}
				for name, hdr := range b.spec.GlobalResponseHeaders {
					resp.Headers[name] = hdr
				}
				op.Responses[code] = resp
			}
		}
	}
}

func ref(name string) *Schema {
	return &Schema{Ref: "#/components/schemas/" + name}
}

func jsonBody(schema string) *RequestBody {
	return &RequestBody{
		Required: true,
		Content: map[string]MediaType{
			"application/json": {Schema: ref(schema)},
		},
	}
}

func errorResponse(desc string) Response {
	return Response{
		Description: desc,
		Content: map[string]MediaType{
			"application/json": {Schema: ref("ErrorResponse")},
		},
	}
}

func auth() []SecurityRequirement {
	return []SecurityRequirement{{"ApiKeyAuth": {}}}
}
