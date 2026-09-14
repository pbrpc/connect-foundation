package errors

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/proto"

	"git.sonicoriginal.software/grpc-testing/mocks/tracer"
)

// connectError fails the test unless err is a *connect.Error carrying code.
func connectError(t *testing.T, err error, code connect.Code) *connect.Error {
	t.Helper()

	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		t.Fatalf("error = %v, want a *connect.Error", err)
	}
	if connectErr.Code() != code {
		t.Errorf("code = %v, want %v", connectErr.Code(), code)
	}

	return connectErr
}

// details decodes every detail on err, in order.
func details(t *testing.T, err *connect.Error) []proto.Message {
	t.Helper()

	var messages []proto.Message

	for _, detail := range err.Details() {
		message, unmarshalErr := connectproto.UnmarshalErrorDetail(detail)
		if unmarshalErr != nil {
			t.Fatalf("detail %s did not decode: %v", detail.Type, unmarshalErr)
		}

		messages = append(messages, message)
	}

	return messages
}

func TestInvalidArgument(t *testing.T) {
	t.Run("without violations", func(t *testing.T) {
		tt, ctx := tracer.New(t)
		defer tt.Shutdown(t)

		err := connectError(t, InvalidArgument(ctx, "validation failed"), connect.CodeInvalidArgument)
		if err.Message() != "validation failed" {
			t.Errorf("message = %q, want %q", err.Message(), "validation failed")
		}
		if len(err.Details()) != 0 {
			t.Errorf("details = %v, want none", err.Details())
		}

		if events := tt.EndSpanAndGetEvents(t); len(events) > 0 {
			t.Error("expected no span events without violations")
		}
	})

	t.Run("with violations", func(t *testing.T) {
		tt, ctx := tracer.New(t)
		defer tt.Shutdown(t)

		err := connectError(t, InvalidArgument(ctx, "validation failed",
			FieldViolation{Field: "email", Description: "invalid format"},
			FieldViolation{Field: "name", Description: "required"},
		), connect.CodeInvalidArgument)

		decoded := details(t, err)
		if len(decoded) != 1 {
			t.Fatalf("details = %v, want one BadRequest", decoded)
		}

		badRequest, ok := decoded[0].(*errdetails.BadRequest)
		if !ok {
			t.Fatalf("detail = %T, want *errdetails.BadRequest", decoded[0])
		}
		if len(badRequest.FieldViolations) != 2 {
			t.Fatalf("violations = %v, want two", badRequest.FieldViolations)
		}
		if badRequest.FieldViolations[0].Field != "email" {
			t.Errorf("field = %q, want email", badRequest.FieldViolations[0].Field)
		}

		events := tt.EndSpanAndGetEvents(t)
		if len(events) != 2 {
			t.Fatalf("expected 2 span events, got %d", len(events))
		}
		if events[0].Name != "validation_error" {
			t.Errorf("expected event name 'validation_error', got %q", events[0].Name)
		}
		assertAttribute(t, events[0].Attributes, "field", "email")
		assertAttribute(t, events[0].Attributes, "description", "invalid format")
	})
}

func TestAlreadyExists(t *testing.T) {
	tt, ctx := tracer.New(t)
	defer tt.Shutdown(t)

	err := connectError(
		t, AlreadyExists(ctx, "user already exists", "email", "test@example.com"), connect.CodeAlreadyExists,
	)

	decoded := details(t, err)
	if len(decoded) != 1 {
		t.Fatalf("details = %v, want one PreconditionFailure", decoded)
	}

	failure, ok := decoded[0].(*errdetails.PreconditionFailure)
	if !ok {
		t.Fatalf("detail = %T, want *errdetails.PreconditionFailure", decoded[0])
	}
	if failure.Violations[0].Subject != "email" {
		t.Errorf("subject = %q, want email", failure.Violations[0].Subject)
	}

	events := tt.EndSpanAndGetEvents(t)
	if len(events) != 1 {
		t.Fatalf("expected 1 span event, got %d", len(events))
	}
	if events[0].Name != "already_exists" {
		t.Errorf("expected event name 'already_exists', got %q", events[0].Name)
	}
	assertAttribute(t, events[0].Attributes, "field", "email")
	assertAttribute(t, events[0].Attributes, "conflict_value", "test@example.com")
}

func TestNotFound(t *testing.T) {
	tt, ctx := tracer.New(t)
	defer tt.Shutdown(t)

	err := connectError(t, NotFound(ctx, "User", "123"), connect.CodeNotFound)
	if err.Message() != "User not found" {
		t.Errorf("message = %q, want %q", err.Message(), "User not found")
	}

	decoded := details(t, err)
	if len(decoded) != 1 {
		t.Fatalf("details = %v, want one ResourceInfo", decoded)
	}

	info, ok := decoded[0].(*errdetails.ResourceInfo)
	if !ok {
		t.Fatalf("detail = %T, want *errdetails.ResourceInfo", decoded[0])
	}
	if info.ResourceName != "123" {
		t.Errorf("resource name = %q, want 123", info.ResourceName)
	}

	events := tt.EndSpanAndGetEvents(t)
	if len(events) != 1 {
		t.Fatalf("expected 1 span event, got %d", len(events))
	}
	if events[0].Name != "not_found" {
		t.Errorf("expected event name 'not_found', got %q", events[0].Name)
	}
	assertAttribute(t, events[0].Attributes, "resource_type", "User")
	assertAttribute(t, events[0].Attributes, "resource_id", "123")
}

func TestRateLimited(t *testing.T) {
	t.Run("with custom message", func(t *testing.T) {
		tt, ctx := tracer.New(t)
		defer tt.Shutdown(t)

		err := connectError(t, RateLimited(ctx, "too many requests", 5*time.Second), connect.CodeResourceExhausted)
		if err.Message() != "too many requests" {
			t.Errorf("message = %q, want %q", err.Message(), "too many requests")
		}

		decoded := details(t, err)
		if len(decoded) != 2 {
			t.Fatalf("details = %v, want RetryInfo and QuotaFailure", decoded)
		}

		retry, ok := decoded[0].(*errdetails.RetryInfo)
		if !ok {
			t.Fatalf("detail = %T, want *errdetails.RetryInfo", decoded[0])
		}
		if retry.RetryDelay.AsDuration() != 5*time.Second {
			t.Errorf("retry delay = %v, want 5s", retry.RetryDelay.AsDuration())
		}
		if _, ok := decoded[1].(*errdetails.QuotaFailure); !ok {
			t.Fatalf("detail = %T, want *errdetails.QuotaFailure", decoded[1])
		}

		events := tt.EndSpanAndGetEvents(t)
		if len(events) != 1 {
			t.Fatalf("expected 1 span event, got %d", len(events))
		}
		if events[0].Name != "rate_limited" {
			t.Errorf("expected event name 'rate_limited', got %q", events[0].Name)
		}
		assertAttribute(t, events[0].Attributes, "message", "too many requests")
		assertInt64Attribute(t, events[0].Attributes, "retry_after_ms", 5000)
	})

	t.Run("with empty message", func(t *testing.T) {
		tt, ctx := tracer.New(t)
		defer tt.Shutdown(t)

		err := connectError(t, RateLimited(ctx, "", time.Second), connect.CodeResourceExhausted)
		if err.Message() != "rate limit exceeded" {
			t.Errorf("expected default message, got %q", err.Message())
		}
	})
}

func TestPreconditionFailed(t *testing.T) {
	tt, ctx := tracer.New(t)
	defer tt.Shutdown(t)

	err := connectError(
		t,
		PreconditionFailed(ctx, "cannot delete", "STATE_INVALID", "resource", "must be inactive"),
		connect.CodeFailedPrecondition,
	)

	decoded := details(t, err)
	if len(decoded) != 1 {
		t.Fatalf("details = %v, want one PreconditionFailure", decoded)
	}

	failure, ok := decoded[0].(*errdetails.PreconditionFailure)
	if !ok {
		t.Fatalf("detail = %T, want *errdetails.PreconditionFailure", decoded[0])
	}
	if failure.Violations[0].Type != "STATE_INVALID" {
		t.Errorf("type = %q, want STATE_INVALID", failure.Violations[0].Type)
	}

	events := tt.EndSpanAndGetEvents(t)
	if len(events) != 1 {
		t.Fatalf("expected 1 span event, got %d", len(events))
	}
	if events[0].Name != "precondition_failed" {
		t.Errorf("expected event name 'precondition_failed', got %q", events[0].Name)
	}
	assertAttribute(t, events[0].Attributes, "violation_type", "STATE_INVALID")
	assertAttribute(t, events[0].Attributes, "subject", "resource")
	assertAttribute(t, events[0].Attributes, "description", "must be inactive")
}

func TestInternal(t *testing.T) {
	tt, ctx := tracer.New(t)
	defer tt.Shutdown(t)

	err := connectError(
		t, Internal(ctx, "something went wrong", "DATABASE_ERROR", "myapp.storage"), connect.CodeInternal,
	)

	decoded := details(t, err)
	if len(decoded) != 1 {
		t.Fatalf("details = %v, want one ErrorInfo", decoded)
	}

	info, ok := decoded[0].(*errdetails.ErrorInfo)
	if !ok {
		t.Fatalf("detail = %T, want *errdetails.ErrorInfo", decoded[0])
	}
	if info.Reason != "DATABASE_ERROR" {
		t.Errorf("reason = %q, want DATABASE_ERROR", info.Reason)
	}

	events := tt.EndSpanAndGetEvents(t)
	if len(events) != 1 {
		t.Fatalf("expected 1 span event, got %d", len(events))
	}
	if events[0].Name != "internal_error" {
		t.Errorf("expected event name 'internal_error', got %q", events[0].Name)
	}
	assertAttribute(t, events[0].Attributes, "reason", "DATABASE_ERROR")
	assertAttribute(t, events[0].Attributes, "domain", "myapp.storage")
}

func TestWithHelp(t *testing.T) {
	t.Run("with Connect error", func(t *testing.T) {
		tt, ctx := tracer.New(t)
		defer tt.Shutdown(t)

		link := &errdetails.Help_Link{Description: "docs", Url: "https://example.com/docs"}

		err := connectError(t, WithHelp(InvalidArgument(ctx, "validation failed"), link), connect.CodeInvalidArgument)

		decoded := details(t, err)
		if len(decoded) != 1 {
			t.Fatalf("details = %v, want one Help", decoded)
		}

		help, ok := decoded[0].(*errdetails.Help)
		if !ok {
			t.Fatalf("detail = %T, want *errdetails.Help", decoded[0])
		}
		if help.Links[0].Url != link.Url {
			t.Errorf("url = %q, want %q", help.Links[0].Url, link.Url)
		}
	})

	t.Run("with non-Connect error", func(t *testing.T) {
		if err := WithHelp(context.DeadlineExceeded); err != context.DeadlineExceeded {
			t.Error("expected original error to be returned")
		}
	})
}

func TestWithLocalizedMessage(t *testing.T) {
	t.Run("with Connect error", func(t *testing.T) {
		tt, ctx := tracer.New(t)
		defer tt.Shutdown(t)

		err := connectError(
			t,
			WithLocalizedMessage(InvalidArgument(ctx, "validation failed"), "es", "validación fallida"),
			connect.CodeInvalidArgument,
		)

		decoded := details(t, err)
		if len(decoded) != 1 {
			t.Fatalf("details = %v, want one LocalizedMessage", decoded)
		}

		localized, ok := decoded[0].(*errdetails.LocalizedMessage)
		if !ok {
			t.Fatalf("detail = %T, want *errdetails.LocalizedMessage", decoded[0])
		}
		if localized.Locale != "es" {
			t.Errorf("locale = %q, want es", localized.Locale)
		}
	})

	t.Run("with non-Connect error", func(t *testing.T) {
		err := WithLocalizedMessage(context.DeadlineExceeded, "es", "tiempo agotado")
		if err != context.DeadlineExceeded {
			t.Error("expected original error to be returned")
		}
	})
}

func assertAttribute(t *testing.T, attrs []attribute.KeyValue, key, expected string) {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			if attr.Value.AsString() != expected {
				t.Errorf("attribute %q: expected %q, got %q", key, expected, attr.Value.AsString())
			}
			return
		}
	}
	t.Errorf("attribute %q not found", key)
}

func assertInt64Attribute(t *testing.T, attrs []attribute.KeyValue, key string, expected int64) {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			if attr.Value.AsInt64() != expected {
				t.Errorf("attribute %q: expected %d, got %d", key, expected, attr.Value.AsInt64())
			}
			return
		}
	}
	t.Errorf("attribute %q not found", key)
}
