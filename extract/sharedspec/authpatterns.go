package sharedspec

import "strings"

// AuthFunctionNames is the framework-neutral set of function / decorator /
// annotation / attribute names that the extractors treat as auth-requirement
// markers. Any call or annotation whose simple name matches one of these is
// inspected for string-literal arguments; those literals are surfaced as
// OAuth2 scope requirements on the operation.
//
// The list is deliberately generic and covers the most common naming
// conventions across Go web frameworks, Spring Security, JAX-RS, FastAPI,
// Flask-Security, Django REST, NestJS guards/decorators, ASP.NET Core
// Authorize attributes, and similar libraries.
var AuthFunctionNames = []string{
	// Spring Security / JAX-RS / generic Java annotations.
	"PreAuthorize",
	"PostAuthorize",
	"Secured",
	"RolesAllowed",
	"PermitAll",
	"DenyAll",

	// ASP.NET Core / .NET attributes.
	"Authorize",
	"Authorise",

	// Generic "require / check / has" naming used across Go middleware,
	// Express middleware, NestJS guards, FastAPI dependencies, and custom
	// frameworks. Any call whose simple name equals one of these has its
	// string-literal arguments extracted.
	"RequireAuth",
	"RequireAuthentication",
	"RequireAuthorization",
	"RequireRights",
	"RequireRight",
	"RequireScope",
	"RequireScopes",
	"RequireRole",
	"RequireRoles",
	"RequirePermission",
	"RequirePermissions",
	"RequireClaim",
	"RequireClaims",
	"CheckScope",
	"CheckScopes",
	"CheckPermission",
	"CheckPermissions",
	"CheckRole",
	"CheckRoles",
	"HasScope",
	"HasScopes",
	"HasRole",
	"HasRoles",
	"HasPermission",
	"HasPermissions",
	"HasAuthority",
	"HasAnyAuthority",
	"HasAnyRole",

	// FastAPI / Flask-Security / Django REST decorator family.
	"Security",
	"Depends",
	"requires_scopes",
	"auth_required",
	"login_required",
	"roles_required",
	"permissions_required",
	"permission_classes",
	"authentication_classes",
	"permission_required",
	"OAuth2PasswordBearer",
	"OAuth2AuthorizationCodeBearer",

	// NestJS guards / decorators.
	"UseGuards",
	"Roles",
	"Scopes",
	"SetMetadata",
}

// authFunctionNameSet is the canonical lookup table built from
// AuthFunctionNames. The comparison is case-insensitive so we match e.g.
// "preAuthorize" alongside "PreAuthorize".
var authFunctionNameSet = func() map[string]struct{} {
	out := make(map[string]struct{}, len(AuthFunctionNames))
	for _, n := range AuthFunctionNames {
		out[strings.ToLower(n)] = struct{}{}
	}
	return out
}()

// IsAuthFunctionName reports whether name matches a known auth-requirement
// function / annotation / decorator. The comparison is case-insensitive and
// also accepts the common "*Middleware" naming suffix.
func IsAuthFunctionName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	low := strings.ToLower(name)
	if _, ok := authFunctionNameSet[low]; ok {
		return true
	}
	// "<word>AuthMiddleware" / "<word>AuthorizationMiddleware".
	if strings.HasSuffix(low, "authmiddleware") ||
		strings.HasSuffix(low, "authzmiddleware") ||
		strings.HasSuffix(low, "authorizationmiddleware") ||
		strings.HasSuffix(low, "authenticationmiddleware") {
		return true
	}
	// Final check: if the string contains both "auth" and "middleware" as substrings.
	if strings.Contains(low, "auth") && strings.Contains(low, "middleware") {
		return true
	}
	return false
}
