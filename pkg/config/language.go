package config

import (
	"sort"
	"strings"
)

// Languages are the values Config.Language accepts. The interface is written in
// French today and this field changes nothing yet — it exists now so that the
// i18n phase is a translation job and not also a configuration job, and so that
// a config file written today does not have to be edited then.
var Languages = map[string]bool{
	"fr": true,
	"en": true,
}

// languageList renders Languages for an error message, in a stable order.
func languageList() string {
	names := make([]string, 0, len(Languages))
	for name := range Languages {
		names = append(names, name)
	}

	sort.Strings(names)

	return strings.Join(names, ", ")
}
