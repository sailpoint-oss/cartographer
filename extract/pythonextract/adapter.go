package pythonextract

import (
	"github.com/sailpoint-oss/cartographer/extract/authscope"
	"github.com/sailpoint-oss/cartographer/extract/specmodel"
)

// ToUnifiedResult converts a Python extraction Result into the unified
// specmodel.Result used by the shared OpenAPI generator.
func (r *Result) ToUnifiedResult() *specmodel.Result {
	ops := make([]*specmodel.Operation, len(r.Operations))
	for i, op := range r.Operations {
		ops[i] = convertPythonOperation(op)
	}
	return &specmodel.Result{
		Operations: ops,
		Schemas:    r.Schemas,
		Types:      r.Types,
	}
}

func convertPythonOperation(op *Operation) *specmodel.Operation {
	params := make([]*specmodel.Parameter, len(op.Parameters))
	for i, p := range op.Parameters {
		params[i] = convertPythonParam(p, op.File)
	}

	var security []specmodel.SecurityRequirement
	if op.RequiresAuth && len(op.Security) > 0 {
		// op.Security from the Python extractor is `[scheme, token1, token2, ...]`.
		// If only a scheme is present (length 1) we treat that as a bare scheme
		// requirement (no scopes); otherwise everything after the first element
		// is a scope token and the scheme defaults to oauth2.
		scheme := op.Security[0]
		tokens := op.Security[1:]
		if len(tokens) == 0 {
			tokens = []string{scheme}
			scheme = "oauth2"
		}
		scopes := authscope.Normalize(tokens)
		if len(scopes) > 0 {
			security = []specmodel.SecurityRequirement{
				{Scheme: scheme, Scopes: scopes},
			}
		}
	} else if op.RequiresAuth {
		security = []specmodel.SecurityRequirement{
			{Scheme: "bearerAuth", Scopes: nil},
		}
	}

	return &specmodel.Operation{
		Path:                op.Path,
		Method:              op.Method,
		OperationID:         op.OperationID,
		Summary:             op.Summary,
		Description:         op.Description,
		Tags:                op.Tags,
		Parameters:          params,
		RequestBodyType:     op.RequestBodyType,
		ResponseType:        op.ResponseType,
		ResponseStatus:      op.ResponseStatus,
		Deprecated:          op.Deprecated,
		Security:            security,
		ConsumesContentType: op.ConsumesContentType,
		ProducesContentType: op.ProducesContentType,
		ResponseHeaders:     op.ResponseHeaders,
		File:                op.File,
		Line:                op.Line,
		Column:              op.Column,
	}
}

func convertPythonParam(p *Parameter, defaultFile string) *specmodel.Parameter {
	file := defaultFile
	if p.File != "" {
		file = p.File
	}
	return &specmodel.Parameter{
		Name:         p.Name,
		In:           p.In,
		Type:         p.Type,
		Required:     p.Required,
		DefaultValue: p.DefaultValue,
		Description:  p.Description,
		Format:       p.Format,
		File:         file,
		Line:         p.Line,
		Column:       p.Column,
	}
}
