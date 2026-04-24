;; Tree-sitter queries for Spring Boot controllers.
;; Detects @RestController, @Controller, @GetMapping, @PostMapping, etc.

;; Controller class with marker annotation (no arguments)
(class_declaration
  (modifiers
    (marker_annotation
      name: (identifier) @annotation
      (#match? @annotation "^(RestController|Controller)$")))
  name: (identifier) @class_name) @controller

;; HTTP method mappings with annotation arguments
(method_declaration
  (modifiers
    (annotation
      name: (identifier) @http_method
      (#match? @http_method "^(GetMapping|PostMapping|PutMapping|DeleteMapping|PatchMapping|RequestMapping)$")))
  name: (identifier) @method_name
  parameters: (formal_parameters) @params) @endpoint

;; HTTP method mappings with marker annotation (no arguments)
(method_declaration
  (modifiers
    (marker_annotation
      name: (identifier) @http_method_marker
      (#match? @http_method_marker "^(GetMapping|PostMapping|PutMapping|DeleteMapping|PatchMapping)$")))
  name: (identifier) @method_name_marker
  parameters: (formal_parameters) @params_marker) @endpoint_marker
