;; Tree-sitter queries for NestJS controllers.
;; Detects @Controller, @Get, @Post, @Put, @Delete, @Patch, @Param, @Body, @Query, etc.

;; Class-level controller decorator
(class_declaration
  (decorator
    (call_expression
      function: (identifier) @decorator_name
      (#match? @decorator_name "^(Controller|ApiTags)$")
      arguments: (arguments) @decorator_args))
  name: (type_identifier) @class_name
  body: (class_body) @class_body) @controller

;; Method-level HTTP decorators
(method_definition
  (decorator
    (call_expression
      function: (identifier) @http_method
      (#match? @http_method "^(Get|Post|Put|Delete|Patch|Head|Options|All)$")
      arguments: (arguments)? @method_args))
  name: (property_identifier) @method_name
  parameters: (formal_parameters) @params
  return_type: (type_annotation)? @return_type) @endpoint
