package redis

import (
	"testing"
	"time"

	"testing_helpers"
	"store"
)

// table of instructions, and whether they're
// a write (true) or read (false)
var isWrite = []struct {
	cmd string
	result bool
}{
	{"GET", false},
	{"SET", true},
	{"DEL", true},
}

func TestIsWriteCmd(t *testing.T) {
	r := &Redis{}
	for _, c := range isWrite {
		if result := r.IsWriteCommand(c.cmd); result != c.result {
			if result {
				t.Errorf("%v erroneously identified as a write", c.cmd)
			} else {
				t.Errorf("%v not identified as a write", c.cmd)
			}
		}
	}
}

func TestIsReadCmd(t *testing.T) {
	r := &Redis{}
	for _, c := range isWrite {
		if result := r.IsReadCommand(c.cmd); result != !c.result {
			if result {
				t.Errorf("%v erroneously identified as a read", c.cmd)
			} else {
				t.Errorf("%v not identified as a read", c.cmd)
			}
		}
	}
}

func TestInterfaceIsImplemented(t *testing.T) {
	t.Skipf("Not working yet!")
	func(s store.Store) { _ = s }(&Redis{})
}

// ----------- data import / export -----------

func TestGetRawKeySuccess(t *testing.T) {
	r := setupRedis()
	expected, err := r.ExecuteWrite("SET", "a", []string{"b"}, time.Now())
	if err != nil {
		t.Fatalf("Unexpected error executing set: %v", err)
	}

	rval, err := r.GetRawKey("a")
	if err != nil {
		t.Fatal("unexpectedly got error: %v", err)
	}
	val, ok := rval.(*String)
	if !ok {
		t.Fatal("expected value of type stringValue, got %T", rval)
	}
	testing_helpers.AssertEqual(t, "value", expected, val)
}

func TestSetRawKey(t *testing.T) {
	r := setupRedis()
	expected := NewBoolean(true, time.Now())
	r.SetRawKey("x", expected)

	rval, exists := r.data["x"]
	if !exists {
		t.Fatalf("no value found for key 'x'")
	}
	val, ok := rval.(*Boolean)
	if !ok {
		t.Fatal("expected value of type boolValues, got %T", rval)
	}

	testing_helpers.AssertEqual(t, "val", expected, val)
}

func TestGetKeys(t *testing.T) {
	r := setupRedis()
	val := NewBoolean(true, time.Now())
	r.SetRawKey("x", val)
	r.SetRawKey("y", val)
	r.SetRawKey("z", val)

	// make set of expected keys
	expected := map[string]bool {"x": true, "y": true, "z": true}

	seen := make(map[string] bool)
	keys := r.GetKeys()
	testing_helpers.AssertEqual(t, "num keys", len(expected), len(keys))
	for _, key := range keys {
		if expected[key] && !seen[key] {
			seen[key] = true
		} else if seen[key] {
			t.Errorf("Key '%v' seen more than once", key)
		} else {
			t.Errorf("Unexpected key returned: '%v'", key)
		}
	}
}

func TestKeyExists(t *testing.T) {
	r := setupRedis()
	val := NewBoolean(true, time.Now())
	r.SetRawKey("x", val)

	testing_helpers.AssertEqual(t, "existing key", true, r.KeyExists("x"))
	testing_helpers.AssertEqual(t, "existing key", false, r.KeyExists("y"))
}

// tests that attempting to reconcile an empty map
// returns an error
func TestReconcilingEmptyMap(t *testing.T) {
	vmap := map[string]store.Value {}
	ractual, adjustments, err := setupRedis().Reconcile("k", vmap)

	if err == nil {
		t.Fatalf("expected reconciliation error")
	} else {
		t.Log(err)
	}
	testing_helpers.AssertEqual(t, "returned val", nil, ractual)
	testing_helpers.AssertEqual(t, "adjustment size", 0, len(adjustments))
}

// tests reconciling a value map with only a single element
func TestReconcileSingleValue(t *testing.T) {
	ts0 := time.Now()
	expected := NewString("a", ts0)
	vmap := map[string]store.Value { "0": expected }
	ractual, adjustments, err := setupRedis().Reconcile("k", vmap)

	if err != nil {
		t.Fatalf("unexpected reconciliation error: %v", err)
	}

	actual, ok := ractual.(*String)
	if !ok { t.Fatalf("Unexpected return value type: %T", ractual) }

	assertEqualValue(t, "reconciled value", expected, actual)
	testing_helpers.AssertEqual(t, "adjustment size", 0, len(adjustments))
}

