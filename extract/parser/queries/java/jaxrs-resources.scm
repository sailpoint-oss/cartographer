;; Tree-sitter queries for JAX-RS resources.
;; Detects @Path, @GET, @POST, @PathParam, @QueryParam, etc.

(class_declaration
  (modifiers
    (marker_annotation
      name: (identifier) @path_annotation
      (#eq? @path_annotation "Path")))
  name: (identifier) @class_name
  body: (class_body) @class_body) @resource

(method_declaration
  (modifiers
    (marker_annotation
      name: (identifier) @http_method
      (#match? @http_method "^(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)$")))
  name: (identifier) @method_name
  parameters: (formal_parameters) @params
  type: (_) @return_type) @endpoint
