package util

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// ExpandOptions controls environment variable expansion behavior.
type ExpandOptions struct {
	FailOnMissing bool
	AllowPath     func(path string) bool // if nil, allow all paths
}

var (
	// matches $VAR
	dollarVarRe = regexp.MustCompile(`^\$([A-Za-z_][A-Za-z0-9_]*)$`)
	// matches ${VAR} and ${VAR:-default}
	braceVarRe = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}$`)
)

// ExpandEnvVarsInConfig walks a decoded config (pointer to struct, possibly containing maps/slices)
// and replaces any string fields that exactly match env references with environment values.
// Supported patterns (exact match only):
//
//	$VAR
//	${VAR}
//	${VAR:-default}
func ExpandEnvVarsInConfig(cfg any, opts ExpandOptions) error {
	if cfg == nil {
		return nil
	}
	v := reflect.ValueOf(cfg)
	if v.Kind() != reflect.Ptr {
		return errors.New("cfg must be a pointer to a value")
	}
	v = v.Elem()
	return expandValue(v, "", &opts)
}

// processString centralizes detection and replacement logic. It returns (newVal, matched, err).
func processString(s string, failOnMissing bool) (string, bool, error) {
	if s == "" {
		return "", false, nil
	}
	if m := dollarVarRe.FindStringSubmatch(s); m != nil {
		name := m[1]
		if val, ok := os.LookupEnv(name); ok {
			return val, true, nil
		}
		if failOnMissing {
			return "", true, fmt.Errorf("environment variable %s not set", name)
		}
		return "", true, nil
	}
	if m := braceVarRe.FindStringSubmatch(s); m != nil {
		name := m[1]
		def := m[2]
		if val, ok := os.LookupEnv(name); ok {
			return val, true, nil
		}
		if def != "" {
			return def, true, nil
		}
		if failOnMissing {
			return "", true, fmt.Errorf("environment variable %s not set", name)
		}
		return "", true, nil
	}
	return s, false, nil
}

// expandValue is a single recursive walker handling all kinds and using opts.FailOnMissing
func expandValue(v reflect.Value, path string, opts *ExpandOptions) error {
	if opts.AllowPath != nil && path != "" && !opts.AllowPath(path) {
		return nil
	}
	if !v.IsValid() {
		return nil
	}

	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return expandValue(v.Elem(), path, opts)
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			sf := t.Field(i)
			if sf.PkgPath != "" { // unexported
				continue
			}
			name := jsonFieldName(sf)
			sub := joinPath(path, name)
			if err := expandValue(v.Field(i), sub, opts); err != nil {
				return err
			}
		}
	case reflect.Map:
		if v.IsNil() || v.Type().Key().Kind() != reflect.String {
			return nil
		}
		for _, key := range v.MapKeys() {
			k := key.String()
			val := v.MapIndex(key)
			copyVal := reflect.New(val.Type()).Elem()
			copyVal.Set(val)
			sub := joinPath(path, k)
			if err := expandValue(copyVal, sub, opts); err != nil {
				return err
			}
			v.SetMapIndex(key, copyVal)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			sub := joinPath(path, strconv.Itoa(i))
			if err := expandValue(v.Index(i), sub, opts); err != nil {
				return err
			}
		}
	case reflect.String:
		if !v.CanSet() {
			return nil
		}
		orig := v.String()
		newStr, matched, err := processString(orig, opts.FailOnMissing)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if matched {
			v.SetString(newStr)
		}
	}
	return nil
}

func joinPath(base, part string) string {
	if part == "" {
		return base
	}
	if base == "" {
		return part
	}
	return base + "." + part
}

func jsonFieldName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name
	}
	parts := strings.Split(tag, ",")
	name := parts[0]
	if name == "" || name == "-" {
		return f.Name
	}
	return name
}
