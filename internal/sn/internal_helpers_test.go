package sn

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/domain"
)

func TestWValueRendersAllJSONShapes(t *testing.T) {
	cases := map[string]string{
		`"text"`:                       "text",
		`7`:                            "7",
		`true`:                         "true",
		`{"value":"abc","link":"x"}`:   "abc",
		`{"value":4}`:                  "4",
		`null`:                         "",
		`[1,2]`:                        "",
		`{"display_value":"no value"}`: "",
	}
	for in, want := range cases {
		if got := wValue(json.RawMessage(in)); got != want {
			t.Errorf("wValue(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestWDecodeFirstAndOneRejectGarbage(t *testing.T) {
	if _, err := wDecodeFirst("incident", []byte("nope")); err == nil {
		t.Error("garbage list body must fail")
	}
	if _, err := wDecodeOne("incident", []byte(`{"result":null}`)); err == nil {
		t.Error("missing result must fail")
	}
}

func TestWRecordName(t *testing.T) {
	if wRecordName(domain.Record{Fields: map[string]string{"number": "INC1"}}, "id") != "INC1" || wRecordName(domain.Record{}, "id") != "id" {
		t.Fatal("number, else fallback")
	}
}

func TestSleepCtx(t *testing.T) {
	if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
}

func TestErrorTextAndHints(t *testing.T) {
	errs := []interface {
		error
		Category() output.Category
		Hint() string
	}{
		&NotFoundError{Status: 404, Message: "m"}, &ConflictError{Status: 409, Message: "m"}, &ConflictError{Status: 409, Message: "m", AppliedChange: true},
		&ValidationError{Status: 400, Message: "m"}, &APIError{Status: 418, Message: "m"}, &ServerError{Status: 500, Message: "m"},
		&ResponseTooLargeError{Limit: 8}, &ProducerConfigError{},
	}
	for _, e := range errs {
		if e.Error() == "" || e.Hint() == "" || e.Category() == "" {
			t.Errorf("%T: empty text, hint or category", e)
		}
	}
	for _, st := range []interface{ HTTPStatus() int }{&NotFoundError{Status: 1}, &ConflictError{Status: 1}, &ValidationError{Status: 1}, &APIError{Status: 1}, &ServerError{Status: 1}} {
		if st.HTTPStatus() != 1 {
			t.Errorf("%T status", st)
		}
	}
	if !strings.Contains((&ConflictError{AppliedChange: true}).Hint(), "already applied") {
		t.Error("applied conflict hint")
	}
}

func TestTransientSend(t *testing.T) {
	if transientSend(context.Background(), errors.New("plain")) {
		t.Error("an arbitrary error is not transient")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if transientSend(ctx, &ServerError{Status: 500}) {
		t.Error("a cancelled context is never transient")
	}
	if transientSend(context.Background(), context.DeadlineExceeded) {
		t.Error("deadline is final")
	}
}
