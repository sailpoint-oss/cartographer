;; Tree-sitter queries for Go router registration (gorilla/mux, chi).
;; Used when tree-sitter-based extraction is enabled (Phase 2+).

;; gorilla/mux: router.Handle(path, handler) or router.HandleFunc(path, handler)
(call_expression
  function: (selector_expression
    operand: (_) @router
    field: (field_identifier) @method_name
    (#match? @method_name "^(Handle|HandleFunc)$"))
  arguments: (argument_list
    (interpreted_string_literal) @path
    (_) @handler)) @route_call

;; chi: r.Get(path, handler), r.Post(path, handler), etc.
(call_expression
  function: (selector_expression
    operand: (_) @router
    field: (field_identifier) @method_name
    (#match? @method_name "^(Get|Post|Put|Delete|Patch|Head|Options)$"))
  arguments: (argument_list
    (interpreted_string_literal) @path
    (_) @handler)) @route_call
