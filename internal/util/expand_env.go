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
	if opts.FailOnMissing {
		return expandValue(v, "", &opts)
	}
	return expandValueNoFail(v, "", &opts)
}

func expandValue(v reflect.Value, path string, opts *ExpandOptions) error {
	if opts.AllowPath != nil && path != "" && !opts.AllowPath(path) {
		return nil
	}
	if !v.IsValid() {
		return nil
	}

	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return nil
		}
		return expandValue(v.Elem(), path, opts)
	case reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return expandValue(v.Elem(), path, opts)
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			ft := t.Field(i)
			// only exported fields are settable
			if ft.PkgPath != "" {
				continue
			}

			name := jsonFieldName(ft)
			subPath := joinPath(path, name)
			if err := expandValue(field, subPath, opts); err != nil {
				return err
			}
		}
	case reflect.Map:
		if v.IsNil() {
			return nil
		}
		if v.Type().Key().Kind() != reflect.String {
			return nil
		}
		for _, key := range v.MapKeys() {
			k := key.String()
			val := v.MapIndex(key)
			// Work on a settable copy
			newVal := reflect.New(val.Type()).Elem()
			newVal.Set(val)
			subPath := joinPath(path, k)
			if err := expandValue(newVal, subPath, opts); err != nil {
				return err
			}
			v.SetMapIndex(key, newVal)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			elem := v.Index(i)
			subPath := joinPath(path, strconv.Itoa(i))
			if err := expandValue(elem, subPath, opts); err != nil {
				return err
			}
		}
	case reflect.String:
		orig := v.String()
		newStr, matched, err := expandString(orig)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if matched {
			if v.CanSet() {
				v.SetString(newStr)
			}
		}
	}
	return nil
}

func expandString(s string) (string, bool, error) {
	if s == "" {
		return "", false, nil
	}
	if m := dollarVarRe.FindStringSubmatch(s); m != nil {
		varName := m[1]
		val, ok := os.LookupEnv(varName)
		if ok {
			return val, true, nil
		}
		// no default form for $VAR
		return "", true, fmt.Errorf("environment variable %s not set", varName)
	}
	if m := braceVarRe.FindStringSubmatch(s); m != nil {
		varName := m[1]
		defaultVal := m[2]
		val, ok := os.LookupEnv(varName)
		if ok {
			return val, true, nil
		}
		if defaultVal != "" {
			return defaultVal, true, nil
		}
		return "", true, fmt.Errorf("environment variable %s not set", varName)
	}
	return s, false, nil
}

func expandValueNoFail(v reflect.Value, path string, opts *ExpandOptions) error {
	if opts.AllowPath != nil && path != "" && !opts.AllowPath(path) {
		return nil
	}
	if !v.IsValid() {
		return nil
	}

	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return nil
		}
		return expandValueNoFail(v.Elem(), path, opts)
	case reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return expandValueNoFail(v.Elem(), path, opts)
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			ft := t.Field(i)
			if ft.PkgPath != "" {
				continue
			}
			name := jsonFieldName(ft)
			subPath := joinPath(path, name)
			if err := expandValueNoFail(field, subPath, opts); err != nil {
				return err
			}
		}
	case reflect.Map:
		if v.IsNil() {
			return nil
		}
		if v.Type().Key().Kind() != reflect.String {
			return nil
		}
		for _, key := range v.MapKeys() {
			k := key.String()
			val := v.MapIndex(key)
			newVal := reflect.New(val.Type()).Elem()
			newVal.Set(val)
			subPath := joinPath(path, k)
			if err := expandValueNoFail(newVal, subPath, opts); err != nil {
				return err
			}
			v.SetMapIndex(key, newVal)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			elem := v.Index(i)
			subPath := joinPath(path, strconv.Itoa(i))
			if err := expandValueNoFail(elem, subPath, opts); err != nil {
				return err
			}
		}
	case reflect.String:
		orig := v.String()
		if m := dollarVarRe.FindStringSubmatch(orig); m != nil {
			varName := m[1]
			if val, ok := os.LookupEnv(varName); ok {
				if v.CanSet() {
					v.SetString(val)
				}
				return nil
			}
			if v.CanSet() {
				v.SetString("")
			}
			return nil
		}
		if m := braceVarRe.FindStringSubmatch(orig); m != nil {
			varName := m[1]
			defaultVal := m[2]
			if val, ok := os.LookupEnv(varName); ok {
				if v.CanSet() {
					v.SetString(val)
				}
				return nil
			}
			if defaultVal != "" {
				if v.CanSet() {
					v.SetString(defaultVal)
				}
				return nil
			}
			if v.CanSet() {
				v.SetString("")
			}
			return nil
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
