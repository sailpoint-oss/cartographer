package javaextract

import (
	"github.com/sailpoint-oss/cartographer/extract/specmodel"
)

// ToUnifiedResult converts a Java extraction Result into the unified specmodel.Result.
func (r *Result) ToUnifiedResult() *specmodel.Result {
	ops := make([]*specmodel.Operation, len(r.Operations))
	for i, op := range r.Operations {
		ops[i] = convertJavaOperation(op)
	}
	return &specmodel.Result{
		Operations: ops,
		Schemas:    r.Schemas,
		Types:      r.Types,
	}
}

func convertJavaOperation(op *Operation) *specmodel.Operation {
	params := make([]*specmodel.Parameter, len(op.Parameters))
	for i, p := range op.Parameters {
		params[i] = convertJavaParam(p, op.File)
	}

	var formParams []*specmodel.Parameter
	for _, p := range op.FormParams {
		formParams = append(formParams, convertJavaParam(p, op.File))
	}

	var security []specmodel.SecurityRequirement
	if len(op.Security) > 0 {
		security = []specmodel.SecurityRequirement{
			{Scheme: "oauth2", Scopes: op.Security},
		}
	}

	var annotated []specmodel.AnnotatedResponse
	for _, ar := range op.AnnotatedResponses {
		annotated = append(annotated, specmodel.AnnotatedResponse{
			StatusCode:  ar.StatusCode,
			Description: ar.Description,
			SchemaType:  ar.SchemaType,
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
		Deprecated:             op.Deprecated,
		DeprecatedSince:        op.DeprecatedSince,
		Security:               security,
		ConsumesContentType:    op.ConsumesContentType,
		ProducesContentType:    op.ProducesContentType,
		ReturnDescription:      op.ReturnDescription,
		ErrorResponses:         op.ErrorResponses,
		AnnotatedResponses:     annotated,
		ResponseHeaders:        op.ResponseHeaders,
		NullableResponse:       op.NullableResponse,
		RateLimited:            op.RateLimited,
		FormParams:             formParams,
		File:                   op.File,
		Line:                   op.Line,
		Column:                 op.Column,
	}
}

func convertJavaParam(p *Parameter, defaultFile string) *specmodel.Parameter {
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
