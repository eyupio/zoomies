package api

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/events"
	"github.com/eyupio/zoomies/internal/naming"
	"github.com/eyupio/zoomies/internal/store"
)

// poolResponse is the shape GET /pools returns, rendered by the controller so
// the event stream's pool.* frames are the same JSON. See controller/views.go
// for why the renderer lives there.
type poolResponse = controller.PoolView

// poolFor renders a pool for the caller in front of it.
//
// Everything on the view is a viewer's to see except the environment a pool
// injects into its runners, which is where a registry password ends up if one
// is anywhere. Setting a pool's env is an operator action, so reading the
// values back is one too; a viewer gets the keys, which is all the pool page
// draws.
func poolFor(r *http.Request, v controller.PoolView) controller.PoolView {
	if auth.Allowed(Identity(r.Context()), auth.ActionPoolsWrite) {
		return v
	}
	return v.WithoutEnvValues()
}

// poolForAudit returns the pool as an audit row should record it: every env key
// kept, every env value gone.
//
// auth.Redact cannot do this on its own. It matches names, so it blanks
// GITHUB_TOKEN and leaves DEPLOY_KEY, NEXUS_PW and REGISTRY_AUTH untouched --
// an operator names their own variables, and a name is not a reliable signal of
// what stands behind it. So the values come out here, before the row is
// written, rather than being trusted to a word list.
//
// Unlike poolFor this is not role-based, and that is the point. A response is
// rendered for one caller and gone; an audit row is kept for the life of the
// database, is never pruned, and is readable at viewer -- so a secret written
// into one is readable by every viewer from then on, and no later fix can
// retract it. The keys stay, because which variable changed is what the row is
// read for.
func poolForAudit(p *store.Pool) *store.Pool {
	if p == nil || len(p.Env) == 0 {
		return p
	}
	blanked := *p
	blanked.Env = make(store.StringMap, len(p.Env))
	for k := range p.Env {
		blanked.Env[k] = ""
	}
	return &blanked
}

// handleListPools answers GET /api/v1/pools.
func (s *Server) handleListPools(w http.ResponseWriter, r *http.Request) {
	pools, err := s.ctrl.Store().ListPools(r.Context())
	if err != nil {
		s.internal(w, r, "listing pools", err)
		return
	}
	view, err := s.ctrl.PoolRenderer(r.Context())
	if err != nil {
		s.internal(w, r, "listing pools", err)
		return
	}
	out := make([]poolResponse, 0, len(pools))
	for _, p := range pools {
		out = append(out, poolFor(r, view.View(p)))
	}
	writeJSON(w, http.StatusOK, newList(out))
}

// handleGetPool answers GET /api/v1/pools/{id}.
func (s *Server) handleGetPool(w http.ResponseWriter, r *http.Request) {
	p, err := s.ctrl.Store().GetPool(r.Context(), chiURLParam(r, "id"))
	if err != nil {
		s.fail(w, r, "reading the pool", err)
		return
	}
	view, err := s.ctrl.PoolRenderer(r.Context())
	if err != nil {
		s.internal(w, r, "reading the pool", err)
		return
	}
	writeJSON(w, http.StatusOK, poolFor(r, view.View(p)))
}

// ---------------------------------------------------------------------------
// Creating and editing
// ---------------------------------------------------------------------------

// poolInput is PoolCreate and PoolUpdate in one type.
//
// Every field is a pointer so that a PATCH can tell "leave this alone" from
// "set it to zero": without that, editing a pool's name would silently reset
// min_runners to 0, which on a pool with warm runners is a fleet-wide change
// nobody asked for.
type poolInput struct {
	Name                   *string            `json:"name"`
	InstallationID         *string            `json:"installation_id"`
	Labels                 *[]string          `json:"labels"`
	RunnerGroup            *string            `json:"runner_group"`
	Backend                *string            `json:"backend"`
	Platform               *store.Platform    `json:"platform"`
	Image                  *string            `json:"image"`
	PullPolicy             *string            `json:"pull_policy"`
	RunnerVersion          *string            `json:"runner_version"`
	MinRunners             *int               `json:"min_runners"`
	MaxRunners             *int               `json:"max_runners"`
	RepositoryScaleUpLimit *int               `json:"repository_scale_up_limit"`
	CostPerRunnerHour      *float64           `json:"cost_per_runner_hour"`
	Priority               *int               `json:"priority"`
	IdleTimeout            *string            `json:"idle_timeout"`
	Ephemeral              *bool              `json:"ephemeral"`
	DockerMode             *string            `json:"docker_mode"`
	Resources              *store.Resources   `json:"resources"`
	Cache                  *store.CacheConfig `json:"cache"`
	HostSelector           *map[string]string `json:"host_selector"`
	Env                    *map[string]string `json:"env"`
	RunAsRoot              *bool              `json:"run_as_root"`
	Enabled                *bool              `json:"enabled"`
}

// defaultPool is a new pool before the request is applied: the defaults the
// OpenAPI document states, so that a minimal create produces the same pool the
// wizard's review step showed.
func (s *Server) defaultPool() *store.Pool {
	return &store.Pool{
		Backend: store.BackendDocker,
		// No image: a pool that names none follows its platform, and a pool
		// with no platform follows the instance default. Pinning the default
		// here would freeze every pool on whatever the default was the day it
		// was created.
		Image:       "",
		PullPolicy:  store.PullIfNotPresent,
		MinRunners:  0,
		MaxRunners:  4,
		IdleTimeout: store.Duration(5 * time.Minute),
		Ephemeral:   true,
		DockerMode:  store.DockerNone,
		Cache:       store.CacheConfig{Scope: store.CacheScopePool},
		Enabled:     true,
	}
}

// apply folds the request into a pool, returning the field errors it could not.
//
// Parsing and validation are the same pass on purpose: "idle_timeout: 5 munutes"
// is a validation failure with a field name, not a 400 about JSON.
func (in *poolInput) apply(p *store.Pool) []fieldError {
	var errs []fieldError
	add := func(field, msg string) { errs = append(errs, fieldError{field, msg}) }

	if in.Name != nil {
		// Branded here as well as in the store, so that the uniqueness check
		// below and the error messages that quote the name are talking about
		// the name the pool will actually have.
		p.Name = store.BrandedName(*in.Name)
	}
	if in.InstallationID != nil {
		p.InstallationID = strings.TrimSpace(*in.InstallationID)
	}
	if in.Labels != nil {
		// Every pool answers to the brand as well as to whatever it was given,
		// so that "runs-on: zoomies" reaches this fleet without naming one of
		// its pools. That is the label the migration wizard writes into a
		// repository that has not yet been assigned to a pool, and a pool that
		// quietly dropped it would take no work from those repositories.
		p.Labels = store.BrandLabels(*in.Labels)
	}
	if in.RunnerGroup != nil {
		p.RunnerGroup = strings.TrimSpace(*in.RunnerGroup)
	}
	if in.Backend != nil {
		p.Backend = store.BackendKind(strings.ToLower(strings.TrimSpace(*in.Backend)))
	}
	if in.Platform != nil {
		// Keep what the operator sent rather than the normalised form: an
		// operating system Zoomies does not know has to survive as far as the
		// validator, which can then say so by name.
		p.Platform = store.Platform{
			OS:        strings.ToLower(strings.TrimSpace(in.Platform.OS)),
			OSVersion: strings.TrimSpace(in.Platform.OSVersion),
			Arch:      strings.ToLower(strings.TrimSpace(in.Platform.Arch)),
		}
	}
	if in.Image != nil {
		p.Image = strings.TrimSpace(*in.Image)
	}
	if in.PullPolicy != nil {
		p.PullPolicy = store.PullPolicy(strings.ToLower(strings.TrimSpace(*in.PullPolicy)))
	}
	if in.RunnerVersion != nil {
		p.RunnerVersion = strings.TrimSpace(*in.RunnerVersion)
	}
	if in.MinRunners != nil {
		p.MinRunners = *in.MinRunners
	}
	if in.MaxRunners != nil {
		p.MaxRunners = *in.MaxRunners
	}
	if in.RepositoryScaleUpLimit != nil {
		p.RepositoryScaleUpLimit = *in.RepositoryScaleUpLimit
	}
	if in.CostPerRunnerHour != nil {
		p.CostPerRunnerHour = in.CostPerRunnerHour
	}
	if in.Priority != nil {
		p.Priority = *in.Priority
	}
	if in.IdleTimeout != nil {
		raw := strings.TrimSpace(*in.IdleTimeout)
		switch {
		case raw == "":
			p.IdleTimeout = 0
		default:
			d, err := time.ParseDuration(raw)
			switch {
			case err != nil:
				add("idle_timeout", fmt.Sprintf("%q is not a duration; write it like 5m, 30s or 1h30m", raw))
			case d < 0:
				add("idle_timeout", "an idle timeout cannot be negative; use 0 to keep idle runners until something else removes them")
			default:
				p.IdleTimeout = store.Duration(d)
			}
		}
	}
	if in.Ephemeral != nil {
		p.Ephemeral = *in.Ephemeral
	}
	if in.DockerMode != nil {
		p.DockerMode = store.DockerMode(strings.ToLower(strings.TrimSpace(*in.DockerMode)))
	}
	if in.Resources != nil {
		p.Resources = *in.Resources
	}
	if in.Cache != nil {
		p.Cache = *in.Cache
		p.Cache.Source = strings.TrimSpace(p.Cache.Source)
	}
	if in.HostSelector != nil {
		p.HostSelector = store.StringMap(*in.HostSelector)
	}
	if in.Env != nil {
		p.Env = store.StringMap(*in.Env)
	}
	if in.RunAsRoot != nil {
		p.RunAsRoot = *in.RunAsRoot
	}
	if in.Enabled != nil {
		p.Enabled = *in.Enabled
	}
	// The image the pool will actually run. A pool that gives its jobs a daemon
	// cannot use the stock image, which has no client for it, so the stock
	// image is swapped for its Docker variant here, as the request is folded
	// in, rather than left for the operator to remember: the response, the
	// audit row and the pool's page then all show the image that runs, and the
	// wizard's dry run agrees with the create it precedes. What is and is not
	// swapped is config.RunnerImageFor's to say.
	p.Image = config.RunnerImageFor(p.Image, p.DockerMode.GivesDaemon())
	return errs
}

// platformOption is one row of the runner image catalogue, as the pool wizard
// needs it: what to select, what to call it, and which architectures it can be
// asked for.
type platformOption struct {
	OS        string   `json:"os"`
	OSVersion string   `json:"os_version"`
	Label     string   `json:"label"`
	Image     string   `json:"image"`
	Arches    []string `json:"arches"`
	Default   bool     `json:"default"`
}

// handlePoolPlatforms answers GET /api/v1/pools/platforms.
//
// The catalogue is compiled into the binary, so this is a constant list. It is
// served rather than duplicated in the UI because a dropdown offering an
// operating system no image is published for is a pool that validates and then
// never starts a runner.
func (s *Server) handlePoolPlatforms(w http.ResponseWriter, _ *http.Request) {
	images := naming.Images()
	out := make([]platformOption, 0, len(images))
	for _, img := range images {
		out = append(out, platformOption{
			OS:        img.OS,
			OSVersion: img.Version,
			Label:     naming.PrettyOS(img.OS) + " " + img.Version,
			Image:     img.Ref(),
			Arches:    img.Arches,
			Default:   img.Default,
		})
	}
	writeJSON(w, http.StatusOK, newList(out))
}

// validatePlatform checks the machine a pool asks for.
//
// The rule it enforces is that a pool must be startable: either it names an
// image, or the platform it names is one Zoomies publishes an image for. A
// pool that asks for Ubuntu 20.04 and no image would otherwise be accepted and
// then fail at every create, which is a much later and much worse place to
// find out.
func validatePlatform(p *store.Pool) []fieldError {
	var errs []fieldError
	add := func(field, msg string) { errs = append(errs, fieldError{field, msg}) }

	if p.Platform.OS != "" && naming.NormalizeOS(p.Platform.OS) == "" {
		add("platform.os", fmt.Sprintf("%q is not an operating system Zoomies knows; use one of %s, or leave it blank and name an image instead",
			p.Platform.OS, naming.SupportedPlatforms()))
	}
	if p.Platform.Arch != "" && naming.NormalizeArch(p.Platform.Arch) == "" {
		add("platform.arch", fmt.Sprintf("%q is not an architecture Zoomies runs on; use amd64 or arm64", p.Platform.Arch))
	}
	if p.Backend == store.BackendProcess {
		// There is no image: the host itself is the environment, so nothing
		// below applies.
		return errs
	}

	os := naming.NormalizeOS(p.Platform.OS)
	if p.Image == "" && os != "" {
		img, ok := naming.FindImage(os, p.Platform.OSVersion)
		switch {
		case !ok:
			add("platform.os_version", fmt.Sprintf("no runner image is published for %s %s; Zoomies publishes %s, or name an image of your own",
				naming.PrettyOS(os), p.Platform.OSVersion, naming.SupportedPlatforms()))
		case !img.Supports(p.Platform.Arch):
			add("platform.arch", fmt.Sprintf("%s is not published for %s; it is built for %s",
				img.Ref(), p.Platform.Arch, strings.Join(img.Arches, " and ")))
		}
	}
	return errs
}

// validatePool checks a pool the way the creation wizard does, and for the same
// reasons, so that the review step and the server never disagree.
func (s *Server) validatePool(ctx context.Context, p *store.Pool, existingID string) []fieldError {
	var errs []fieldError
	add := func(field, msg string) { errs = append(errs, fieldError{field, msg}) }

	switch {
	case p.Name == "":
		add("name", "a pool needs a name; it is how you will refer to it in the UI and on the CLI")
	case len([]rune(p.Name)) > 64:
		add("name", "a pool name must be 64 characters or fewer")
	default:
		if existing, err := s.ctrl.Store().GetPoolByName(ctx, p.Name); err == nil && existing.ID != existingID {
			add("name", fmt.Sprintf("a pool called %q already exists; pick another name", p.Name))
		}
	}

	if p.InstallationID == "" {
		add("installation_id", "a pool has to belong to a GitHub App installation; connect one on the Installations page first")
	} else if _, err := s.ctrl.Store().GetInstallation(ctx, p.InstallationID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			add("installation_id", fmt.Sprintf("there is no installation %s; it may have been removed since this form was opened", p.InstallationID))
		} else {
			add("installation_id", "the installation could not be read: "+err.Error())
		}
	}

	switch {
	case len(p.Labels) == 0:
		add("labels", "a pool needs at least one label your workflows can ask for")
	case !hasDistinctiveLabel(p.Labels):
		add("labels", fmt.Sprintf("a pool needs at least one label of its own: %q is on every Zoomies pool and the rest are labels every runner advertises anyway, so nothing would ever select this pool in particular. Try %q.",
			store.BrandLabel, store.BrandedLabel(p.Name)))
	}
	for _, l := range p.Labels {
		if strings.ContainsAny(l, " ,") {
			add("labels", fmt.Sprintf("%q contains a space or a comma; GitHub labels may not", l))
			break
		}
	}

	if !p.Backend.Valid() {
		add("backend", fmt.Sprintf("%q is not a backend; use docker, podman or process", p.Backend))
	}
	if !p.DockerMode.Valid() {
		add("docker_mode", fmt.Sprintf("%q is not a docker mode; use none, dind or host-socket", p.DockerMode))
	}
	if p.Backend == store.BackendProcess && p.DockerMode != store.DockerNone && p.DockerMode != "" {
		add("docker_mode", "the process backend runs jobs directly on the host, so it cannot give them a Docker daemon of their own; use the docker or podman backend, or set docker_mode to none")
	}
	// An empty image is only a problem when nothing else can supply one: a
	// pool that names its platform gets the variant that platform selects, and
	// one that names neither gets the instance default.
	if p.Image == "" && p.Platform.OS == "" && p.Backend != store.BackendProcess &&
		strings.TrimSpace(s.cfg().GitHub.RunnerImage) == "" {
		add("image", "a container backend needs a runner image; leave it blank only for the process backend, "+
			"or name an operating system and let its published image be used")
	}
	if !p.PullPolicy.Valid() {
		add("pull_policy", "use if-not-present, always, or pinned-only")
	}
	if p.PullPolicy == store.PullPinnedOnly && !digestReference(p.Image) {
		add("image", "pinned-only requires an immutable digest reference such as image@sha256:…; mutable tags are rejected")
	}
	errs = append(errs, validatePlatform(p)...)

	if p.MinRunners < 0 {
		add("min_runners", "the minimum cannot be negative")
	}
	if p.RepositoryScaleUpLimit < 0 {
		add("repository_scale_up_limit", "must be zero or greater")
	}
	if p.CostPerRunnerHour != nil && *p.CostPerRunnerHour < 0 {
		add("cost_per_runner_hour", "must be zero or greater")
	}
	if p.MaxRunners < 1 {
		add("max_runners", "a pool that may have no runners can never run a job; set at least 1")
	}
	if p.MinRunners > p.MaxRunners {
		add("min_runners", fmt.Sprintf("the minimum (%d) is above the maximum (%d); warm runners cannot exceed the cap", p.MinRunners, p.MaxRunners))
	}
	if p.Ephemeral && p.MinRunners > 0 && p.IdleTimeout.Duration() == 0 {
		add("idle_timeout", "warm ephemeral runners with no idle timeout are replaced after every job and never reaped; give an idle timeout, or set min_runners to 0")
	}

	if p.Resources.CPUs < 0 {
		add("resources.cpus", "a CPU limit cannot be negative; use 0 for no limit")
	}
	if p.Resources.MemoryMB < 0 {
		add("resources.memory_mb", "a memory limit cannot be negative; use 0 for no limit")
	}
	if p.Resources.DiskGB < 0 {
		add("resources.disk_gb", "a disk limit cannot be negative; use 0 for no limit")
	}
	if p.Resources.PidsLimit < 0 {
		add("resources.pids_limit", "a process limit cannot be negative; use 0 for no limit")
	}
	if p.Cache.Enabled {
		if !p.Cache.Scope.Valid() {
			add("cache.scope", "use pool or repository")
		}
		if p.Cache.SizeLimit < 0 {
			add("cache.size_limit", "the cache size limit cannot be negative; use 0 for no limit")
		}
		if strings.Contains(p.Cache.Source, "..") {
			add("cache.source", "path traversal is not allowed")
		}
		// A size limit is kept by evicting entries from a directory on the
		// host. There is no directory to measure behind a named volume, so
		// accepting the number there would promise an enforcement that does
		// not exist -- which is worse than refusing it.
		if p.Cache.SizeLimit > 0 && !filepath.IsAbs(strings.TrimSpace(p.Cache.Source)) {
			add("cache.size_limit", "a size limit is enforced by evicting from a host directory, so set the cache source to an absolute host path, or leave the limit at 0")
		}
		if p.Cache.Scope == store.CacheScopeRepository {
			repo := strings.TrimSpace(p.Cache.Repository)
			inst, err := s.ctrl.Store().GetInstallation(ctx, p.InstallationID)
			switch {
			case err != nil:
				// The installation itself is already reported as invalid.
			case inst.TargetType == store.TargetRepo:
				if repo != "" && !strings.EqualFold(repo, inst.Target) {
					add("cache.repository", "this pool's installation is scoped to "+inst.Target+", so its repository cache can only be for that repository; leave it empty")
				}
			case repo == "":
				add("cache.repository", "this pool's installation covers all of "+inst.Target+", so a repository cache has to name the repository it is for, as "+inst.Target+"/name")
			case !validRepositoryPath(repo):
				add("cache.repository", "name the repository as owner/name, for example "+inst.Target+"/widgets")
			case !strings.EqualFold(strings.SplitN(repo, "/", 2)[0], inst.Target):
				add("cache.repository", "this pool's installation covers "+inst.Target+", so its cache repository has to be under that owner")
			}
		}
	}
	for k := range p.Env {
		if strings.TrimSpace(k) == "" {
			add("env", "an environment variable needs a name")
			break
		}
	}
	for k := range p.HostSelector {
		if strings.TrimSpace(k) == "" {
			add("host_selector", "a host selector key cannot be empty")
			break
		}
	}
	return errs
}

// validRepositoryPath accepts exactly "owner/name" with both halves present and
// nothing that could climb out of a cache directory built from it.
func validRepositoryPath(repo string) bool {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return false
	}
	return owner != "." && owner != ".." && name != "." && name != ".."
}

func digestReference(ref string) bool {
	parts := strings.Split(ref, "@sha256:")
	if len(parts) != 2 || parts[0] == "" || len(parts[1]) != 64 {
		return false
	}
	for _, c := range parts[1] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

// hasDistinctiveLabel reports whether a pool advertises anything beyond the
// labels every actions/runner binary advertises anyway. A pool of only implicit
// labels would match every job in the organisation, which is never what an
// operator meant.
func hasDistinctiveLabel(labels []string) bool {
	return slices.ContainsFunc(labels, func(l string) bool {
		l = store.NormalizeLabel(l)
		// The brand is on every pool, so it distinguishes this one from the
		// others no better than "self-hosted" does. A fleet reached only by
		// "runs-on: zoomies" is a fleet where no workflow can say which pool
		// it meant.
		return !store.ImplicitLabels[l] && l != store.BrandLabel
	})
}

// handleCreatePool answers POST /api/v1/pools.
func (s *Server) handleCreatePool(w http.ResponseWriter, r *http.Request) {
	var in poolInput
	if !decode(w, r, &in) {
		return
	}
	p := s.defaultPool()
	errs := in.apply(p)
	// Organisation installations get the dedicated group provisioned by the
	// connection probe. An omitted group means "use the Zoomies isolation
	// boundary", while an explicit empty string still means GitHub Default.
	if in.RunnerGroup == nil && p.InstallationID != "" {
		if inst, err := s.ctrl.Store().GetInstallation(r.Context(), p.InstallationID); err == nil && inst.TargetType == store.TargetOrg {
			p.RunnerGroup = controller.ManagedRunnerGroupName
		}
	}
	errs = append(errs, s.validatePool(r.Context(), p, "")...)
	if len(errs) > 0 {
		unprocessable(w, "this pool cannot be created as described", errs)
		return
	}

	if err := s.ctrl.Store().CreatePool(r.Context(), p); err != nil {
		s.fail(w, r, "creating the pool", err)
		return
	}

	s.auth.Auditor().Created(r.Context(), Identity(r.Context()), "pool", p.ID, poolForAudit(p))
	s.ctrl.PublishPool(r.Context(), events.KindPoolCreated, p)
	// A new pool with a minimum above zero has runners to create; a new pool
	// with none may still claim jobs that are queued right now.
	s.ctrl.Nudge()
	_, _ = s.ctrl.PrewarmPool(r.Context(), p)

	view, err := s.ctrl.PoolRenderer(r.Context())
	if err != nil {
		s.internal(w, r, "reading the pool back", err)
		return
	}
	writeJSON(w, http.StatusCreated, poolFor(r, view.View(p)))
}

// validatePoolResponse is the wizard's review step: the errors that would stop
// the pool being created, the warnings it would produce, and how many hosts
// could actually run it.
type validatePoolResponse struct {
	Valid         bool                 `json:"valid"`
	Errors        []fieldError         `json:"errors"`
	Warnings      []controller.Problem `json:"warnings"`
	MatchingHosts int                  `json:"matching_hosts"`
	// SelectedHosts is how many hosts this pool's host selector reaches, and
	// ExcludedHosts is every one of those the fleet could not run it on, with
	// the reason. The wizard's placement step counts hosts by the selector
	// alone -- it is asked before the backend and the size are known -- so
	// without these two the review step's smaller number reads as the wizard
	// contradicting itself two clicks later.
	SelectedHosts int                        `json:"selected_hosts"`
	ExcludedHosts []controller.HostExclusion `json:"excluded_hosts"`
	// Image is the image the pool would actually run, which is not always the
	// one the request named: a pool that gives its jobs a daemon runs the
	// stock image's Docker variant, and the review step should show that
	// rather than promise a pool the server will not make.
	Image string `json:"image"`
}

// handleValidatePool answers POST /api/v1/pools/validate. It creates nothing.
//
// `?id=` names the pool this is a dry run of an edit to, and it matters: without
// it the name check compares against every pool including that one, so opening
// a pool, changing its image and pressing on was refused with "a pool called
// zoomies-linux-x64 already exists" -- about itself. The form could not be
// saved at all without also renaming the pool.
func (s *Server) handleValidatePool(w http.ResponseWriter, r *http.Request) {
	var in poolInput
	if !decode(w, r, &in) {
		return
	}
	p := s.defaultPool()
	errs := in.apply(p)
	errs = append(errs, s.validatePool(r.Context(), p, r.URL.Query().Get("id"))...)

	fit, err := s.ctrl.HostFit(r.Context(), p)
	if err != nil {
		s.internal(w, r, "counting the hosts that could run this pool", err)
		return
	}
	excluded := fit.Excluded
	if excluded == nil {
		excluded = []controller.HostExclusion{}
	}
	// The installation is part of what a warning is about; when it does not
	// exist, validatePool has already said so in the errors.
	var inst *store.Installation
	if i, err := s.ctrl.Store().GetInstallation(r.Context(), p.InstallationID); err == nil {
		inst = i
	}
	warnings := controller.PoolWarnings(p, inst)
	if fit.Count == 0 {
		why, fix := noHostWarning(p, fit)
		warnings = append(warnings, controller.Problem{
			Code:         "pool.no_matching_hosts",
			Severity:     config.SeverityWarning,
			Title:        "no host can run this pool as configured",
			Detail:       why,
			Fix:          fix,
			Alternatives: fit.Alternatives,
		})
	}
	if errs == nil {
		errs = []fieldError{}
	}
	if warnings == nil {
		warnings = []controller.Problem{}
	}
	writeJSON(w, http.StatusOK, validatePoolResponse{
		Valid:         len(errs) == 0,
		Errors:        errs,
		Warnings:      warnings,
		MatchingHosts: fit.Count,
		SelectedHosts: fit.Selected,
		ExcludedHosts: excluded,
		Image:         p.Image,
	})
}

// noHostWarning says why nothing can run this pool and what to change, choosing
// the sentence from the rule the hosts actually failed rather than assuming the
// commonest one. An operator told to "make the docker backend usable" on a host
// whose only problem is that it has four CPUs goes and looks at a daemon that
// was working all along.
func noHostWarning(p *store.Pool, fit controller.HostFit) (why, fix string) {
	if fit.Selected == 0 {
		// A selector on what the agent reports about its machine is answered
		// by joining such a machine, not by labelling one: an operator told
		// to "label a host" for os=windows would put a Linux box in a Windows
		// pool, which is exactly the placement the selector exists to stop.
		if platform := platformSelector(p.HostSelector); platform != "" {
			return fmt.Sprintf("no host reports itself as %s, so every runner this pool asks for would wait for a host that does not exist.", platform),
				fmt.Sprintf("join a %s host to this fleet, or change the host selector to a platform it already has.", platform)
		}
		return "no host matches this pool's host selector, so every runner it asks for would wait for a host that does not exist.",
			"relax the host selector, or label a host to match it."
	}
	counts := map[string]int{}
	for _, ex := range fit.Excluded {
		counts[ex.Code]++
	}
	switch {
	case counts[controller.ExcludedSize] > 0 && counts[controller.ExcludedSize] == len(fit.Excluded):
		// The one refusal that is about a number an operator typed rather than
		// about the fleet, so the numbers go in the sentence.
		why = fmt.Sprintf("no host this pool's selector reaches is big enough for one of its runners, "+
			"so every runner it asks for would wait for a machine that does not exist. %s", hostDetail(fit))
		fix = "lower this pool's CPU, memory or disk request, turn off Docker in Docker if it is not needed, or add a host large enough to run one."
	case counts[controller.ExcludedPlatform] > 0 && p.Platform.Describe() != "":
		platform := p.Platform.Describe()
		why = fmt.Sprintf("no healthy, uncordoned host is running %s with the %s backend, "+
			"so every runner this pool asks for would wait for a host that does not exist.", platform, p.Backend)
		fix = fmt.Sprintf("add a %s host, or change this pool's platform to one your fleet already has.", platform)
	case counts[controller.ExcludedUnavailable] == len(fit.Excluded):
		why = fmt.Sprintf("every host this pool's selector reaches is cordoned, not heartbeating, or running an agent this "+
			"controller cannot talk to, so nothing would be placed there. %s", hostDetail(fit))
		fix = "uncordon a host, check that its agent is running and can reach this controller, or add one that can take work."
	default:
		why = fmt.Sprintf("no healthy, uncordoned host offers the %s backend and matches this pool's host selector, "+
			"so every runner it asks for would wait for a host that does not exist. %s", p.Backend, hostDetail(fit))
		fix = fmt.Sprintf("make the %s backend usable on that host%s.", p.Backend, switchTo(fit.Alternatives))
	}
	return strings.TrimSpace(why), fix
}

// platformSelector renders a host selector that asks only about the machine
// -- the os and arch keys every agent answers for -- as prose, e.g.
// "Windows, arm64". It is empty for a selector that asks about anything else,
// because a label an operator invented is theirs to explain.
func platformSelector(sel map[string]string) string {
	var parts []string
	for k, v := range sel {
		switch strings.ToLower(strings.TrimSpace(k)) {
		case store.LabelOS:
			if os := naming.PrettyOS(v); os != "" {
				parts = append([]string{os}, parts...)
				continue
			}
		case store.LabelArch:
			if arch := naming.NormalizeArch(v); arch != "" {
				parts = append(parts, arch)
				continue
			}
		}
		return ""
	}
	return strings.Join(parts, ", ")
}

// hostDetail names the hosts that were turned down and why, up to the point
// where a paragraph stops being read. The names matter: an operator who can see
// which machine was refused can check it, and a count alone sends them round
// the whole fleet.
func hostDetail(fit controller.HostFit) string {
	const most = 3
	parts := make([]string, 0, most)
	for _, ex := range fit.Excluded {
		if len(parts) == most {
			parts = append(parts, fmt.Sprintf("and %d more", len(fit.Excluded)-most))
			break
		}
		parts = append(parts, ex.Host+": "+ex.Reason)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ") + "."
}

// switchTo names the backends a pool could move to instead, or says plainly
// that there are none. It is the wizard's half of the sentence the scheduler
// writes for a pool that already exists, kept in the same words on purpose:
// the warning before creation and the problem after it are the same fact.
func switchTo(alternatives []string) string {
	switch len(alternatives) {
	case 0:
		return "; your hosts offer no other backend either"
	case 1:
		return ", or point this pool at " + alternatives[0] + ", which they already offer"
	default:
		return ", or point this pool at a backend they already offer: " + strings.Join(alternatives, ", ")
	}
}

// handleUpdatePool answers PATCH /api/v1/pools/{id}.
func (s *Server) handleUpdatePool(w http.ResponseWriter, r *http.Request) {
	id := chiURLParam(r, "id")
	existing, err := s.ctrl.Store().GetPool(r.Context(), id)
	if err != nil {
		s.fail(w, r, "reading the pool", err)
		return
	}

	var in poolInput
	if !decode(w, r, &in) {
		return
	}
	before := *existing
	updated := *existing
	errs := in.apply(&updated)
	errs = append(errs, s.validatePool(r.Context(), &updated, id)...)
	if len(errs) > 0 {
		unprocessable(w, "this pool cannot be changed as described", errs)
		return
	}

	if err := s.ctrl.Store().UpdatePool(r.Context(), &updated); err != nil {
		s.fail(w, r, "saving the pool", err)
		return
	}

	s.auth.Auditor().Updated(r.Context(), Identity(r.Context()), "pool", id, poolForAudit(&before), poolForAudit(&updated))
	s.ctrl.PublishPool(r.Context(), events.KindPoolUpdated, &updated)
	// The pool's shape decides how many runners should exist, so the scheduler
	// should look again rather than wait out its interval.
	s.ctrl.Nudge()
	// A daemon that comes or goes can change the image the runners are made
	// from without changing the row -- a pool on the controller's default
	// image has nothing in its own image field to differ -- so it counts as
	// an image change here too.
	if before.Image != updated.Image || before.DockerMode.GivesDaemon() != updated.DockerMode.GivesDaemon() ||
		before.PullPolicy != updated.PullPolicy || before.Backend != updated.Backend || !maps.Equal(before.HostSelector, updated.HostSelector) {
		_, _ = s.ctrl.PrewarmPool(r.Context(), &updated)
	}

	view, verr := s.ctrl.PoolRenderer(r.Context())
	if verr != nil {
		s.internal(w, r, "reading the pool back", verr)
		return
	}
	writeJSON(w, http.StatusOK, poolFor(r, view.View(&updated)))
}

func (s *Server) handlePrewarmPool(w http.ResponseWriter, r *http.Request) {
	p, err := s.ctrl.Store().GetPool(r.Context(), chiURLParam(r, "id"))
	if err != nil {
		s.fail(w, r, "reading the pool", err)
		return
	}
	n, err := s.ctrl.PrewarmPool(r.Context(), p)
	if err != nil {
		if errors.Is(err, controller.ErrPrewarmUnsupported) {
			unprocessable(w, err.Error(), nil)
			return
		}
		s.fail(w, r, "queueing the image pull", err)
		return
	}
	states, err := s.ctrl.Store().ListPoolPrewarms(r.Context(), p.ID)
	if err != nil {
		s.internal(w, r, "reading prewarm state", err)
		return
	}
	// Every mutating operator route writes a row, and this one pulls an image
	// onto every host that matches the pool: minutes of network on somebody
	// else's machines, and the one action of the set that left no trace.
	s.auth.Auditor().Act(r.Context(), Identity(r.Context()), "pool.prewarm", "pool", p.ID, map[string]any{
		"image": s.ctrl.RunnerImage(p), "hosts": n,
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": n, "hosts": states})
}

// deletePoolResponse says how much of the fleet the deletion took with it.
type deletePoolResponse struct {
	RunnersAffected int `json:"runners_affected"`
}

// handleDeletePool answers DELETE /api/v1/pools/{id}.
//
// Draining is the default and the only safe choice while work is in flight: the
// runners finish their current job and then go. force tears them down now,
// interrupting whatever they were running, which is sometimes exactly what an
// operator wants and is never what they should get by accident.
//
// A drain the pool does not outlive is not a drain. The pool row used to go in
// the same request that started one, and deleting it takes its runners' rows
// with it -- so the next heartbeat reported runners the controller had no
// record of, the agent stopped tracking them, and its orphan sweep destroyed
// the containers about two minutes later, killing the jobs the drain existed to
// protect. So the delete is refused while any runner is still going, and the
// operator calls again once they have finished. The drain has already been
// started by then, which is what makes the second call the short one.
//
// An idle pool still goes in one call: a runner that is not busy is removed
// outright rather than drained, so it is terminal by the time this looks. Only
// a pool with work on it needs the second call, and force skips the wait by
// killing the jobs.
func (s *Server) handleDeletePool(w http.ResponseWriter, r *http.Request) {
	id := chiURLParam(r, "id")
	force := queryBool(r, "force", false)
	drain := queryBool(r, "drain", true)

	p, err := s.ctrl.Store().GetPool(r.Context(), id)
	if err != nil {
		s.fail(w, r, "reading the pool", err)
		return
	}
	runners, err := s.ctrl.Store().ListRunnersForPool(r.Context(), id)
	if err != nil {
		s.internal(w, r, "listing the pool's runners", err)
		return
	}

	affected := 0
	for _, run := range runners {
		if run.State.Terminal() {
			continue
		}
		var rerr error
		_, rerr = s.ctrl.RemoveRunner(r.Context(), run.ID, "pool "+p.Name+" was deleted", force || !drain, true)
		if rerr != nil {
			// One runner that cannot be told to stop must not leave the pool
			// half-deleted; the row goes either way and the reaper cleans up.
			s.logger(r).Warn("could not stop a runner while deleting its pool",
				"pool", id, "runner", run.ID, "error", rerr)
			continue
		}
		affected++
	}

	// Read the runners back rather than trusting the loop above: a drain leaves
	// a runner in draining, which is not terminal, and only the agent reporting
	// the stop moves it on.
	remaining, err := s.ctrl.Store().ListRunnersForPool(r.Context(), id)
	if err != nil {
		s.internal(w, r, "checking whether the pool's runners have finished", err)
		return
	}
	live := 0
	for _, run := range remaining {
		if !run.State.Terminal() {
			live++
		}
	}
	if live > 0 {
		conflict(w, fmt.Sprintf("%s still has %s finishing, so it was not deleted: deleting the pool now would "+
			"take their records with it and the jobs they are running would be killed a couple of minutes later. "+
			"They have been told to stop and will go as their jobs finish; delete the pool again once they have. "+
			"To stop them now and lose those jobs, delete it with force=true.",
			p.Name, runnerCount(live)))
		return
	}

	if err := s.ctrl.DeletePool(r.Context(), id); err != nil {
		s.fail(w, r, "deleting the pool", err)
		return
	}
	s.auth.Auditor().Deleted(r.Context(), Identity(r.Context()), "pool", id, poolForAudit(p))
	s.ctrl.Nudge()
	writeJSON(w, http.StatusOK, deletePoolResponse{RunnersAffected: affected})
}

// handleEnablePool and handleDisablePool are the two halves of the switch on
// the pool page. Disabling never interrupts a running job: the scheduler stops
// creating runners and drains the idle ones.
func (s *Server) handleEnablePool(w http.ResponseWriter, r *http.Request) {
	s.setPoolEnabled(w, r, true)
}

func (s *Server) handleDisablePool(w http.ResponseWriter, r *http.Request) {
	s.setPoolEnabled(w, r, false)
}

func (s *Server) setPoolEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	id := chiURLParam(r, "id")
	p, err := s.ctrl.Store().GetPool(r.Context(), id)
	if err != nil {
		s.fail(w, r, "reading the pool", err)
		return
	}
	if p.Enabled != enabled {
		before := *p
		p.Enabled = enabled
		if err := s.ctrl.Store().UpdatePool(r.Context(), p); err != nil {
			s.fail(w, r, "saving the pool", err)
			return
		}
		action := "pool.disable"
		if enabled {
			action = "pool.enable"
		}
		s.auth.Auditor().Act(r.Context(), Identity(r.Context()), action, "pool", id, map[string]any{
			"name": p.Name, "enabled": enabled, "was": before.Enabled,
		})
		s.ctrl.PublishPool(r.Context(), events.KindPoolUpdated, p)
		s.ctrl.Nudge()
	}

	view, verr := s.ctrl.PoolRenderer(r.Context())
	if verr != nil {
		s.internal(w, r, "reading the pool back", verr)
		return
	}
	writeJSON(w, http.StatusOK, poolFor(r, view.View(p)))
}

// emptySlice and emptyMap keep a JSON response from carrying null where the UI
// expects a collection.
func emptySlice[T any](in []T) []T {
	if in == nil {
		return []T{}
	}
	return in
}

func emptyMap(in store.StringMap) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	return in
}

// runnerCount renders a count of runners for a sentence an operator reads.
func runnerCount(n int) string {
	if n == 1 {
		return "1 runner"
	}
	return fmt.Sprintf("%d runners", n)
}
