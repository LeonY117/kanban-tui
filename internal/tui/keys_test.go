package tui

import (
	"reflect"
	"testing"

	"github.com/charmbracelet/bubbles/key"
)

// A binding declared on keyMap but left out of the keys literal is silently
// dead: key.Matches never fires and the feature simply doesn't respond. That
// happened to Copy, so every field is checked here.
func TestEveryBindingHasKeys(t *testing.T) {
	v := reflect.ValueOf(keys)
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name
		binding, ok := v.Field(i).Interface().(key.Binding)
		if !ok {
			t.Fatalf("keyMap field %s is not a key.Binding", name)
		}
		if len(binding.Keys()) == 0 {
			t.Errorf("keyMap.%s has no keys — it was declared but never bound", name)
		}
		if !binding.Enabled() {
			t.Errorf("keyMap.%s is disabled", name)
		}
	}
}

// Two bindings answering to the same key make one of them unreachable in any
// view that handles both.
func TestBindingsDoNotCollide(t *testing.T) {
	// Bindings that intentionally share a key with another, because no view
	// handles both: none today.
	owner := map[string]string{}
	v := reflect.ValueOf(keys)
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name
		binding := v.Field(i).Interface().(key.Binding)
		for _, k := range binding.Keys() {
			if prev, taken := owner[k]; taken {
				t.Errorf("key %q is bound to both %s and %s", k, prev, name)
				continue
			}
			owner[k] = name
		}
	}
}
