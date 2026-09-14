package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/cryptox"
	"github.com/eyupio/zoomies/internal/store"
)

// `zoomies config get|set|unset|list`: the fleet's settings, from a terminal.
//
// The settings page is where these are normally changed. This exists for the
// two moments when it is not reachable: a stored value that stops the
// controller starting, and a stored value that binds it somewhere nobody can
// get to. Both are recoverable through an environment variable, which is the
// layer above the database -- but a variable is a workaround that has to be
// remembered at every restart, and this is the fix.
//
// It works on a stopped controller. A running one holds the database lock, and
// writing settings under a process that has already read them would leave that
// process and the row disagreeing with no way for either to know.

// openForSettings loads the configuration far enough to open the database and
// the key that unseals what is in it, which is all these commands need.
func openForSettings(ctx context.Context, cfgPath string) (*config.Config, *store.Store, *cryptox.Key, func(), error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	unlock, err := store.Lock(cfg.Database.Path)
	if err != nil {
		if errors.Is(err, store.ErrLocked) {
			return nil, nil, nil, nil, fmt.Errorf(
				"a controller is running against %s, and it has already read the settings this would change; "+
					"stop it first, or change the setting on the settings page", cfg.Database.Path)
		}
		return nil, nil, nil, nil, err
	}
	st, err := store.Open(ctx, store.Options{Path: cfg.Database.Path})
	if err != nil {
		_ = unlock()
		return nil, nil, nil, nil, err
	}
	// A missing key is not fatal here: everything but a credential can still be
	// read and written without one, and refusing the whole command would take
	// away the tool somebody uses when the key is the thing that is wrong.
	key, _ := loadSettingsKey(cfg)
	return cfg, st, key, func() { st.Close(); _ = unlock() }, nil
}

func loadSettingsKey(cfg *config.Config) (*cryptox.Key, error) {
	if raw := strings.TrimSpace(cfg.Security.EncryptionKey); raw != "" {
		return cryptox.ParseKey(raw)
	}
	if path := strings.TrimSpace(cfg.Security.EncryptionKeyFile); path != "" {
		return cryptox.LoadKeyFile(path)
	}
	return nil, os.ErrNotExist
}

// effective assembles the configuration the way the controller does, so these
// commands answer about what a controller would actually run rather than about
// one layer of it.
func effective(ctx context.Context, cfg *config.Config, st *store.Store, key *cryptox.Key) ([]store.InstanceSetting, error) {
	rows, err := st.InstanceSettings(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := cfg.Rebuild(rows, key); err != nil {
		return nil, err
	}
	return rows, nil
}

func runConfigList(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies config list [--config path] [--all]",
		"List what this fleet has stored. --all lists every setting, including the ones nobody has changed.")
	cfgPath := fs.String("config", "", "path to zoomies.yaml (default: "+config.DefaultConfigFile()+")")
	all := fs.Bool("all", false, "list every setting, not only the ones with a value")
	fs.example("zoomies config list", "zoomies config list --all")
	if err := fs.parse(args); err != nil {
		return err
	}
	if err := fs.noMoreArgs(); err != nil {
		return err
	}

	cfg, st, key, done, err := openForSettings(ctx, *cfgPath)
	if err != nil {
		return err
	}
	defer done()
	if _, err := effective(ctx, cfg, st, key); err != nil {
		return err
	}

	w := tabwriter.NewWriter(e.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SETTING\tVALUE\tFROM")
	shown := 0
	for _, s := range config.Settings() {
		source := cfg.Source(s.Key)
		if !*all && source == config.SourceDefault {
			continue
		}
		value, err := cfg.Value(s.Key)
		if err != nil {
			continue
		}
		text := config.Text(s, value)
		if s.Secret {
			text = secretPlaceholder
			if text == "" {
				text = "(not set)"
			}
		}
		if text == "" {
			text = "(not set)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", s.Key, text, source)
		shown++
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if shown == 0 {
		fmt.Fprintln(e.out, "Nothing is set: this fleet is running on the defaults.")
	}
	return nil
}

func runConfigGet(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies config get <key> [--config path]",
		"Print one setting's effective value, and which layer it came from.")
	cfgPath := fs.String("config", "", "path to zoomies.yaml (default: "+config.DefaultConfigFile()+")")
	fs.example("zoomies config get server.bind")
	if err := fs.parse(args); err != nil {
		return err
	}
	key, err := fs.oneArg("a setting, for example server.bind")
	if err != nil {
		return err
	}
	setting, ok := config.LookupSetting(key)
	if !ok {
		return usagef("config get", "%q is not a setting this version has; `zoomies config list --all` lists them", key)
	}

	cfg, st, encKey, done, err := openForSettings(ctx, *cfgPath)
	if err != nil {
		return err
	}
	defer done()
	if _, err := effective(ctx, cfg, st, encKey); err != nil {
		return err
	}

	value, err := cfg.Value(key)
	if err != nil {
		return err
	}
	if setting.Secret {
		if config.Text(setting, value) == "" {
			fmt.Fprintln(e.out, "(not set)")
		} else {
			fmt.Fprintln(e.out, secretPlaceholder)
		}
	} else {
		fmt.Fprintln(e.out, config.Text(setting, value))
	}
	fmt.Fprintf(e.err, "%s -- %s\nfrom: %s\n", setting.Label, setting.Summary, sourceSentence(cfg, setting))
	return nil
}

// sourceSentence says where a value came from in a way that names what to
// change, because "environment" on its own leaves somebody looking for which
// variable.
func sourceSentence(cfg *config.Config, s config.Setting) string {
	switch cfg.Source(s.Key) {
	case config.SourceEnvironment:
		return s.Env + " in the environment, which overrides both the database and the file"
	case config.SourceDatabase:
		return "this fleet's database"
	case config.SourceFile:
		if p := cfg.Path(); p != "" {
			return p
		}
		return "the configuration file"
	default:
		return "the built-in default"
	}
}

func runConfigSet(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies config set <key> <value> [--config path]",
		"Store one setting in this fleet's database. The controller must be stopped.")
	cfgPath := fs.String("config", "", "path to zoomies.yaml (default: "+config.DefaultConfigFile()+")")
	fs.example(
		"zoomies config set server.bind 127.0.0.1:8080",
		"zoomies config set oidc.scopes openid,profile,email",
	)
	if err := fs.parse(args); err != nil {
		return err
	}
	rest, err := fs.atLeastOneArg("a setting and its value, for example server.bind 127.0.0.1:8080")
	if err != nil {
		return err
	}
	if len(rest) != 2 {
		return usagef("config set", "takes a setting and one value; quote a value that contains spaces")
	}
	key, raw := rest[0], rest[1]

	setting, ok := config.LookupSetting(key)
	if !ok {
		return usagef("config set", "%q is not a setting this version has; `zoomies config list --all` lists them", key)
	}
	if !setting.Stored() {
		return usagef("config set", "%s cannot live in the database -- %s; set it in the configuration file or as %s",
			key, scopeReason(setting), setting.Env)
	}

	cfg, st, encKey, done, err := openForSettings(ctx, *cfgPath)
	if err != nil {
		return err
	}
	defer done()
	if _, err := effective(ctx, cfg, st, encKey); err != nil {
		return err
	}

	// Parsed against a copy first, so a value that will not do stops here
	// rather than in the next start.
	candidate := *cfg
	candidate.SetSources(cfg.Sources())
	if _, err := candidate.SetValueString(key, raw); err != nil {
		return err
	}
	parsed, err := candidate.Value(key)
	if err != nil {
		return err
	}
	row, err := config.EncodeStored(setting, parsed, encKey)
	if err != nil {
		return err
	}
	if err := st.PutInstanceSettings(ctx, "zoomies config set", []store.InstanceSetting{row}); err != nil {
		return err
	}

	fmt.Fprintf(e.out, "%s is now %s.\n", setting.Key, displayFor(setting, parsed))
	if cfg.Source(key) == config.SourceEnvironment {
		fmt.Fprintf(e.err, "It will not take effect while %s is set in the environment, which overrides the database.\n", setting.Env)
	} else if !setting.Live {
		fmt.Fprintln(e.err, "It takes effect when the controller next starts.")
	}
	return nil
}

func runConfigUnset(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies config unset <key> [--config path]",
		"Forget a stored setting, so the configuration file or the built-in default decides it again.")
	cfgPath := fs.String("config", "", "path to zoomies.yaml (default: "+config.DefaultConfigFile()+")")
	fs.example("zoomies config unset server.bind")
	if err := fs.parse(args); err != nil {
		return err
	}
	key, err := fs.oneArg("a setting, for example server.bind")
	if err != nil {
		return err
	}
	if _, ok := config.LookupSetting(key); !ok {
		return usagef("config unset", "%q is not a setting this version has; `zoomies config list` lists what is stored", key)
	}

	_, st, _, done, err := openForSettings(ctx, *cfgPath)
	if err != nil {
		return err
	}
	defer done()

	if _, err := st.GetInstanceSetting(ctx, key); errors.Is(err, store.ErrNotFound) {
		fmt.Fprintf(e.out, "%s was not stored, so nothing changed.\n", key)
		return nil
	} else if err != nil {
		return err
	}
	if err := st.DeleteInstanceSettings(ctx, []string{key}); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "%s is no longer stored; the configuration file or the built-in default decides it now.\n", key)
	return nil
}

func scopeReason(s config.Setting) string {
	switch s.Scope {
	case config.ScopeBootstrap:
		return "it is read before the database can be opened"
	case config.ScopeLocal:
		return "it belongs to a standalone agent's own host"
	default:
		return "it is not a setting this fleet stores"
	}
}

func displayFor(s config.Setting, value any) string {
	if s.Secret {
		return secretPlaceholder
	}
	text := config.Text(s, value)
	if text == "" {
		return "empty"
	}
	return text
}
