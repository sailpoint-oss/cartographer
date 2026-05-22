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
	var rights []string
	if op.RequiresAuth && len(op.Security) > 0 {
		scheme := op.Security[0]
		tokens := op.Security[1:]
		if len(tokens) == 0 {
			tokens = []string{scheme}
			scheme = "oauth2"
		}
		var oauthScopes []string
		rights, oauthScopes = authscope.PartitionTokens(tokens, nil)
		if len(oauthScopes) > 0 {
			security = []specmodel.SecurityRequirement{
				{Scheme: scheme, Scopes: oauthScopes},
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
		Rights:              rights,
		ConsumesContentType: op.ConsumesContentType,
		ProducesContentType: op.ProducesContentType,
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
