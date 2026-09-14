//revive:disable:package-comments
package errors

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

// FieldViolation represents a validation error for a specific field
type FieldViolation struct {
	Field       string
	Description string
}

// withDetail attaches msg to err as a serialized detail. Packing a generated
// errdetails message cannot fail, so the packing error is not consulted; a
// nil detail is ignored by WithDetail either way.
func withDetail(err *connect.Error, msg proto.Message) *connect.Error {
	detail, _ := connectproto.NewErrorDetail(msg)

	return err.WithDetail(detail)
}

// InvalidArgument returns a Connect error with structured field violations
// Use this for validation errors where specific fields failed validation
func InvalidArgument(ctx context.Context, message string, violations ...FieldViolation) error {
	err := connect.NewError(connect.CodeInvalidArgument, message)

	if len(violations) > 0 {
		fieldViolations := make([]*errdetails.BadRequest_FieldViolation, len(violations))
		for i, v := range violations {
			fieldViolations[i] = &errdetails.BadRequest_FieldViolation{
				Field:       v.Field,
				Description: v.Description,
			}
			// Record span event for each field violation
			trace.SpanFromContext(ctx).AddEvent("validation_error", trace.WithAttributes(
				attribute.String("field", v.Field),
				attribute.String("description", v.Description),
			))
		}

		err = withDetail(err, &errdetails.BadRequest{FieldViolations: fieldViolations})
	}

	return err
}

// AlreadyExists returns a Connect error indicating a resource already exists
// Includes precondition failure details for the conflicting field
func AlreadyExists(ctx context.Context, message string, field string, conflictValue string) error {
	err := connect.NewError(connect.CodeAlreadyExists, message)

	err = withDetail(err, &errdetails.PreconditionFailure{
		Violations: []*errdetails.PreconditionFailure_Violation{
			{
				Type:        "RESOURCE_ALREADY_EXISTS",
				Subject:     field,
				Description: message + ": " + conflictValue,
			},
		},
	})

	trace.SpanFromContext(ctx).AddEvent("already_exists", trace.WithAttributes(
		attribute.String("field", field),
		attribute.String("conflict_value", conflictValue),
	))

	return err
}

// NotFound returns a Connect error indicating a resource was not found
// Includes resource info for the missing resource
func NotFound(ctx context.Context, resourceType string, resourceID string) error {
	message := resourceType + " not found"
	err := connect.NewError(connect.CodeNotFound, message)

	err = withDetail(err, &errdetails.ResourceInfo{
		ResourceType: resourceType,
		ResourceName: resourceID,
		Description:  message,
	})

	trace.SpanFromContext(ctx).AddEvent("not_found", trace.WithAttributes(
		attribute.String("resource_type", resourceType),
		attribute.String("resource_id", resourceID),
	))

	return err
}

// RateLimited returns a Connect error indicating rate limiting
// Includes retry information with the recommended retry delay
func RateLimited(ctx context.Context, message string, retryAfter time.Duration) error {
	if message == "" {
		message = "rate limit exceeded"
	}
	err := connect.NewError(connect.CodeResourceExhausted, message)

	err = withDetail(err, &errdetails.RetryInfo{
		RetryDelay: durationpb.New(retryAfter),
	})

	err = withDetail(err, &errdetails.QuotaFailure{
		Violations: []*errdetails.QuotaFailure_Violation{
			{
				Subject:     "rate_limit",
				Description: message,
			},
		},
	})

	trace.SpanFromContext(ctx).AddEvent("rate_limited", trace.WithAttributes(
		attribute.String("message", message),
		attribute.Int64("retry_after_ms", retryAfter.Milliseconds()),
	))

	return err
}

// PreconditionFailed returns a Connect error for failed preconditions
// Use when an operation cannot proceed due to the current state of the resource
func PreconditionFailed(ctx context.Context, message string, violationType string, subject string, description string) error {
	err := connect.NewError(connect.CodeFailedPrecondition, message)

	err = withDetail(err, &errdetails.PreconditionFailure{
		Violations: []*errdetails.PreconditionFailure_Violation{
			{
				Type:        violationType,
				Subject:     subject,
				Description: description,
			},
		},
	})

	trace.SpanFromContext(ctx).AddEvent("precondition_failed", trace.WithAttributes(
		attribute.String("violation_type", violationType),
		attribute.String("subject", subject),
		attribute.String("description", description),
	))

	return err
}

// Internal returns a Connect internal error with debug info
// Includes error info with reason and domain for categorization
func Internal(ctx context.Context, message string, reason string, domain string) error {
	err := connect.NewError(connect.CodeInternal, message)

	err = withDetail(err, &errdetails.ErrorInfo{
		Reason: reason,
		Domain: domain,
	})

	trace.SpanFromContext(ctx).AddEvent("internal_error", trace.WithAttributes(
		attribute.String("reason", reason),
		attribute.String("domain", domain),
	))

	return err
}

// WithHelp adds help links to an existing error
// Useful for providing documentation links for common errors
func WithHelp(err error, links ...*errdetails.Help_Link) error {
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		return err
	}

	return withDetail(connectErr, &errdetails.Help{Links: links})
}

// WithLocalizedMessage adds a localized message to an existing error
// Useful for internationalization support
func WithLocalizedMessage(err error, locale string, localizedMessage string) error {
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		return err
	}

	return withDetail(connectErr, &errdetails.LocalizedMessage{
		Locale:  locale,
		Message: localizedMessage,
	})
}
