package gui

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/olemoudi/kawarimi/internal/demo"
)

// The SPA iterates list fields straight off API responses, so a slice that
// marshals as JSON null (a nil Go slice) is a client crash waiting for its first
// pristine state — exactly how the demo's day-0 view once died. This marshals
// REAL freshly-constructed responses (the pristine states, where nil slices hide)
// and flags any slice/map field that came out null instead of empty.
func TestAPIResponsesMarshalListsAsArrays(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("KAWARIMI_TELEGRAM_API", "")
	t.Setenv("KAWARIMI_GITHUB_API", "")

	w, err := demo.NewWorld(demo.Options{ForceLocalEngine: true, Version: "test"})
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	t.Cleanup(func() { w.Close() })
	freshSnap, err := w.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	s := &server{opts: Options{Version: "test", Demo: w}, sess: &session{}, lastSeen: time.Now(), quit: make(chan struct{})}

	responses := map[string]any{
		"fresh demo.Snapshot": freshSnap,
		"fresh stateResponse": s.buildState(),
	}
	for name, resp := range responses {
		raw, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		typ := reflect.TypeOf(resp)
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		for _, field := range nullListFields(typ, string(raw)) {
			t.Errorf("%s: field %q marshals as null — initialize it so the SPA always gets an array", name, field)
		}
	}
}

// nullListFields returns the JSON names of slice/map fields (recursively, through
// nested structs) whose value is null in the marshaled JSON.
func nullListFields(typ reflect.Type, marshaled string) []string {
	var bad []string
	var walk func(t reflect.Type)
	walk = func(t reflect.Type) {
		if t.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			jsonName := strings.Split(f.Tag.Get("json"), ",")[0]
			if jsonName == "" || jsonName == "-" {
				continue
			}
			switch f.Type.Kind() {
			case reflect.Slice, reflect.Map:
				if strings.Contains(marshaled, `"`+jsonName+`":null`) {
					bad = append(bad, jsonName)
				}
			case reflect.Struct:
				walk(f.Type)
			}
		}
	}
	walk(typ)
	return bad
}
