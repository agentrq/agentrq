// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunner

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

// ABIVersion is the contract between the server and a hook: a WASI preview1
// command that answers "manifest" and "handle" with JSON on stdout.
const ABIVersion = 1

const (
	maxInputs   = 32
	maxEvents   = 64
	maxTextSize = 512
)

var (
	hookNameRe  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	inputNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)
	eventNameRe = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
)

// The types an input may take.
const (
	InputString  = "string"
	InputNumber  = "number"
	InputBoolean = "boolean"
	InputArray   = "array"
)

// Manifest is what a hook says about itself.
type Manifest struct {
	ABIVersion  int     `json:"abiVersion"`
	Name        string  `json:"name"`
	Version     string  `json:"version"`
	Description string  `json:"description,omitempty"`
	Inputs      []Input `json:"inputs"`
	Events      []Event `json:"events"`
}

// Input is a value a registration of the hook provides, such as its webhook
// secret. A secret one is write-only once stored.
type Input struct {
	Name     string `json:"name"`
	Label    string `json:"label,omitempty"`
	Type     string `json:"type"`
	Items    string `json:"items,omitempty"`
	Secret   bool   `json:"secret,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// Event is one kind of event the hook writes.
type Event struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Declares reports whether the hook declares the event name.
func (m Manifest) Declares(name string) bool {
	for _, e := range m.Events {
		if e.Name == name {
			return true
		}
	}
	return false
}

// parseManifest reads a hook's answer to "manifest" and checks it.
func parseManifest(out []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(out, &m); err != nil {
		return Manifest{}, fmt.Errorf("manifest is not JSON: %w", err)
	}
	return m, m.validate()
}

func (m Manifest) validate() error {
	switch {
	case m.ABIVersion != ABIVersion:
		return fmt.Errorf("abiVersion %d is not supported; want %d", m.ABIVersion, ABIVersion)
	case !hookNameRe.MatchString(m.Name):
		return fmt.Errorf("name %q must be lowercase letters, digits and -, starting with a letter, at most 64", m.Name)
	case m.Version == "" || len(m.Version) > 32:
		return errors.New("version must be 1 to 32 characters")
	case len(m.Description) > maxTextSize:
		return fmt.Errorf("description is over %d bytes", maxTextSize)
	case len(m.Inputs) > maxInputs:
		return fmt.Errorf("more than %d inputs", maxInputs)
	case len(m.Events) == 0 || len(m.Events) > maxEvents:
		return fmt.Errorf("must declare 1 to %d events", maxEvents)
	}
	seen := make(map[string]bool, len(m.Inputs)+len(m.Events))
	secret := false
	for _, in := range m.Inputs {
		if err := in.validate(); err != nil {
			return err
		}
		if seen[in.Name] {
			return fmt.Errorf("input %q is declared twice", in.Name)
		}
		seen[in.Name] = true
		secret = secret || (in.Secret && in.Required)
	}
	// The URL's id is guessable; a secret the hook checks is what keeps a
	// stranger from firing the account's events.
	if !secret {
		return errors.New("must declare a required secret input, which the hook checks")
	}
	clear(seen)
	for _, e := range m.Events {
		switch {
		case !eventNameRe.MatchString(e.Name):
			return fmt.Errorf("event name %q must be lowercase letters, digits, '.', '_' and '-', starting with a letter, at most 64", e.Name)
		case seen[e.Name]:
			return fmt.Errorf("event %q is declared twice", e.Name)
		case len(e.Description) > maxTextSize:
			return fmt.Errorf("event %q: description is over %d bytes", e.Name, maxTextSize)
		}
		seen[e.Name] = true
	}
	return nil
}

func (in Input) validate() error {
	switch {
	case !inputNameRe.MatchString(in.Name):
		return fmt.Errorf("input name %q must be letters, digits and _, starting with a letter, at most 64", in.Name)
	case len(in.Label) > maxTextSize:
		return fmt.Errorf("input %q: label is over %d bytes", in.Name, maxTextSize)
	}
	switch in.Type {
	case InputString:
	case InputNumber, InputBoolean, InputArray:
		if in.Secret {
			return fmt.Errorf("input %q: only a string can be secret", in.Name)
		}
	default:
		return fmt.Errorf("input %q: type %q is not string, number, boolean or array", in.Name, in.Type)
	}
	if in.Type == InputArray && in.Items != InputString && in.Items != InputNumber {
		return fmt.Errorf("input %q: an array's items must be string or number", in.Name)
	}
	if in.Type != InputArray && in.Items != "" {
		return fmt.Errorf("input %q: items is for an array", in.Name)
	}
	return nil
}
