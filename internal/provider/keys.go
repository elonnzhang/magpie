package provider

// A provider can hold several accounts, as a subscription can: several API
// keys — a personal one and the team's, a paid plan and a free one — with
// one in use and the rest one click away. Key stays the one in use, so
// everything that talks to the vendor reads it as before.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// KeyAccount is a saved key that isn't in use.
type KeyAccount struct {
	Name string `json:"name,omitempty"`
	Key  string `json:"key"`
}

// KeyInfo describes one of a provider's keys without giving it away.
type KeyInfo struct {
	ID     string `json:"id"` // a fingerprint, to name it in a switch
	Name   string `json:"name,omitempty"`
	Masked string `json:"masked"`
	Active bool   `json:"active"`
}

func keyID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:5])
}

// KeyList is a provider's keys, the one in use first.
func (p Provider) KeyList() []KeyInfo {
	var out []KeyInfo
	if p.Key != "" {
		out = append(out, KeyInfo{ID: keyID(p.Key), Name: p.KeyName, Masked: Mask(p.Key), Active: true})
	}
	for _, k := range p.Keys {
		out = append(out, KeyInfo{ID: keyID(k.Key), Name: k.Name, Masked: Mask(k.Key)})
	}
	return out
}

// AddKey saves one more key for a provider. It is put in use when the
// provider has none yet.
func AddKey(id, name, key string) error {
	name, key = strings.TrimSpace(name), strings.TrimSpace(key)
	if key == "" {
		return errors.New("paste the key to add")
	}
	p, err := Find(id)
	if err != nil {
		return err
	}
	if p.Account != nil {
		return errors.New("a signed-in account has no keys")
	}
	for _, k := range p.KeyList() {
		if k.ID == keyID(key) {
			return fmt.Errorf("%s already has this key", p.Name)
		}
	}
	if p.Key == "" {
		p.Key, p.KeyName = key, name
	} else {
		p.Keys = append(p.Keys, KeyAccount{Name: name, Key: key})
	}
	return Save(*p)
}

// UseKey puts one of a provider's saved keys in use; the one it replaces
// is kept with the others.
func UseKey(id, keyRef string) error {
	p, err := Find(id)
	if err != nil {
		return err
	}
	for i, k := range p.Keys {
		if keyID(k.Key) == keyRef {
			p.Keys[i] = KeyAccount{Name: p.KeyName, Key: p.Key}
			p.Key, p.KeyName = k.Key, k.Name
			if p.Key == "" || p.Keys[i].Key == "" {
				p.Keys = append(p.Keys[:i], p.Keys[i+1:]...)
			}
			return Save(*p)
		}
	}
	if p.Key != "" && keyID(p.Key) == keyRef {
		return nil
	}
	return fmt.Errorf("%s has no such key", p.Name)
}

// RemoveKey forgets a saved key. The one in use stays: switch first.
func RemoveKey(id, keyRef string) error {
	p, err := Find(id)
	if err != nil {
		return err
	}
	for i, k := range p.Keys {
		if keyID(k.Key) == keyRef {
			p.Keys = append(p.Keys[:i], p.Keys[i+1:]...)
			return Save(*p)
		}
	}
	if p.Key != "" && keyID(p.Key) == keyRef {
		return errors.New("that key is in use; switch to another before removing it")
	}
	return fmt.Errorf("%s has no such key", p.Name)
}

// RenameKey names one of a provider's keys, "Personal", "Team".
func RenameKey(id, keyRef, name string) error {
	p, err := Find(id)
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if p.Key != "" && keyID(p.Key) == keyRef {
		p.KeyName = name
		return Save(*p)
	}
	for i, k := range p.Keys {
		if keyID(k.Key) == keyRef {
			p.Keys[i].Name = name
			return Save(*p)
		}
	}
	return fmt.Errorf("%s has no such key", p.Name)
}
