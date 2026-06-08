package settings

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Sentinel errors so callers (the REST handler) can map to a 400 with the
// message intact rather than a generic 500.
var (
	ErrUnknownKey  = errors.New("unknown setting")
	ErrNotEditable = errors.New("setting is not editable")
	ErrEnvLocked   = errors.New("setting is pinned by an environment variable and cannot be changed in the UI")
	ErrInvalid     = errors.New("invalid value")
)

// Set validates and persists a DB override for an editable, non-env-pinned
// setting, then refreshes the cache so hot settings apply immediately.
func (s *Service) Set(ctx context.Context, key, value, by string) error {
	def, ok := byKey[key]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownKey, key)
	}
	if !def.Editable || def.Secret {
		return fmt.Errorf("%w: %q", ErrNotEditable, key)
	}
	if envPinned(key) {
		return fmt.Errorf("%w: %q (set via %s)", ErrEnvLocked, key, def.Env)
	}
	if err := validate(def, value); err != nil {
		return err
	}
	if s == nil || s.client == nil {
		return errors.New("settings store unavailable")
	}
	if err := s.client.set(ctx, key, value, by); err != nil {
		return err
	}
	return s.Refresh(ctx)
}

// validate checks a candidate value against the setting's kind and bounds.
func validate(def Setting, value string) error {
	v := strings.TrimSpace(value)
	switch def.Kind {
	case KindInt:
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%w: %q is not an integer", ErrInvalid, value)
		}
		if def.Min != nil && n < *def.Min {
			return fmt.Errorf("%w: %d is below the minimum %d", ErrInvalid, n, *def.Min)
		}
		if def.Max != nil && n > *def.Max {
			return fmt.Errorf("%w: %d is above the maximum %d", ErrInvalid, n, *def.Max)
		}
	case KindBool:
		switch strings.ToLower(v) {
		case "true", "false", "1", "0":
		default:
			return fmt.Errorf("%w: %q is not a boolean", ErrInvalid, value)
		}
	case KindEnum:
		for _, opt := range def.Options {
			if v == opt {
				return nil
			}
		}
		return fmt.Errorf("%w: %q is not one of %v", ErrInvalid, value, def.Options)
	case KindString:
		// any string accepted; length-bound to keep a value sane
		if len(v) > 4096 {
			return fmt.Errorf("%w: value too long", ErrInvalid)
		}
	}
	return nil
}
