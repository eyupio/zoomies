package mcp

import "sort"

// Reference is everything this server has to offer, offered or not: every
// tool with whether it changes the fleet, every resource and every prompt. It
// is what `zoomies mcp --describe` prints and what the skill's generated
// reference is built from, so an agent reading about the tools reads this
// release's definitions and not a list somebody once typed.
type Reference struct {
	Tools     []ToolReference     `json:"tools"`
	Resources []ResourceReference `json:"resources"`
	Prompts   []PromptReference   `json:"prompts"`
}

// ToolReference is one tool as the reference describes it.
type ToolReference struct {
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
	ReadOnly    bool           `json:"read_only"`
	// Action is a tool that changes the fleet, offered only when the server
	// is started with leave to; Admin needs an administrator's leave as well.
	Action bool `json:"action"`
	Admin  bool `json:"admin"`
}

// ResourceReference is one resource, or a template for a family of them.
type ResourceReference struct {
	URI         string `json:"uri"`
	Template    bool   `json:"template"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// PromptReference is one prompt and the arguments it takes.
type PromptReference struct {
	Name        string           `json:"name"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Arguments   []promptArgument `json:"arguments,omitempty"`
}

// Describe is the whole surface, in the order the server lists it, with the
// action tools included whatever a running server was given leave to offer.
func Describe() Reference {
	var ref Reference
	for _, t := range tools() {
		ref.Tools = append(ref.Tools, ToolReference{
			Name: t.Name, Title: t.Title, Description: t.Description, InputSchema: t.InputSchema,
			ReadOnly: t.Annotations.ReadOnly, Action: t.action, Admin: IsAdminTool(t.Name),
		})
	}
	for _, r := range resources {
		ref.Resources = append(ref.Resources, ResourceReference{
			URI: r.uri, Template: r.template, Name: r.name, Title: r.title, Description: r.description,
		})
	}
	for _, p := range prompts {
		ref.Prompts = append(ref.Prompts, PromptReference{Name: p.Name, Title: p.Title, Description: p.Description, Arguments: p.Arguments})
	}
	return ref
}

// SchemaProperties is a schema's properties in a stable order, required ones
// first, for a document that is diffed against the binary: a map's order is
// not one.
func SchemaProperties(schema map[string]any) []SchemaProperty {
	props, _ := schema["properties"].(map[string]any)
	required := map[string]bool{}
	if list, ok := schema["required"].([]string); ok {
		for _, name := range list {
			required[name] = true
		}
	}
	out := make([]SchemaProperty, 0, len(props))
	for name, raw := range props {
		def, _ := raw.(map[string]any)
		p := SchemaProperty{Name: name, Required: required[name]}
		p.Type, _ = def["type"].(string)
		p.Description, _ = def["description"].(string)
		if values, ok := def["enum"].([]string); ok {
			p.Enum = values
		} else if items, ok := def["items"].(map[string]any); ok {
			p.Enum, _ = items["enum"].([]string)
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Required != out[j].Required {
			return out[i].Required
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// SchemaProperty is one argument of a tool, flattened for a reader.
type SchemaProperty struct {
	Name        string
	Type        string
	Description string
	Required    bool
	Enum        []string
}
