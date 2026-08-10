package main

import (
	"flag"
	"fmt"
	"strings"
)

// extractHost pulls the single positional argument out of args and returns the
// rest for the flag package to parse.
func extractHost(fs *flag.FlagSet, args []string) (host string, rest []string, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--" {
			rest = append(rest, args[i:]...)
			break
		}

		if !strings.HasPrefix(arg, "-") || arg == "-" {
			if host != "" {
				return "", nil, fmt.Errorf("unexpected argument %q (expected one host)", arg)
			}
			host = arg
			continue
		}

		rest = append(rest, arg)

		if strings.Contains(arg, "=") {
			continue
		}

		defined := fs.Lookup(strings.TrimLeft(arg, "-"))
		if defined == nil {
			// Unknown flag. Leave it for the flag package to complain about,
			// with its own message and its own usage output.
			continue
		}
		if boolFlag, ok := defined.Value.(interface{ IsBoolFlag() bool }); ok && boolFlag.IsBoolFlag() {
			continue
		}

		if i+1 < len(args) {
			i++
			rest = append(rest, args[i])
		}
	}

	return host, rest, nil
}

// parseWithHost parses args for a subcommand that takes exactly one host.
func parseWithHost(fs *flag.FlagSet, args []string) (string, error) {
	host, rest, err := extractHost(fs, args)
	if err != nil {
		fs.Usage()
		return "", err
	}
	if err := fs.Parse(rest); err != nil {
		return "", err
	}
	if host == "" {
		fs.Usage()
		return "", fmt.Errorf("expected a host, for example: 192.168.1.10")
	}
	return host, nil
}
