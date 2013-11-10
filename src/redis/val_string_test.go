package redis

import (
	"testing"
	"time"

	"store"
	"testing_helpers"
)

// tests the string value
func TestStringValue(t *testing.T) {
	s := setupRedis()
	src := NewString("blake", time.Now())

	b, err := s.SerializeValue(src)
	if err != nil {
		t.Fatalf("Unexpected serialization error: %v", err)
	}

	val, vtype, err := s.DeserializeValue(b)
	if err != nil {
		t.Fatalf("Unexpected deserialization error: %v", err)
	}
	if vtype != STRING_VALUE {
		t.Fatalf("Unexpected value type enum: %v", vtype)
	}
	dst, ok := val.(*String)
	if !ok {
		t.Fatalf("Unexpected value type: %T", val)
	}

	testing_helpers.AssertEqual(t, "data", src.data, dst.data)
	testing_helpers.AssertEqual(t, "time", src.time, dst.time)
}

// tests that the tombstone struct satisfies the
// value interface
func TestStringInterface(_ *testing.T) {
	func (store.Value){}(NewTombstone(time.Now()))
}

// tests that mismatched values are reconciled and
// corrected as expected
func TestMismatchReconciliation(t *testing.T) {
	ts0 := time.Now()
	ts1 := ts0.Add(time.Duration(-3000))
	expected := NewString("a", ts0)
	vmap := map[string]store.Value {
		"0": expected,
		"1": NewString("b", ts1),
		"2": expected,
	}

	actual, adjustments, err := reconcileString("k", vmap)

	if err != nil {
		t.Fatalf("unexpected reconciliation error: %v", err)
	}

	// TODO: use value equal method instead
	testing_helpers.AssertEqual(t, "reconciled value", *expected, *actual)
	testing_helpers.AssertEqual(t, "adjustment size", 1, len(adjustments))

	instructions, ok := adjustments["1"]
	if !ok {
		t.Fatalf("instruction set for '1' not found")
	}
	testing_helpers.AssertEqual(t, "num instructions", 1, len(instructions))

	instruction := instructions[0]
	expected_instr := store.Instruction{Cmd:"SET", Key:"k", Args:[]string{"a"}, Timestamp:ts0}
	if !expected_instr.Equal(instruction) {
		t.Fatalf("unexpected instruction value. Expected: [%v], got: [%v]", expected_instr, instruction)
	}
}

// should set values of different types to the value
// with the largest timestamp
func TestMultiTypeReconciliation(t *testing.T) {
	ts0 := time.Now()
	ts1 := ts0.Add(time.Duration(-3000))
	expected := NewString("a", ts0)
	vmap := map[string]store.Value {
		"0": expected,
		"1": NewTombstone(ts1),
	}

	actual, adjustments, err := reconcileString("k", vmap)

	if err != nil {
		t.Fatalf("unexpected reconciliation error: %v", err)
	}

	// TODO: use value equal method instead
	testing_helpers.AssertEqual(t, "reconciled value", *expected, *actual)
	testing_helpers.AssertEqual(t, "adjustment size", 1, len(adjustments))

	instructions, ok := adjustments["1"]
	if !ok {
		t.Fatalf("instruction set for '1' not found")
	}
	testing_helpers.AssertEqual(t, "num instructions", 1, len(instructions))

	instruction := instructions[0]
	expected_instr := store.Instruction{Cmd:"SET", Key:"k", Args:[]string{"a"}, Timestamp:ts0}
	if !expected_instr.Equal(instruction) {
		t.Fatalf("unexpected instruction value. Expected: [%v], got: [%v]", expected_instr, instruction)
	}
}

// should return the correct value and no adjustment
// instructions if all of the values match
func TestNoOpReconciliation(t *testing.T) {
	ts0 := time.Now()
	expected := NewString("a", ts0)
	vmap := map[string]store.Value {
		"0": expected,
		"1": expected,
		"2": expected,
	}

	actual, adjustments, err := reconcileString("k", vmap)

	if err != nil {
		t.Fatalf("unexpected reconciliation error: %v", err)
	}

	// TODO: use value equal method instead
	testing_helpers.AssertEqual(t, "reconciled value", *expected, *actual)
	testing_helpers.AssertEqual(t, "adjustment size", 0, len(adjustments))
}
