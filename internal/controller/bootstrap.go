package controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/config"
)

// BootstrapFromEnvironment creates the first identity ZOOMIES_BOOTSTRAP_*
// describes, if the database has no accounts yet.
//
// It runs once, at start, beside the setup token. On an empty database it is
// what makes an instance a compose file or a Terraform module brought up
// usable with no human step: nobody is there to read the setup token out of
// the log. Once any account exists the variables are ignored -- they must
// never be a way to add a platform account to an instance somebody has
// already claimed -- and bootstrap.ignored names them, because variables that
// silently do nothing are a credential sitting in a file for no reason.
//
// A malformed request is an error that stops startup: a provisioner waiting
// on /readyz for bootstrap_required to go false would otherwise wait for ever.
func (c *Controller) BootstrapFromEnvironment(ctx context.Context) error {
	b := c.cfg().Bootstrap
	if !b.Requested() {
		return nil
	}
	svc := c.Auth()
	need, err := svc.NeedsBootstrap(ctx)
	if err != nil {
		return err
	}
	if !need {
		c.ignoreBootstrap(b)
		return nil
	}

	in := auth.Unattended{Username: b.Admin}
	if b.TokenFile != "" {
		if in.Token, err = readSecretFile("ZOOMIES_BOOTSTRAP_TOKEN_FILE", b.TokenFile); err != nil {
			return err
		}
	} else {
		if in.Password, err = readSecretFile("ZOOMIES_BOOTSTRAP_PASSWORD_FILE", b.PasswordFile); err != nil {
			return err
		}
	}
	u, tok, err := svc.CreateUnattendedIdentity(ctx, in)
	if errors.Is(err, auth.ErrAlreadyBootstrapped) {
		// Somebody used the setup token between the check above and here.
		c.ignoreBootstrap(b)
		return nil
	}
	if err != nil {
		return fmt.Errorf("creating the first account from ZOOMIES_BOOTSTRAP_ADMIN: %w", err)
	}
	if tok != nil {
		c.log.Info("created the first account from the environment, with the API token the bootstrap file holds",
			"username", u.Username, "role", u.Role, "token_id", tok.ID)
	} else {
		c.log.Info("created the first account from the environment", "username", u.Username, "role", u.Role)
	}
	return nil
}

func (c *Controller) ignoreBootstrap(b config.Bootstrap) {
	vars := b.Variables()
	c.bootstrapIgnored.Store(&vars)
	c.log.Warn("ignoring the bootstrap variables: this instance already has an account",
		"variables", strings.Join(vars, ", "))
}

// bootstrapProblems is the warning that the bootstrap variables did nothing.
func (c *Controller) bootstrapProblems() []Problem {
	p := c.bootstrapIgnored.Load()
	if p == nil || len(*p) == 0 {
		return nil
	}
	names := strings.Join(*p, ", ")
	return []Problem{{
		Code:     "bootstrap.ignored",
		Severity: config.SeverityWarning,
		Title:    names + " set on an instance that already has accounts",
		Detail: "they only ever create the first account on an empty database, so this start ignored them. " +
			"The file they point at is a credential kept for nothing.",
		Fix: "remove " + names + " from the controller's environment, and delete the file they name.",
	}}
}

// readSecretFile reads a bootstrap credential, refusing a file that group or
// other can read -- the rule cryptox.LoadKeyFile applies to the encryption
// key, for the same reason: a platform credential readable by every account
// on the host is not one only the provisioner holds. Windows is skipped for
// the reason LoadKeyFile gives: its mode bits say nothing about its ACL.
func readSecretFile(variable, path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", variable, err)
	}
	if mode := info.Mode().Perm(); runtime.GOOS != "windows" && mode&0o077 != 0 {
		return "", fmt.Errorf("%s: %s is mode %04o; it must not be readable by group or other (chmod 600 %s)",
			variable, path, mode, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", variable, err)
	}
	v := strings.TrimRight(string(b), "\r\n")
	if v == "" {
		return "", fmt.Errorf("%s: %s is empty", variable, path)
	}
	return v, nil
}
