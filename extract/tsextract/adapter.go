package tsextract

import (
	"github.com/sailpoint-oss/cartographer/extract/specmodel"
)

// ToUnifiedResult converts a TypeScript extraction Result into the unified specmodel.Result.
func (r *Result) ToUnifiedResult() *specmodel.Result {
	ops := make([]*specmodel.Operation, len(r.Operations))
	for i, op := range r.Operations {
		ops[i] = convertTSOperation(op)
	}
	return &specmodel.Result{
		Operations: ops,
		Schemas:    r.Schemas,
		Types:      r.Types,
	}
}

func convertTSOperation(op *Operation) *specmodel.Operation {
	params := make([]*specmodel.Parameter, len(op.Parameters))
	for i, p := range op.Parameters {
		params[i] = convertTSParam(p, op.File)
	}

	var security []specmodel.SecurityRequirement
	if op.RequiresAuth && len(op.Security) > 0 {
		// TS convention: first element is scheme name, rest are scopes
		scheme := op.Security[0]
		scopes := op.Security[1:]
		security = []specmodel.SecurityRequirement{
			{Scheme: scheme, Scopes: scopes},
		}
	}

	var annotated []specmodel.AnnotatedResponse
	for _, ar := range op.ApiResponses {
		annotated = append(annotated, specmodel.AnnotatedResponse{
			StatusCode:  ar.Status,
			Description: ar.Description,
			SchemaType:  ar.Type,
		})
	}

	return &specmodel.Operation{
		Path:                   op.Path,
		Method:                 op.Method,
		OperationID:            op.OperationID,
		Summary:                op.Summary,
		Description:            op.Description,
		Tags:                   op.Tags,
		Parameters:             params,
		RequestBodyType:        op.RequestBodyType,
		RequestBodyDescription: op.RequestBodyDescription,
		ResponseType:           op.ResponseType,
		ResponseStatus:         op.ResponseStatus,
		ReturnDescription:      op.ReturnDescription,
		Deprecated:             op.Deprecated,
		DeprecatedSince:        op.DeprecatedSince,
		Security:               security,
		ConsumesContentType:    op.ConsumesContentType,
		ProducesContentType:    op.ProducesContentType,
		ErrorResponses:         op.ErrorResponses,
		AnnotatedResponses:     annotated,
		ResponseHeaders:        op.ResponseHeaders,
		NullableResponse:       op.NullableResponse,
		RateLimited:            op.RateLimited,
		File:                   op.File,
		Line:                   op.Line,
		Column:                 op.Column,
	}
}

func convertTSParam(p *Parameter, defaultFile string) *specmodel.Parameter {
	file := defaultFile
	if p.File != "" {
		file = p.File
	}
	sp := &specmodel.Parameter{
		Name:         p.Name,
		In:           p.In,
		Type:         p.Type,
		Required:     p.Required,
		DefaultValue: p.DefaultValue,
		Description:  p.Description,
		Format:       p.Format,
		Pattern:      p.Pattern,
		Example:      p.Example,
		Enum:         p.Enum,
		File:         file,
		Line:         p.Line,
		Column:       p.Column,
	}
	if p.Minimum != nil {
		v := *p.Minimum
		sp.Minimum = &v
	}
	if p.Maximum != nil {
		v := *p.Maximum
		sp.Maximum = &v
	}
	if p.MinLength != nil {
		v := *p.MinLength
		sp.MinLength = &v
	}
	if p.MaxLength != nil {
		v := *p.MaxLength
		sp.MaxLength = &v
	}
	if p.MinItems != nil {
		v := *p.MinItems
		sp.MinItems = &v
	}
	if p.MaxItems != nil {
		v := *p.MaxItems
		sp.MaxItems = &v
	}
	return sp
}
