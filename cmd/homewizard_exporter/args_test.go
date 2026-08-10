package main

import (
	"flag"
	"io"
	"slices"
	"testing"
)

func testFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.String("o", "", "output file")
	fs.String("name", "", "user name")
	fs.Bool("insecure", false, "skip verification")
	return fs
}

func TestExtractHost(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantHost string
		wantRest []string
		wantErr  bool
	}{
		{
			name:     "host first, as everyone types it",
			args:     []string{"192.168.1.10", "-o", "token"},
			wantHost: "192.168.1.10",
			wantRest: []string{"-o", "token"},
		},
		{
			name:     "host last, as Go expects",
			args:     []string{"-o", "token", "192.168.1.10"},
			wantHost: "192.168.1.10",
			wantRest: []string{"-o", "token"},
		},
		{
			name:     "host in the middle",
			args:     []string{"-insecure", "192.168.1.10", "-o", "token"},
			wantHost: "192.168.1.10",
			wantRest: []string{"-insecure", "-o", "token"},
		},
		{
			name:     "flag value is not the host",
			args:     []string{"-o", "token"},
			wantHost: "",
			wantRest: []string{"-o", "token"},
		},
		{
			name:     "equals form",
			args:     []string{"-o=token", "192.168.1.10"},
			wantHost: "192.168.1.10",
			wantRest: []string{"-o=token"},
		},
		{
			// A bool flag takes no value, so the next word really is the host.
			name:     "bool flag does not swallow the host",
			args:     []string{"-insecure", "192.168.1.10"},
			wantHost: "192.168.1.10",
			wantRest: []string{"-insecure"},
		},
		{
			name:     "double dash",
			args:     []string{"192.168.1.10", "--", "-o", "token"},
			wantHost: "192.168.1.10",
			wantRest: []string{"--", "-o", "token"},
		},
		{
			name:     "long form flags",
			args:     []string{"--name", "local/app", "192.168.1.10"},
			wantHost: "192.168.1.10",
			wantRest: []string{"--name", "local/app"},
		},
		{
			name:    "two hosts",
			args:    []string{"192.168.1.10", "192.168.1.11"},
			wantErr: true,
		},
		{
			name:     "no arguments",
			args:     nil,
			wantHost: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, rest, err := extractHost(testFlagSet(), tt.args)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got host=%q rest=%v", host, rest)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if host != tt.wantHost {
				t.Errorf("host = %q, want %q", host, tt.wantHost)
			}
			if !slices.Equal(rest, tt.wantRest) {
				t.Errorf("rest = %v, want %v", rest, tt.wantRest)
			}
		})
	}
}

// TestParseWithHost checks that the flags actually take effect, since the whole
// point of splitting the host out is that the rest still parses normally.
func TestParseWithHost(t *testing.T) {
	fs := testFlagSet()
	out := fs.String("out", "", "output")

	host, err := parseWithHost(fs, []string{"192.168.1.10", "-out", "token", "-insecure"})
	if err != nil {
		t.Fatal(err)
	}
	if host != "192.168.1.10" {
		t.Errorf("host = %q", host)
	}
	if *out != "token" {
		t.Errorf("-out = %q, want token", *out)
	}
	if insecure := fs.Lookup("insecure").Value.String(); insecure != "true" {
		t.Errorf("-insecure = %s, want true", insecure)
	}
}

func TestParseWithHostRequiresAHost(t *testing.T) {
	fs := testFlagSet()
	fs.Usage = func() {}

	if _, err := parseWithHost(fs, []string{"-insecure"}); err == nil {
		t.Error("a command that needs a host should say so when given none")
	}
}
