package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/eyupio/zoomies/internal/cliconfig"
)

// firstRunSetup asks where the controller is when no flag, environment
// variable, connection file or installed configuration names one. The answer
// is saved for the next command, as reet's ResolveURL does, so the question is
// asked once rather than every time.
//
// asked says whether the setup ran at all: a command in a script has nobody to
// answer, and must get the existing error naming all three places instead of
// waiting on a terminal that is not there.
func firstRunSetup(e *env, existing cliConfig) (cfg cliConfig, asked bool, err error) {
	if e == nil || !canAskAt(e.in, e.err) {
		return existing, false, nil
	}
	asked = true

	fmt.Fprintln(e.err, "No controller is configured on this machine.")
	fmt.Fprintln(e.err, "Enter its address once; zoomies saves it for next time.")

	entered, err := askControllerURL(e, "")
	if err != nil {
		return existing, asked, err
	}

	cfg = existing
	cfg.URL = entered
	if existing.Token == "" {
		token, err := askSecret(e, "API token (optional, Enter for an unauthenticated controller)")
		if err != nil {
			return existing, asked, err
		}
		if token != "" {
			cfg.Token = token
		} else {
			fmt.Fprintln(e.err, "No token entered. Read-only commands work where the controller allows it; mint one with `zoomies tokens create`.")
		}
	}

	path, err := cliconfig.Save(cliConfigPath(), cfg)
	if err != nil {
		return existing, asked, err
	}
	fmt.Fprintf(e.err, "Saved the controller in %s\n", path)

	saved, err := loadCLIConfig(path)
	if err != nil {
		return existing, asked, err
	}
	return saved, asked, nil
}

// askControllerURL asks for a controller address and returns one a command can
// dial. A bad answer is a question worth asking again, not a failed first run;
// three attempts is enough to catch a typo without holding a terminal forever.
func askControllerURL(e *env, current string) (string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		value, err := askLine(e.in, e.err, "Controller URL", current)
		if err != nil {
			return "", err
		}
		value = strings.TrimSpace(value)
		if value == "" {
			fmt.Fprintln(e.err, "A controller address is needed, for example https://zoomies.example.com.")
			continue
		}
		// A bare host is what people type first. Assume https, which is what
		// the client does with --url, rather than asking them to type it.
		if !strings.Contains(value, "://") {
			value = "https://" + value
		}
		if err := cliconfig.ValidateURL(value); err != nil {
			fmt.Fprintf(e.err, "%v\n", err)
			continue
		}
		return strings.TrimRight(value, "/"), nil
	}
	return "", errors.New("no usable controller URL was entered; nothing was saved")
}

// joinControllerURL resolves the address `agent join` enrols this host with. A
// join run from a script has nobody to answer, and --non-interactive says so even
// at a terminal; both keep the usage error that names what is missing.
func joinControllerURL(e *env, fs *flagSet, nonInteractive bool) (string, error) {
	switch fs.NArg() {
	case 1:
		return fs.Arg(0), nil
	case 0:
		if canAskAt(e.in, e.err) && !nonInteractive {
			return askControllerURL(e, "")
		}
		return "", usagef("agent join", "needs the controller URL, e.g. https://zoomies.example.com")
	default:
		return "", usagef("agent join", "takes one controller URL, but %d were given", fs.NArg())
	}
}

// joinToken resolves the one-time token a host enrols with. It is not saved: the
// controller spends it on this join, and it is not the CLI's own credential.
func joinToken(e *env, token string, nonInteractive bool) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" && canAskAt(e.in, e.err) && !nonInteractive {
		value, err := askSecret(e, "Join token (Hosts -> Add a host)")
		if err != nil {
			return "", err
		}
		token = strings.TrimSpace(value)
	}
	if token == "" {
		return "", usagef("agent join", "needs --token; mint one in the UI under Hosts -> Add a host, which prints the whole command to paste")
	}
	return token, nil
}

// askLine asks one question and reads one answer, without consuming a later
// answer from the same input. Prompts go to stderr, so a command whose stdout
// is JSON stays parseable.
func askLine(in io.Reader, out io.Writer, label, current string) (string, error) {
	if current != "" {
		fmt.Fprintf(out, "%s [%s]: ", label, current)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}
	value, err := readLine(in)
	if err != nil {
		return "", err
	}
	if value == "" {
		value = current
	}
	return value, nil
}

// askSecret asks for a value that must not be echoed, through the same reader
// the account commands use so that a terminal turns its echo off and a pipe is
// still an ordinary line.
func askSecret(e *env, label string) (string, error) {
	value, err := readSecret(e, label+": ")
	if err != nil {
		return "", err
	}
	if len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00") {
		return "", errors.New("invalid or oversized token")
	}
	return value, nil
}

// readLine reads one line a byte at a time, so that the next question does not
// start with whatever a buffered reader swallowed past the newline.
func readLine(in io.Reader) (string, error) {
	var b strings.Builder
	one := make([]byte, 1)
	for b.Len() <= 4096 {
		n, err := in.Read(one)
		if n > 0 {
			if one[0] == '\n' {
				break
			}
			if one[0] != '\r' {
				b.WriteByte(one[0])
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", errors.New("input ended; nothing was saved")
			}
			return "", err
		}
	}
	value := strings.TrimSpace(b.String())
	if len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00") {
		return "", errors.New("invalid or oversized answer")
	}
	return value, nil
}
