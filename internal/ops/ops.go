// Package ops defines WhatsApp operations once and exposes each one both as
// an MCP tool and as a REST endpoint (POST /api/<name>), so the two surfaces
// share input schemas, validation and behaviour.
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Kieldro/whatsmeow-mcp/internal/wa"
)

// Kind classifies an operation for MCP tool annotations.
type Kind int

const (
	// Read only reads (local store or WhatsApp) and changes nothing.
	Read Kind = iota
	// Write changes state or sends something, but is easily undone or harmless.
	Write
	// Destructive is outward-facing or hard to undo (block, leave, join, settings changes).
	Destructive
)

// Op is one operation available over MCP and REST.
type Op struct {
	Name, Title, Description string
	Kind                     Kind

	inType reflect.Type
	addMCP func(srv *mcp.Server, svc *wa.Service, t *mcp.Tool)
	call   func(ctx context.Context, svc *wa.Service, body []byte) (any, error)
}

// define builds an Op whose input is decoded into In.
func define[In any](name, title, desc string, kind Kind, run func(ctx context.Context, svc *wa.Service, in In) (any, error)) Op {
	return Op{
		Name: name, Title: title, Description: desc, Kind: kind,
		inType: reflect.TypeFor[In](),
		addMCP: func(srv *mcp.Server, svc *wa.Service, t *mcp.Tool) {
			mcp.AddTool(srv, t, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
				out, err := run(ctx, svc, in)
				if err != nil {
					return nil, nil, err
				}
				b, err := json.MarshalIndent(out, "", "  ")
				if err != nil {
					return nil, nil, err
				}
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
			})
		},
		call: func(ctx context.Context, svc *wa.Service, body []byte) (any, error) {
			var in In
			if len(body) > 0 {
				if err := json.Unmarshal(body, &in); err != nil {
					return nil, fmt.Errorf("%w: invalid JSON: %v", wa.ErrInvalidInput, err)
				}
			}
			return run(ctx, svc, in)
		},
	}
}

// Tool returns the MCP tool definition with annotations derived from Kind.
func (o Op) Tool() *mcp.Tool {
	readOnly := o.Kind == Read
	destructive := o.Kind == Destructive
	t := &mcp.Tool{Name: o.Name, Title: o.Title, Description: o.Description,
		Annotations: &mcp.ToolAnnotations{Title: o.Title, ReadOnlyHint: readOnly}}
	if !readOnly {
		t.Annotations.DestructiveHint = &destructive
	}
	return t
}

// Register adds every op as an MCP tool.
func Register(srv *mcp.Server, svc *wa.Service) {
	for _, o := range All() {
		o.addMCP(srv, svc, o.Tool())
	}
}

// Validate checks a JSON body against the op's input schema (required
// fields, types), like the MCP SDK does for tool calls.
func (o Op) Validate(body []byte) error {
	schema, err := jsonschema.ForType(o.inType, &jsonschema.ForOptions{})
	if err != nil {
		return err
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return err
	}
	var v any = map[string]any{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &v); err != nil {
			return fmt.Errorf("%w: invalid JSON: %v", wa.ErrInvalidInput, err)
		}
	}
	if err := resolved.Validate(v); err != nil {
		return fmt.Errorf("%w: %v", wa.ErrInvalidInput, err)
	}
	return nil
}

// Call validates body and runs the op.
func (o Op) Call(ctx context.Context, svc *wa.Service, body []byte) (any, error) {
	if err := o.Validate(body); err != nil {
		return nil, err
	}
	return o.call(ctx, svc, body)
}

// IsInvalidInput reports whether err was caused by bad input.
func IsInvalidInput(err error) bool { return errors.Is(err, wa.ErrInvalidInput) }
