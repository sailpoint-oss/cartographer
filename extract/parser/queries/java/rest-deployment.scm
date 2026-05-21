;; Tree-sitter queries for plugin-registered JAX-RS applications.
;; Detects:
;;   new RestDeployment("/base/path", AppClass.class)
;;   addDeployment(new RestDeployment("/path", App.class))
;;   addRestDeployment(new RestDeployment("/path", App.class))
;;   context.getInstance(RestConfig.class).addDeployment(new RestDeployment(...))
;;
;; The result maps Application subclass names to their registered context path,
;; which lets JAX-RS resources with @Path("") inherit a proper prefix.

(object_creation_expression
  type: (type_identifier) @deployment_type
  (#eq? @deployment_type "RestDeployment")
  arguments: (argument_list) @deployment_args) @deployment_call

;; A class whose constructor registers resources via `add(ClassName.class)` calls.
;; The javaextract package cross-references these to app base paths.
(method_invocation
  name: (identifier) @add_method
  (#match? @add_method "^(add|register)$")
  arguments: (argument_list) @add_args) @add_call
