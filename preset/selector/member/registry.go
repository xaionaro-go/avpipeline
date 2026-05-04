package member

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

type Registry[K comparable, M any] struct {
	lock         sync.Mutex
	byID         map[id.MemberID]Entry[K, M]
	byStorageKey map[K]Entry[K, M]
	nextToken    Token
}

func NewRegistry[K comparable, M any]() *Registry[K, M] {
	return &Registry[K, M]{
		byID:         map[id.MemberID]Entry[K, M]{},
		byStorageKey: map[K]Entry[K, M]{},
	}
}

func (r *Registry[K, M]) Put(
	_ context.Context,
	memberID id.MemberID,
	storageKey K,
	value M,
) (Entry[K, M], error) {
	if memberID < 0 {
		return Entry[K, M]{}, fmt.Errorf("%w: member %d", ErrInvalidMemberID, memberID)
	}

	r.lock.Lock()
	defer r.lock.Unlock()

	if _, ok := r.byID[memberID]; ok {
		return Entry[K, M]{}, fmt.Errorf("%w: member %d", ErrDuplicateMemberID, memberID)
	}
	if _, ok := r.byStorageKey[storageKey]; ok {
		return Entry[K, M]{}, ErrDuplicateStorageKey
	}

	r.nextToken++
	entry := Entry[K, M]{
		ID:         memberID,
		StorageKey: storageKey,
		Value:      value,
		Token:      r.nextToken,
	}
	r.byID[memberID] = entry
	r.byStorageKey[storageKey] = entry

	return entry, nil
}

func (r *Registry[K, M]) LoadByID(
	_ context.Context,
	memberID id.MemberID,
) (Entry[K, M], bool) {
	r.lock.Lock()
	defer r.lock.Unlock()

	entry, ok := r.byID[memberID]
	return entry, ok
}

func (r *Registry[K, M]) LoadByStorageKey(
	_ context.Context,
	storageKey K,
) (Entry[K, M], bool) {
	r.lock.Lock()
	defer r.lock.Unlock()

	entry, ok := r.byStorageKey[storageKey]
	return entry, ok
}

func (r *Registry[K, M]) UnbindStorageKey(
	_ context.Context,
	entry Entry[K, M],
) bool {
	r.lock.Lock()
	defer r.lock.Unlock()

	current, ok := r.byStorageKey[entry.StorageKey]
	if !ok {
		return false
	}
	if !sameEntry(current, entry) {
		return false
	}

	delete(r.byStorageKey, entry.StorageKey)
	return true
}

func (r *Registry[K, M]) CompareAndDelete(
	_ context.Context,
	entry Entry[K, M],
) bool {
	r.lock.Lock()
	defer r.lock.Unlock()

	current, ok := r.byID[entry.ID]
	if !ok {
		return false
	}
	if !sameEntry(current, entry) {
		return false
	}

	delete(r.byID, entry.ID)
	if currentByStorageKey, ok := r.byStorageKey[entry.StorageKey]; ok && sameEntry(currentByStorageKey, entry) {
		delete(r.byStorageKey, entry.StorageKey)
	}

	return true
}

func (r *Registry[K, M]) Range(
	ctx context.Context,
	fn func(Entry[K, M]) bool,
) {
	snapshot := r.snapshot(ctx)
	for _, entry := range snapshot {
		if !fn(entry) {
			return
		}
	}
}

func (r *Registry[K, M]) snapshot(
	_ context.Context,
) []Entry[K, M] {
	r.lock.Lock()
	defer r.lock.Unlock()

	snapshot := make([]Entry[K, M], 0, len(r.byID))
	for _, entry := range r.byID {
		snapshot = append(snapshot, entry)
	}
	slices.SortFunc(snapshot, func(
		left Entry[K, M],
		right Entry[K, M],
	) int {
		return compareMemberID(left.ID, right.ID)
	})

	return snapshot
}

func compareMemberID(
	left id.MemberID,
	right id.MemberID,
) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}
