package csharpextract

import (
	"github.com/sailpoint-oss/cartographer/extract/authscope"
	"github.com/sailpoint-oss/cartographer/extract/specmodel"
)

func (r *Result) ToUnifiedResult() *specmodel.Result {
	ops := make([]*specmodel.Operation, 0, len(r.Operations))
	for _, op := range r.Operations {
		params := make([]*specmodel.Parameter, 0, len(op.Parameters))
		for _, p := range op.Parameters {
			params = append(params, &specmodel.Parameter{
				Name:     p.Name,
				In:       p.In,
				Type:     p.Type,
				Required: p.Required,
				File:     p.File,
				Line:     p.Line,
				Column:   p.Column,
			})
		}
		var security []specmodel.SecurityRequirement
		if len(op.Security) > 0 {
			scopes := authscope.Normalize(op.Security)
			if len(scopes) > 0 {
				security = append(security, specmodel.SecurityRequirement{Scheme: "oauth2", Scopes: scopes})
			}
		}
		ops = append(ops, &specmodel.Operation{
			Path:            op.Path,
			Method:          op.Method,
			OperationID:     op.OperationID,
			Summary:         op.Summary,
			Description:     op.Description,
			Tags:            op.Tags,
			Parameters:      params,
			RequestBodyType: op.RequestBodyType,
			ResponseType:    op.ResponseType,
			ResponseStatus:  op.ResponseStatus,
			ResponseHeaders: op.ResponseHeaders,
			Security:        security,
			File:            op.File,
			Line:            op.Line,
			Column:          op.Column,
		})
	}
	return &specmodel.Result{Operations: ops, Schemas: r.Schemas, Types: r.Types}
}
