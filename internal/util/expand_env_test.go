package util

import (
	"os"
	"testing"
)

// Shared types for tests so verify closures can assert concrete types.
type OpenAICfg struct {
	OpenAI struct {
		ApiKey string `json:"apiKey"`
	}
}

type SimpleVal struct{ Val string }

type Inner struct{ S string }

type NestedCfg struct {
	Str   string
	Slice []Inner
	Map   map[string]Inner
}

type KeepSkip struct {
	Keep string `json:"keep"`
	Skip string `json:"skip"`
}

func TestExpandEnvVars_TableDriven(t *testing.T) {
	tests := []struct {
		name    string
		setup   func()
		cleanup func()
		cfg     any
		opts    ExpandOptions
		verify  func(t *testing.T, cfg any)
		wantErr bool
	}{
		{
			name:    "DollarVar",
			setup:   func() { os.Setenv("MY_KEY", "secret123") },
			cleanup: func() { os.Unsetenv("MY_KEY") },
			cfg: func() any {
				c := &OpenAICfg{}
				c.OpenAI.ApiKey = "$MY_KEY"
				return c
			}(),
			opts: ExpandOptions{FailOnMissing: true},
			verify: func(t *testing.T, cfg any) {
				c := cfg.(*OpenAICfg)
				if c.OpenAI.ApiKey != "secret123" {
					t.Fatalf("expected secret123, got %q", c.OpenAI.ApiKey)
				}
			},
		},
		{
			name:    "CurlyVarDefault",
			setup:   func() { os.Unsetenv("NO_SUCH_VAR") },
			cleanup: func() {},
			cfg: func() any {
				c := &OpenAICfg{}
				c.OpenAI.ApiKey = "${NO_SUCH_VAR:-fallback}"
				return c
			}(),
			opts: ExpandOptions{FailOnMissing: false},
			verify: func(t *testing.T, cfg any) {
				c := cfg.(*OpenAICfg)
				if c.OpenAI.ApiKey != "fallback" {
					t.Fatalf("expected fallback, got %q", c.OpenAI.ApiKey)
				}
			},
		},
		{
			name:    "MissingFail",
			setup:   func() { os.Unsetenv("MUST_EXIST") },
			cleanup: func() {},
			cfg:     &SimpleVal{Val: "$MUST_EXIST"},
			opts:    ExpandOptions{FailOnMissing: true},
			wantErr: true,
		},
		{
			name:    "PartialUntouched",
			setup:   func() { os.Setenv("X", "1") },
			cleanup: func() { os.Unsetenv("X") },
			cfg:     &SimpleVal{Val: "prefix $X suffix"},
			opts:    ExpandOptions{FailOnMissing: false},
			verify: func(t *testing.T, cfg any) {
				c := cfg.(*SimpleVal)
				if c.Val != "prefix $X suffix" {
					t.Fatalf("expected unchanged string, got %q", c.Val)
				}
			},
		},
		{
			name:    "NestedMapSlice",
			setup:   func() { os.Setenv("A", "aval") },
			cleanup: func() { os.Unsetenv("A") },
			cfg: func() any {
				c := &NestedCfg{
					Str:   "${A}",
					Slice: []Inner{{S: "$A"}, {S: "${A:-no}"}},
					Map:   map[string]Inner{"k": {S: "${A}"}},
				}
				return c
			}(),
			opts: ExpandOptions{FailOnMissing: true},
			verify: func(t *testing.T, cfg any) {
				c := cfg.(*NestedCfg)
				if c.Str != "aval" || c.Slice[0].S != "aval" || c.Slice[1].S != "aval" || c.Map["k"].S != "aval" {
					t.Fatalf("nested expansion failed: %+v", c)
				}
			},
		},
		{
			name:    "AllowPath",
			setup:   func() { os.Setenv("SECRET", "s") },
			cleanup: func() { os.Unsetenv("SECRET") },
			cfg:     &KeepSkip{Keep: "$SECRET", Skip: "$SECRET"},
			opts: ExpandOptions{FailOnMissing: false, AllowPath: func(path string) bool {
				if path == "skip" {
					return false
				}
				return true
			}},
			verify: func(t *testing.T, cfg any) {
				c := cfg.(*KeepSkip)
				if c.Keep != "s" {
					t.Fatalf("expected Keep expanded, got %q", c.Keep)
				}
				if c.Skip != "$SECRET" {
					t.Fatalf("expected Skip untouched, got %q", c.Skip)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup()
			}
			if tc.cleanup != nil {
				t.Cleanup(tc.cleanup)
			}
			err := ExpandEnvVarsInConfig(tc.cfg, tc.opts)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("%s: expected error", tc.name)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.name, err)
			}
			if tc.verify != nil {
				tc.verify(t, tc.cfg)
			}
		})
	}
}
