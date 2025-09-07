package types

type DictionaryItem struct {
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
}
type DictionaryItems []DictionaryItem
