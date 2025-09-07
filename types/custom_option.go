package types

import (
	"slices"
)

type DictionaryItem struct {
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
}
type DictionaryItems []DictionaryItem

func (s DictionaryItems) Deduplicate() DictionaryItems {
	if s == nil {
		return nil
	}
	m := make(map[string]struct{}, len(s))
	var result DictionaryItems
	for _, item := range slices.Backward(s) {
		if _, ok := m[item.Key]; ok {
			continue
		}
		m[item.Key] = struct{}{}
		result = append(result, item)
	}
	slices.Reverse(result)
	return result
}
