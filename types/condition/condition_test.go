package condition

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/types"
)

// mockCondition is a simple condition for testing that matches strings containing a substring.
type mockCondition struct {
	Substring string
	Called    bool
}

func (m *mockCondition) Match(_ context.Context, v string) bool {
	m.Called = true
	return strings.Contains(v, m.Substring)
}

func (m *mockCondition) String() string {
	return fmt.Sprintf("contains(%s)", m.Substring)
}

// --- And[T] tests ---

func TestAnd_EmptyMatchesTrue(t *testing.T) {
	var cond And[string]
	result := cond.Match(context.Background(), "anything")
	assert.True(t, result, "empty And should match true (vacuous truth)")
}

func TestAnd_SingleElementDelegates(t *testing.T) {
	m := &mockCondition{Substring: "hello"}
	cond := And[string]{m}

	assert.True(t, cond.Match(context.Background(), "hello world"))
	assert.True(t, m.Called)

	m.Called = false
	assert.False(t, cond.Match(context.Background(), "goodbye"))
	assert.True(t, m.Called)
}

func TestAnd_MultipleElementsShortCircuitOnFalse(t *testing.T) {
	first := &mockCondition{Substring: "nope"}
	second := &mockCondition{Substring: "hello"}
	cond := And[string]{first, second}

	result := cond.Match(context.Background(), "hello world")
	assert.False(t, result, "first condition fails so And should be false")
	assert.True(t, first.Called, "first condition should be evaluated")
	assert.False(t, second.Called, "second condition should NOT be evaluated (short-circuit)")
}

func TestAnd_AllTrue(t *testing.T) {
	first := &mockCondition{Substring: "hello"}
	second := &mockCondition{Substring: "world"}
	cond := And[string]{first, second}

	result := cond.Match(context.Background(), "hello world")
	assert.True(t, result)
	assert.True(t, first.Called)
	assert.True(t, second.Called)
}

func TestAnd_String_Empty(t *testing.T) {
	var cond And[string]
	// Empty And has no elements; String() returns "()" since join of empty is ""
	assert.Equal(t, "()", cond.String())
}

func TestAnd_String_SingleElement(t *testing.T) {
	m := &mockCondition{Substring: "x"}
	cond := And[string]{m}
	// Single element delegates to the element's String()
	assert.Equal(t, "contains(x)", cond.String())
}

func TestAnd_String_MultipleElements(t *testing.T) {
	m1 := &mockCondition{Substring: "a"}
	m2 := &mockCondition{Substring: "b"}
	cond := And[string]{m1, m2}
	assert.Equal(t, "(contains(a)&contains(b))", cond.String())
}

func TestAnd_Add(t *testing.T) {
	var cond And[string]
	m1 := &mockCondition{Substring: "a"}
	m2 := &mockCondition{Substring: "b"}

	ret := cond.Add(m1)
	require.Same(t, &cond, ret, "Add should return the same pointer")
	assert.Len(t, cond, 1)

	cond.Add(m2)
	assert.Len(t, cond, 2)

	assert.True(t, cond.Match(context.Background(), "ab"))
	assert.False(t, cond.Match(context.Background(), "a"))
}

// --- Or[T] tests ---

func TestOr_EmptyMatchesFalse(t *testing.T) {
	var cond Or[string]
	result := cond.Match(context.Background(), "anything")
	assert.False(t, result, "empty Or should match false")
}

func TestOr_SingleElementDelegates(t *testing.T) {
	m := &mockCondition{Substring: "hello"}
	cond := Or[string]{m}

	assert.True(t, cond.Match(context.Background(), "hello world"))
	assert.True(t, m.Called)

	m.Called = false
	assert.False(t, cond.Match(context.Background(), "goodbye"))
	assert.True(t, m.Called)
}

func TestOr_MultipleElementsShortCircuitOnTrue(t *testing.T) {
	first := &mockCondition{Substring: "hello"}
	second := &mockCondition{Substring: "world"}
	cond := Or[string]{first, second}

	result := cond.Match(context.Background(), "hello world")
	assert.True(t, result, "first condition passes so Or should be true")
	assert.True(t, first.Called, "first condition should be evaluated")
	assert.False(t, second.Called, "second condition should NOT be evaluated (short-circuit)")
}

func TestOr_AllFalse(t *testing.T) {
	first := &mockCondition{Substring: "nope"}
	second := &mockCondition{Substring: "nada"}
	cond := Or[string]{first, second}

	result := cond.Match(context.Background(), "hello world")
	assert.False(t, result)
	assert.True(t, first.Called)
	assert.True(t, second.Called)
}

func TestOr_String_Empty(t *testing.T) {
	var cond Or[string]
	assert.Equal(t, "()", cond.String())
}

func TestOr_String_SingleElement(t *testing.T) {
	m := &mockCondition{Substring: "x"}
	cond := Or[string]{m}
	assert.Equal(t, "contains(x)", cond.String())
}

func TestOr_String_MultipleElements(t *testing.T) {
	m1 := &mockCondition{Substring: "a"}
	m2 := &mockCondition{Substring: "b"}
	cond := Or[string]{m1, m2}
	assert.Equal(t, "(contains(a)|contains(b))", cond.String())
}

func TestOr_Add(t *testing.T) {
	var cond Or[string]
	m1 := &mockCondition{Substring: "a"}
	m2 := &mockCondition{Substring: "b"}

	ret := cond.Add(m1)
	require.Same(t, &cond, ret, "Add should return the same pointer")
	assert.Len(t, cond, 1)

	cond.Add(m2)
	assert.Len(t, cond, 2)

	assert.True(t, cond.Match(context.Background(), "a"))
	assert.True(t, cond.Match(context.Background(), "b"))
	assert.False(t, cond.Match(context.Background(), "c"))
}

// --- Not[T] tests ---

func TestNot_InvertsSingleCondition(t *testing.T) {
	m := &mockCondition{Substring: "hello"}
	cond := Not[string]{m}

	assert.False(t, cond.Match(context.Background(), "hello world"), "Not should invert true to false")
	assert.True(t, cond.Match(context.Background(), "goodbye"), "Not should invert false to true")
}

func TestNot_InvertsAndOfMultiple(t *testing.T) {
	m1 := &mockCondition{Substring: "hello"}
	m2 := &mockCondition{Substring: "world"}
	cond := Not[string]{m1, m2}

	// Both match: And returns true, Not inverts to false
	assert.False(t, cond.Match(context.Background(), "hello world"))

	// Only first matches: And returns false, Not inverts to true
	m1.Called = false
	m2.Called = false
	assert.True(t, cond.Match(context.Background(), "hello"))

	// Neither matches: And returns false, Not inverts to true
	assert.True(t, cond.Match(context.Background(), "goodbye"))
}

func TestNot_String_SingleElement(t *testing.T) {
	m := &mockCondition{Substring: "x"}
	cond := Not[string]{m}
	assert.Equal(t, "Not(contains(x))", cond.String())
}

func TestNot_String_MultipleElements(t *testing.T) {
	m1 := &mockCondition{Substring: "a"}
	m2 := &mockCondition{Substring: "b"}
	cond := Not[string]{m1, m2}
	assert.Equal(t, "Not((contains(a)&contains(b)))", cond.String())
}

// --- Static[T] tests ---

func TestStatic_True(t *testing.T) {
	cond := Static[string](true)
	assert.True(t, cond.Match(context.Background(), "anything"))
	assert.True(t, cond.Match(context.Background(), ""))
}

func TestStatic_False(t *testing.T) {
	cond := Static[string](false)
	assert.False(t, cond.Match(context.Background(), "anything"))
	assert.False(t, cond.Match(context.Background(), ""))
}

func TestStatic_String_True(t *testing.T) {
	cond := Static[string](true)
	assert.Equal(t, "true", cond.String())
}

func TestStatic_String_False(t *testing.T) {
	cond := Static[string](false)
	assert.Equal(t, "false", cond.String())
}

// --- Function[T] tests ---

func TestFunction_DelegatesToCallback(t *testing.T) {
	called := false
	fn := Function[string](func(ctx context.Context, v string) bool {
		called = true
		return v == "match"
	})

	assert.True(t, fn.Match(context.Background(), "match"))
	assert.True(t, called)

	called = false
	assert.False(t, fn.Match(context.Background(), "no match"))
	assert.True(t, called)
}

func TestFunction_String_IncludesPointer(t *testing.T) {
	fn := Function[string](func(ctx context.Context, v string) bool {
		return true
	})
	s := fn.String()
	assert.True(t, strings.HasPrefix(s, "<custom_function:"), "String should start with '<custom_function:'")
	assert.True(t, strings.HasSuffix(s, ">"), "String should end with '>'")
	// Verify it contains a hex pointer
	assert.Contains(t, s, "0x")
}

func TestFunction_NilMatchPanics(t *testing.T) {
	// A nil Function should panic when Match is called (nil function call).
	var fn Function[string]
	assert.Panics(t, func() {
		fn.Match(context.Background(), "test")
	})
}

// --- CombineConds tests ---

func TestCombineConds_EmptyReturnsNil(t *testing.T) {
	result := CombineConds[string]()
	assert.Nil(t, result, "CombineConds with no args should return nil")
}

func TestCombineConds_SinglePassthrough(t *testing.T) {
	m := &mockCondition{Substring: "test"}
	result := CombineConds[string](m)
	require.NotNil(t, result)
	// Should be the same condition, not wrapped
	assert.Equal(t, m, result, "CombineConds with one arg should return it directly")
}

func TestCombineConds_MultipleReturnsAnd(t *testing.T) {
	m1 := &mockCondition{Substring: "a"}
	m2 := &mockCondition{Substring: "b"}
	result := CombineConds[string](m1, m2)
	require.NotNil(t, result)

	// Should be of type And[string]
	andCond, ok := result.(And[string])
	require.True(t, ok, "CombineConds with multiple args should return And[string]")
	assert.Len(t, andCond, 2)
}

func TestCombineConds_MultipleMatchBehavior(t *testing.T) {
	m1 := &mockCondition{Substring: "hello"}
	m2 := &mockCondition{Substring: "world"}
	result := CombineConds[string](m1, m2)

	assert.True(t, result.Match(context.Background(), "hello world"))
	assert.False(t, result.Match(context.Background(), "hello"))
}

// --- Interface compliance tests ---

func TestAnd_ImplementsCondition(t *testing.T) {
	var _ types.Condition[string] = And[string]{}
}

func TestOr_ImplementsCondition(t *testing.T) {
	var _ types.Condition[string] = Or[string]{}
}

func TestNot_ImplementsCondition(t *testing.T) {
	var _ types.Condition[string] = Not[string]{}
}

func TestStatic_ImplementsCondition(t *testing.T) {
	var _ types.Condition[string] = Static[string](true)
}

func TestFunction_ImplementsCondition(t *testing.T) {
	var _ types.Condition[string] = Function[string](nil)
}

// --- Edge case: context propagation ---

func TestFunction_ContextPropagation(t *testing.T) {
	type ctxKey string
	key := ctxKey("test-key")
	ctx := context.WithValue(context.Background(), key, "test-value")

	fn := Function[string](func(ctx context.Context, v string) bool {
		val, ok := ctx.Value(key).(string)
		return ok && val == "test-value"
	})

	assert.True(t, fn.Match(ctx, "irrelevant"))
	assert.False(t, fn.Match(context.Background(), "irrelevant"))
}

// --- Edge case: And/Or with lots of conditions ---

func TestAnd_ManyConditions(t *testing.T) {
	var cond And[string]
	for i := 0; i < 10; i++ {
		cond = append(cond, Static[string](true))
	}
	assert.True(t, cond.Match(context.Background(), "test"))

	// Adding a single false should make entire And false
	cond = append(cond, Static[string](false))
	assert.False(t, cond.Match(context.Background(), "test"))
}

func TestOr_ManyConditions(t *testing.T) {
	var cond Or[string]
	for i := 0; i < 10; i++ {
		cond = append(cond, Static[string](false))
	}
	assert.False(t, cond.Match(context.Background(), "test"))

	// Adding a single true should make entire Or true
	cond = append(cond, Static[string](true))
	assert.True(t, cond.Match(context.Background(), "test"))
}

// --- Composability: nesting conditions ---

func TestNestedConditions(t *testing.T) {
	// (contains("hello") AND NOT(contains("bad"))) OR contains("override")
	containsHello := &mockCondition{Substring: "hello"}
	containsBad := &mockCondition{Substring: "bad"}
	containsOverride := &mockCondition{Substring: "override"}

	inner := And[string]{containsHello, Not[string]{containsBad}}
	outer := Or[string]{inner, containsOverride}

	assert.True(t, outer.Match(context.Background(), "hello world"), "hello without bad should match")
	assert.False(t, outer.Match(context.Background(), "hello bad"), "hello with bad should not match")
	assert.True(t, outer.Match(context.Background(), "override"), "override should match regardless")
	assert.False(t, outer.Match(context.Background(), "nothing"), "nothing should not match")
}
