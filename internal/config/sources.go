package config

import (
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Where a setting's value came from.
//
// Four layers stack, in this order: the built-in defaults, then zoomies.yaml,
// then the fleet's database, then the ZOOMIES_* environment. Each one wins over
// the one before it.
//
// Recording which layer won is not bookkeeping for its own sake. It is the
// answer to the question that makes a configuration problem hard -- "I changed
// it and nothing happened" -- and it is what lets the settings page say *why*
// a field it would otherwise let an administrator edit is not editable: a value
// pinned by an environment variable would be silently overwritten at the next
// restart, so the page names the variable instead of accepting a change it
// knows will not survive.
type Source string

const (
	// SourceDefault is the value Zoomies ships with. Nothing has been said
	// about this setting anywhere.
	SourceDefault Source = "default"
	// SourceFile is zoomies.yaml.
	SourceFile Source = "file"
	// SourceDatabase is this fleet's own database, which is where a change made
	// in the UI goes and where everything belongs that can live there.
	SourceDatabase Source = "database"
	// SourceEnvironment is a ZOOMIES_* variable. It is deliberately the last
	// word: it is the way back in when a stored setting has locked an operator
	// out of the interface that would let them fix it.
	SourceEnvironment Source = "environment"
)

// note records which layer set a key. A nil map is the zero Config's, so this
// allocates on first use rather than asking every constructor to remember.
func (c *Config) note(key string, from Source) {
	if c.sources == nil {
		c.sources = map[string]Source{}
	}
	c.sources[key] = from
}

// Source returns the layer a setting's value came from.
func (c *Config) Source(key string) Source {
	if from, ok := c.sources[key]; ok {
		return from
	}
	return SourceDefault
}

// Sources returns every key that came from somewhere other than the defaults.
func (c *Config) Sources() map[string]Source {
	out := make(map[string]Source, len(c.sources))
	for k, v := range c.sources {
		out[k] = v
	}
	return out
}

// PinnedByEnvironment returns the stored settings an environment variable is
// currently overriding, ordered as the documentation orders them.
//
// These are the ones an administrator cannot change in the UI, because the
// change would be accepted, written, and then lost behind the variable at the
// next restart.
func (c *Config) PinnedByEnvironment() []Setting {
	var out []Setting
	for _, s := range StoredSettings() {
		if c.Source(s.Key) == SourceEnvironment {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return CompareKeys(out[i].Key, out[j].Key) < 0 })
	return out
}

// copySources gives a snapshot its own map, so that a Live update which records
// a new source cannot be seen half-written by a reader holding the old one.
func (c *Config) copySources() map[string]Source {
	out := make(map[string]Source, len(c.sources))
	for k, v := range c.sources {
		out[k] = v
	}
	return out
}

// keysIn returns the dotted keys a YAML document actually mentions.
//
// The struct decode cannot answer this: a key absent from the file and a key
// written with its default value produce the same struct, and the settings page
// has to tell them apart to say whether the file or the database is in charge
// of a setting. So the same bytes are read twice -- once strictly into the
// struct, which is what catches a typo, and once loosely into a tree, which is
// what says which keys were spelled.
func keysIn(doc []byte) []string {
	var tree map[string]any
	if err := yaml.Unmarshal(doc, &tree); err != nil {
		return nil
	}
	var out []string
	var walkTree func(prefix string, node map[string]any)
	walkTree = func(prefix string, node map[string]any) {
		for k, v := range node {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			// A nested map is a section unless it is a setting whose value is
			// a map -- agent.labels is the only one, and the registry is what
			// knows the difference.
			if child, ok := v.(map[string]any); ok {
				if s, known := LookupSetting(path); !known || s.Kind != KindLabels {
					walkTree(path, child)
					continue
				}
			}
			out = append(out, path)
		}
	}
	walkTree("", tree)
	sort.Strings(out)
	return out
}

// SectionsOf groups keys under their section heading, in reading order.
func SectionsOf(keys []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, k := range keys {
		head, _, _ := strings.Cut(k, ".")
		if !seen[head] {
			seen[head] = true
			out = append(out, head)
		}
	}
	sort.Slice(out, func(i, j int) bool { return CompareKeys(out[i], out[j]) < 0 })
	return out
}

// Note records where a value came from. It is exported for the settings API,
// which assigns a value and then has to say which layer it now belongs to --
// the database when it was stored, or the defaults when it was cleared.
func (c *Config) Note(key string, from Source) { c.note(key, from) }

// SetSources replaces the record wholesale, for a caller building a candidate
// configuration out of a running one. The map is copied, so the two snapshots
// do not share it.
func (c *Config) SetSources(from map[string]Source) {
	c.sources = make(map[string]Source, len(from))
	for k, v := range from {
		c.sources[k] = v
	}
}
