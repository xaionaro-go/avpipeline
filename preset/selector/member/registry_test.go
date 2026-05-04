package member_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
)

type testMember struct {
	metadata map[string]string
	liveKey  string
}

type testAllocator struct {
	next id.MemberID
}

func (a *testAllocator) Allocate(
	context.Context,
) (id.MemberID, error) {
	allocated := a.next
	a.next++
	return allocated, nil
}

func TestRegistryPutKeepsCallerOwnedIDsAuthoritative(t *testing.T) {
	ctx := context.Background()
	registry := member.NewRegistry[string, *testMember]()

	highPriority, err := registry.Put(ctx, id.MemberID(0), "camera", &testMember{
		metadata: map[string]string{"source": "camera"},
	})
	require.NoError(t, err)
	require.Equal(t, id.MemberID(0), highPriority.ID)
	require.NotZero(t, highPriority.Token)

	lowerPriority, err := registry.Put(ctx, id.MemberID(10), "rtmp", &testMember{
		metadata: map[string]string{"source": "rtmp"},
	})
	require.NoError(t, err)
	require.Equal(t, id.MemberID(10), lowerPriority.ID)

	loaded, ok := registry.LoadByID(ctx, id.MemberID(0))
	require.True(t, ok)
	require.Equal(t, highPriority.Token, loaded.Token)
	require.Equal(t, "camera", loaded.StorageKey)
}

func TestRegistryAcceptsNonComparableValues(t *testing.T) {
	ctx := context.Background()
	registry := member.NewRegistry[string, testMember]()

	entry, err := registry.Put(ctx, id.MemberID(1), "non-comparable", testMember{
		metadata: map[string]string{"value": "map makes this type non-comparable"},
	})
	require.NoError(t, err)

	loaded, ok := registry.LoadByID(ctx, entry.ID)
	require.True(t, ok)
	require.Equal(t, "map makes this type non-comparable", loaded.Value.metadata["value"])
}

func TestAllocatorKeepsFanoutIDAllocationSeparateFromRegistry(t *testing.T) {
	ctx := context.Background()
	allocator := &testAllocator{next: id.MemberID(100)}
	registry := member.NewRegistry[string, *testMember]()

	allocated, err := allocator.Allocate(ctx)
	require.NoError(t, err)

	entry, err := registry.Put(ctx, allocated, "sender", &testMember{
		metadata: map[string]string{"kind": "sender"},
	})
	require.NoError(t, err)
	require.Equal(t, id.MemberID(100), entry.ID)

	next, err := allocator.Allocate(ctx)
	require.NoError(t, err)
	require.Equal(t, id.MemberID(101), next)
}

func TestRegistryRejectsInvalidDuplicateIDAndDuplicateStorageKey(t *testing.T) {
	ctx := context.Background()
	registry := member.NewRegistry[string, *testMember]()

	_, err := registry.Put(ctx, id.MemberID(1), "stable", &testMember{})
	require.NoError(t, err)

	_, err = registry.Put(ctx, id.MemberID(1), "other", &testMember{})
	require.True(t, errors.Is(err, member.ErrDuplicateMemberID), "error: %v", err)

	_, err = registry.Put(ctx, id.MemberID(2), "stable", &testMember{})
	require.True(t, errors.Is(err, member.ErrDuplicateStorageKey), "error: %v", err)

	_, err = registry.Put(ctx, id.NoMemberID, "none", &testMember{})
	require.True(t, errors.Is(err, member.ErrInvalidMemberID), "error: %v", err)

	_, err = registry.Put(ctx, id.MemberID(-1), "negative", &testMember{})
	require.True(t, errors.Is(err, member.ErrInvalidMemberID), "error: %v", err)
}

func TestRegistryUnbindStorageKeyOnlyForExactEntryToken(t *testing.T) {
	ctx := context.Background()
	registry := member.NewRegistry[string, *testMember]()

	entry, err := registry.Put(ctx, id.MemberID(1), "stable", &testMember{})
	require.NoError(t, err)

	stale := entry
	stale.Token++
	require.False(t, registry.UnbindStorageKey(ctx, stale))

	loaded, ok := registry.LoadByStorageKey(ctx, "stable")
	require.True(t, ok)
	require.Equal(t, entry.Token, loaded.Token)

	require.True(t, registry.UnbindStorageKey(ctx, entry))

	_, ok = registry.LoadByStorageKey(ctx, "stable")
	require.False(t, ok)

	loaded, ok = registry.LoadByID(ctx, entry.ID)
	require.True(t, ok)
	require.Equal(t, entry.Token, loaded.Token)
}

func TestRegistryCompareAndDeletePreservesNewerEntryWithSameStorageKey(t *testing.T) {
	ctx := context.Background()
	registry := member.NewRegistry[string, *testMember]()

	oldEntry, err := registry.Put(ctx, id.MemberID(1), "stable", &testMember{})
	require.NoError(t, err)
	require.True(t, registry.UnbindStorageKey(ctx, oldEntry))

	newEntry, err := registry.Put(ctx, id.MemberID(2), "stable", &testMember{})
	require.NoError(t, err)

	require.True(t, registry.CompareAndDelete(ctx, oldEntry))

	_, ok := registry.LoadByID(ctx, oldEntry.ID)
	require.False(t, ok)

	loaded, ok := registry.LoadByStorageKey(ctx, "stable")
	require.True(t, ok)
	require.Equal(t, newEntry.Token, loaded.Token)

	require.False(t, registry.CompareAndDelete(ctx, oldEntry))
	require.True(t, registry.CompareAndDelete(ctx, newEntry))

	_, ok = registry.LoadByStorageKey(ctx, "stable")
	require.False(t, ok)
}

func TestRegistryRangeUsesSnapshotAndAllowsRegistryCallsInCallback(t *testing.T) {
	ctx := context.Background()
	registry := member.NewRegistry[string, *testMember]()

	first, err := registry.Put(ctx, id.MemberID(1), "one", &testMember{})
	require.NoError(t, err)
	second, err := registry.Put(ctx, id.MemberID(2), "two", &testMember{})
	require.NoError(t, err)

	seen := map[id.MemberID]bool{}
	registry.Range(ctx, func(entry member.Entry[string, *testMember]) bool {
		seen[entry.ID] = true
		if len(seen) == 1 {
			_, putErr := registry.Put(ctx, id.MemberID(3), "three", &testMember{})
			require.NoError(t, putErr)
			require.True(t, registry.CompareAndDelete(ctx, second))
		}
		return true
	})

	require.Equal(t, map[id.MemberID]bool{
		first.ID:  true,
		second.ID: true,
	}, seen)

	_, ok := registry.LoadByID(ctx, id.MemberID(3))
	require.True(t, ok)
}

func TestLiveKeyFuncIsSeparateFromStorageKey(t *testing.T) {
	ctx := context.Background()
	registry := member.NewRegistry[string, *testMember]()
	liveKey := member.LiveKeyFunc[string, *testMember](func(
		_ context.Context,
		value *testMember,
	) (string, error) {
		return value.liveKey, nil
	})

	entry, err := registry.Put(ctx, id.MemberID(1), "storage", &testMember{liveKey: "live"})
	require.NoError(t, err)

	queriedLiveKey, err := liveKey(ctx, entry.Value)
	require.NoError(t, err)
	require.Equal(t, "live", queriedLiveKey)

	wrongStorageKey := entry
	wrongStorageKey.StorageKey = queriedLiveKey
	require.False(t, registry.UnbindStorageKey(ctx, wrongStorageKey))
	require.False(t, registry.CompareAndDelete(ctx, wrongStorageKey))

	_, ok := registry.LoadByStorageKey(ctx, "live")
	require.False(t, ok)

	loaded, ok := registry.LoadByStorageKey(ctx, "storage")
	require.True(t, ok)
	require.Equal(t, entry.Token, loaded.Token)
}
