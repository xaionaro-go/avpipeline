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

func (s *DictionaryItems) SetFirst(input DictionaryItem) {
	for i, item := range *s {
		if item.Key == input.Key {
			(*s)[i].Value = input.Value
			return
		}
	}
	*s = append(*s, input)
}

func (s DictionaryItems) GetFirst(key string) *string {
	for _, item := range s {
		if item.Key == key {
			return &item.Value
		}
	}
	return nil
}
