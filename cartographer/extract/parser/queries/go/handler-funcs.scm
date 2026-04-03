;; Tree-sitter queries for Go HTTP handler function signatures.
;; Detects func(w http.ResponseWriter, r *http.Request).

(function_declaration
  name: (identifier) @func_name
  parameters: (parameter_list
    (parameter_declaration
      type: (qualified_type
        package: (package_identifier) @pkg
        name: (type_identifier) @type
        (#eq? @pkg "http")
        (#eq? @type "ResponseWriter")))
    (parameter_declaration
      type: (pointer_type
        (qualified_type
          package: (package_identifier) @pkg2
          name: (type_identifier) @type2
          (#eq? @pkg2 "http")
          (#eq? @type2 "Request")))))
  body: (block) @body) @handler
