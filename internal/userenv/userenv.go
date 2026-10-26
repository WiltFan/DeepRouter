// Package userenv stores gateway variables in the Windows user environment.
//
// On Windows, deeprouter-setup does not edit .zshrc or a PowerShell profile.
// The same variables are written to HKCU\Environment (what setx updates) so a
// new process inherits them. The registry is used directly because setx
// truncates values longer than 1024 characters.
//
// A record file remembers each variable's previous value so restore and
// uninstall can put the user environment back.
package userenv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Location is the place Windows user environment variables are stored.
const Location = `HKCU\Environment`

// ManagedKeys is the stable order written for a full setup.
var ManagedKeys = []string{
	"ANTHROPIC_BASE_URL",
	"ANTHROPIC_AUTH_TOKEN",
	"ANTHROPIC_MODEL",
	"DEEPROUTER_API_KEY",
}

// Update is one variable to set. An empty value leaves an existing value.
type Update struct {
	Key   string
	Value string
}

// Prior is the value a variable had before this tool first changed it.
type Prior struct {
	Key   string
	Set   bool
	Value string
}

// Value is one managed variable.
type Value struct {
	Key   string
	Value string
}

// Record is the on-disk memory of managed variables and their previous values.
type Record struct {
	Prior  []Prior
	Values []Value
}

// Store is the user-environment backend. The Windows build uses the registry.
// Tests use an in-memory store.
type Store interface {
	Get(key string) (value string, ok bool, err error)
	Set(key, value string) error
	Delete(key string) error
}

// RecordPath is the file that remembers prior user-environment values.
func RecordPath(configDir string) string {
	return filepath.Join(configDir, "user-env.json")
}

// Load reads a record. A missing file is os.ErrNotExist.
func Load(path string) (Record, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return Record{}, nil
	}
	var doc fileDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Record{}, fmt.Errorf("user environment record is not valid JSON: %w", err)
	}
	rec := Record{Prior: make([]Prior, 0, len(doc.Prior)), Values: make([]Value, 0, len(doc.Values))}
	for _, p := range doc.Prior {
		if p.Key == "" {
			continue
		}
		rec.Prior = append(rec.Prior, Prior{Key: p.Key, Set: p.Set, Value: p.Value})
	}
	for _, v := range doc.Values {
		if v.Key == "" {
			continue
		}
		rec.Values = append(rec.Values, Value{Key: v.Key, Value: v.Value})
	}
	return rec, nil
}

// Marshal encodes the record in a stable form.
func (r Record) Marshal() ([]byte, error) {
	doc := fileDoc{
		Prior:  make([]priorDoc, 0, len(r.Prior)),
		Values: make([]valueDoc, 0, len(r.Values)),
	}
	for _, p := range r.Prior {
		doc.Prior = append(doc.Prior, priorDoc{Key: p.Key, Set: p.Set, Value: p.Value})
	}
	for _, v := range r.Values {
		doc.Values = append(doc.Values, valueDoc{Key: v.Key, Value: v.Value})
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Text renders managed values for tests and status checks.
func (r Record) Text() string {
	var b strings.Builder
	for _, v := range r.Values {
		b.WriteString(v.Key)
		b.WriteByte('=')
		b.WriteString(v.Value)
		b.WriteByte('\n')
	}
	return b.String()
}

// Get returns a managed value.
func (r Record) Get(key string) (string, bool) {
	for _, v := range r.Values {
		if v.Key == key {
			return v.Value, true
		}
	}
	return "", false
}

func (r Record) hasPrior(key string) bool {
	for _, p := range r.Prior {
		if p.Key == key {
			return true
		}
	}
	return false
}

// Plan merges updates into current. The first time a key is written, its
// previous store value is kept so a later restore can put it back.
// An empty update value does not change that key.
func Plan(st Store, current Record, updates []Update) (Record, error) {
	if st == nil {
		return Record{}, fmt.Errorf("user environment store is nil")
	}
	next := current
	next.Prior = append([]Prior(nil), current.Prior...)
	values := map[string]string{}
	var order []string
	for _, v := range current.Values {
		if _, ok := values[v.Key]; ok {
			continue
		}
		values[v.Key] = v.Value
		order = append(order, v.Key)
	}
	for _, u := range updates {
		if strings.TrimSpace(u.Key) == "" || u.Value == "" {
			continue
		}
		if !next.hasPrior(u.Key) {
			val, ok, err := st.Get(u.Key)
			if err != nil {
				return Record{}, err
			}
			next.Prior = append(next.Prior, Prior{Key: u.Key, Set: ok, Value: val})
		}
		if _, ok := values[u.Key]; !ok {
			order = append(order, u.Key)
		}
		values[u.Key] = u.Value
	}
	next.Values = nil
	seen := map[string]bool{}
	for _, k := range ManagedKeys {
		if v, ok := values[k]; ok {
			next.Values = append(next.Values, Value{Key: k, Value: v})
			seen[k] = true
		}
	}
	for _, k := range order {
		if seen[k] {
			continue
		}
		if v, ok := values[k]; ok {
			next.Values = append(next.Values, Value{Key: k, Value: v})
		}
	}
	if next.Prior == nil {
		next.Prior = []Prior{}
	}
	if next.Values == nil {
		next.Values = []Value{}
	}
	return next, nil
}

// ApplyValues makes the store match rec. Keys listed in Prior but absent from
// Values are restored to the prior value, or deleted when they did not exist.
// Notify runs only when a value changes.
func ApplyValues(st Store, rec Record) (bool, error) {
	if st == nil {
		return false, fmt.Errorf("user environment store is nil")
	}
	changed := false
	managed := map[string]bool{}
	for _, v := range rec.Values {
		managed[v.Key] = true
		got, ok, err := st.Get(v.Key)
		if err != nil {
			return false, err
		}
		if ok && got == v.Value {
			continue
		}
		if err := st.Set(v.Key, v.Value); err != nil {
			return false, err
		}
		changed = true
	}
	for _, p := range rec.Prior {
		if managed[p.Key] {
			continue
		}
		if p.Set {
			got, ok, err := st.Get(p.Key)
			if err != nil {
				return false, err
			}
			if ok && got == p.Value {
				continue
			}
			if err := st.Set(p.Key, p.Value); err != nil {
				return false, err
			}
		} else {
			_, ok, err := st.Get(p.Key)
			if err != nil {
				return false, err
			}
			if !ok {
				continue
			}
			if err := st.Delete(p.Key); err != nil {
				return false, err
			}
		}
		changed = true
	}
	if changed {
		notify(st)
	}
	return changed, nil
}

// Reconcile updates the store after a backup restore.
// before is the record that was on disk before the restore. after is the
// record afterwards. When the file was removed, variables return to the
// values they had before this tool changed them.
func Reconcile(st Store, before Record, beforeOK bool, after Record, afterOK bool) error {
	if st == nil || (!beforeOK && !afterOK) {
		return nil
	}
	if !afterOK {
		blank := Record{Prior: before.Prior}
		_, err := ApplyValues(st, blank)
		return err
	}
	merged := after
	merged.Prior = append([]Prior(nil), after.Prior...)
	merged.Values = append([]Value(nil), after.Values...)
	if beforeOK {
		for _, p := range before.Prior {
			if _, ok := merged.Get(p.Key); ok {
				continue
			}
			if merged.hasPrior(p.Key) {
				continue
			}
			merged.Prior = append(merged.Prior, p)
		}
	}
	_, err := ApplyValues(st, merged)
	return err
}

func notify(st Store) {
	n, ok := st.(interface{ Notify() })
	if ok {
		n.Notify()
	}
}

type priorDoc struct {
	Key   string `json:"key"`
	Set   bool   `json:"set"`
	Value string `json:"value,omitempty"`
}

type valueDoc struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type fileDoc struct {
	Prior  []priorDoc `json:"prior"`
	Values []valueDoc `json:"values"`
}

// Active is the store setup writes. Tests replace it with SetCurrent.
// On Windows the default is HKCU\Environment. Elsewhere the default is nil.
var current Store

// SetCurrent replaces the process-wide store and returns the previous one.
func SetCurrent(s Store) Store {
	prev := current
	current = s
	return prev
}

// Active returns the store to write, or nil when this OS has none.
func Active() Store {
	if current != nil {
		return current
	}
	return platformDefault()
}

// IsNotExist reports whether err is a missing record file.
func IsNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
