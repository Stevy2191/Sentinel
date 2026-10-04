package netreport

import (
	"context"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// msgNetworkScopeTypes refuses a scope type a metrics report cannot take.
const msgNetworkScopeTypes = "scope_type must be one of: ports, port_roles, devices, sites"

// ValidateScope checks a network scope as requester: shape (counts, roles),
// every named subject visible (one FieldError for hidden and missing), and the
// metrics (known, not enum, 1..10).
func (b *Builder) ValidateScope(ctx context.Context, requester uuid.UUID, scopeType string, scope models.ReportScope) error {
	if !models.IsNetworkScope(scopeType) {
		return &FieldError{Field: "scope_type", Message: msgNetworkScopeTypes}
	}
	if err := scope.Validate(scopeType); err != nil {
		return &FieldError{Field: "scope_data", Message: err.Error()}
	}
	res, err := b.resolve(ctx, requester, scopeType, scope)
	if err != nil {
		return err
	}
	if res.unavailable > 0 {
		return notAvailable()
	}
	infos, err := b.lookupMetrics(ctx, scope.Metrics)
	if err != nil {
		return err
	}
	return checkMetrics(scopeType, scope.Metrics, infos)
}

// notAvailable is the one error for hidden and missing subjects, the same
// from ValidateScope and Preview.
func notAvailable() *FieldError {
	return &FieldError{Field: "scope_data", Message: MsgNotAvailable}
}
