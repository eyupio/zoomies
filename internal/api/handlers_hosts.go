package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/store"
	"github.com/eyupio/zoomies/internal/version"
)

// hostResponse is the shape GET /hosts returns, rendered by the controller
// so the event stream's host.updated frames are the same JSON. See
// controller/views.go for why the renderer lives there.
type hostResponse = controller.HostView

// backendInfoResponse is one entry of hostResponse.backend_info.
type backendInfoResponse = controller.BackendInfoView

// handleListHosts answers GET /api/v1/hosts.
func (s *Server) handleListHosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := s.ctrl.Store().ListHosts(r.Context())
	if err != nil {
		s.internal(w, r, "listing hosts", err)
		return
	}
	out := make([]hostResponse, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, s.ctrl.HostView(h))
	}
	writeJSON(w, http.StatusOK, newList(out))
}

// handleGetHost answers GET /api/v1/hosts/{id}.
func (s *Server) handleGetHost(w http.ResponseWriter, r *http.Request) {
	h, err := s.ctrl.Store().GetHost(r.Context(), chiURLParam(r, "id"))
	if err != nil {
		s.fail(w, r, "reading the host", err)
		return
	}
	writeJSON(w, http.StatusOK, s.ctrl.HostView(h))
}

type hostUpdateRequest struct {
	Capacity *int               `json:"capacity"`
	Labels   *map[string]string `json:"labels"`
	// The reserve: what this machine keeps for itself, in the units its own
	// figures are reported in. Each is optional and each is separate -- an
	// operator who holds back memory has said nothing about CPU.
	ReserveCPUs     *int   `json:"reserve_cpus"`
	ReserveMemoryMB *int64 `json:"reserve_memory_mb"`
	ReserveDiskMB   *int64 `json:"reserve_disk_mb"`
}

// handleUpdateHost changes a host's capacity, labels or reserve.
func (s *Server) handleUpdateHost(w http.ResponseWriter, r *http.Request) {
	id := chiURLParam(r, "id")
	h, err := s.ctrl.Store().GetHost(r.Context(), id)
	if err != nil {
		s.fail(w, r, "reading the host", err)
		return
	}

	var req hostUpdateRequest
	if !decode(w, r, &req) {
		return
	}
	var fields []fieldError
	if req.Capacity != nil {
		switch {
		case *req.Capacity < 0:
			fields = append(fields, fieldError{"capacity", "capacity cannot be negative; use 0 to stop this host taking new runners"})
		case *req.Capacity < h.ActiveRunners:
			// Allowed, and worth saying plainly rather than refusing: the
			// running jobs finish, and nothing new is placed until the host is
			// back under its new capacity.
			s.logger(r).Info("a host's capacity was lowered below the number of runners it is already running",
				"host", h.ID, "capacity", *req.Capacity, "active", h.ActiveRunners)
		}
	}
	if req.Labels != nil {
		for k := range *req.Labels {
			if strings.TrimSpace(k) == "" {
				fields = append(fields, fieldError{"labels", "a label needs a name"})
				break
			}
		}
	}
	// A reserve larger than the machine is refused rather than clamped: it
	// leaves nothing placeable, and an operator who typed megabytes where they
	// meant gigabytes should be told, not quietly obeyed. A reserve on a
	// figure the host has never reported is refused for the same reason --
	// there is nothing to hold back a share of, and the scheduler would ignore
	// it while the page showed it.
	if req.ReserveCPUs != nil {
		switch {
		case *req.ReserveCPUs < 0:
			fields = append(fields, fieldError{"reserve_cpus", "a reserve cannot be negative"})
		case h.CPUs <= 0 && *req.ReserveCPUs > 0:
			fields = append(fields, fieldError{"reserve_cpus", "this host has not reported how many CPUs it has, so there is nothing to hold back; upgrade its agent"})
		case h.CPUs > 0 && *req.ReserveCPUs >= h.CPUs:
			fields = append(fields, fieldError{"reserve_cpus", fmt.Sprintf("this host has %d CPUs; a reserve of %d would leave nothing to place on", h.CPUs, *req.ReserveCPUs)})
		}
	}
	if req.ReserveMemoryMB != nil {
		switch {
		case *req.ReserveMemoryMB < 0:
			fields = append(fields, fieldError{"reserve_memory_mb", "a reserve cannot be negative"})
		case h.MemoryMB <= 0 && *req.ReserveMemoryMB > 0:
			fields = append(fields, fieldError{"reserve_memory_mb", "this host has not reported how much memory it has, so there is nothing to hold back; upgrade its agent"})
		case h.MemoryMB > 0 && *req.ReserveMemoryMB >= h.MemoryMB:
			fields = append(fields, fieldError{"reserve_memory_mb", fmt.Sprintf("this host has %d MB of memory; a reserve of %d would leave nothing to place on", h.MemoryMB, *req.ReserveMemoryMB)})
		}
	}
	if req.ReserveDiskMB != nil {
		switch {
		case *req.ReserveDiskMB < 0:
			fields = append(fields, fieldError{"reserve_disk_mb", "a reserve cannot be negative"})
		case h.DiskTotalMB <= 0 && *req.ReserveDiskMB > 0:
			fields = append(fields, fieldError{"reserve_disk_mb", "this host has not reported its work directory's disk, so there is nothing to hold back; upgrade its agent"})
		case h.DiskTotalMB > 0 && *req.ReserveDiskMB >= h.DiskTotalMB:
			fields = append(fields, fieldError{"reserve_disk_mb", fmt.Sprintf("this host's work directory has %d MB of disk; a reserve of %d would leave nothing to place on", h.DiskTotalMB, *req.ReserveDiskMB)})
		}
	}
	if len(fields) > 0 {
		unprocessable(w, "this host cannot be changed as described", fields)
		return
	}

	before := *h
	changes := store.HostChanges{
		Capacity: req.Capacity, ReserveCPUs: req.ReserveCPUs,
		ReserveMemoryMB: req.ReserveMemoryMB, ReserveDiskMB: req.ReserveDiskMB,
	}
	if req.Labels != nil {
		labels := store.StringMap(*req.Labels)
		changes.Labels = &labels
	}
	if err := s.ctrl.Store().PatchHost(r.Context(), id, changes); err != nil {
		s.fail(w, r, "saving the host", err)
		return
	}
	h, err = s.ctrl.Store().GetHost(r.Context(), id)
	if err != nil {
		s.fail(w, r, "reading the host back", err)
		return
	}

	s.auth.Auditor().Updated(r.Context(), Identity(r.Context()), "host", id, &before, h)
	s.ctrl.PublishHost(h)
	// Capacity, labels and the reserve all decide where runners may be placed.
	s.ctrl.Nudge()
	writeJSON(w, http.StatusOK, s.ctrl.HostView(h))
}

type cordonRequest struct {
	Cordoned bool `json:"cordoned"`
}

// handleCordonHost cordons or uncordons a host.
//
// A cordoned host keeps everything it is already running: cordoning is how an
// operator drains a machine before maintenance without interrupting the jobs on
// it, and killing them would defeat the purpose.
func (s *Server) handleCordonHost(w http.ResponseWriter, r *http.Request) {
	id := chiURLParam(r, "id")
	h, err := s.ctrl.Store().GetHost(r.Context(), id)
	if err != nil {
		s.fail(w, r, "reading the host", err)
		return
	}

	var req cordonRequest
	if !decode(w, r, &req) {
		return
	}
	if h.Cordoned != req.Cordoned {
		if err := s.ctrl.Store().SetHostCordoned(r.Context(), id, req.Cordoned); err != nil {
			s.fail(w, r, "cordoning the host", err)
			return
		}
		h.Cordoned = req.Cordoned
		action := "host.uncordon"
		if req.Cordoned {
			action = "host.cordon"
		}
		s.auth.Auditor().Act(r.Context(), Identity(r.Context()), action, "host", id, map[string]any{
			"name": h.Name, "cordoned": req.Cordoned, "active_runners": h.ActiveRunners,
		})
		s.ctrl.PublishHost(h)
		s.ctrl.Nudge()
	}
	writeJSON(w, http.StatusOK, s.ctrl.HostView(h))
}

// handleDeleteHost removes a host.
//
// It refuses while the host still has live runners unless forced, because
// deleting the row cascades to those runners and their workloads would be left
// running on a machine Zoomies no longer knows about.
func (s *Server) handleDeleteHost(w http.ResponseWriter, r *http.Request) {
	id := chiURLParam(r, "id")
	force := queryBool(r, "force", false)

	h, err := s.ctrl.Store().GetHost(r.Context(), id)
	if err != nil {
		s.fail(w, r, "reading the host", err)
		return
	}
	live, err := s.ctrl.Store().ListRunnersForHost(r.Context(), id)
	if err != nil {
		s.internal(w, r, "listing the host's runners", err)
		return
	}
	alive := 0
	for _, run := range live {
		if !run.State.Terminal() {
			alive++
		}
	}
	if alive > 0 && !force {
		conflict(w, fmt.Sprintf("host %s is still running %d runner(s); cordon it and wait for them to finish, or repeat this with ?force=true to remove it and leave those workloads to be cleaned up by their agent", h.Name, alive))
		return
	}

	if err := s.ctrl.DeleteHost(r.Context(), id); err != nil {
		s.fail(w, r, "deleting the host", err)
		return
	}
	s.auth.Auditor().Deleted(r.Context(), Identity(r.Context()), "host", id, h)
	s.ctrl.Nudge()
	noContent(w)
}

// ---------------------------------------------------------------------------
// Join tokens
// ---------------------------------------------------------------------------

// joinTokenResponse never carries the secret. The prefix is enough to recognise
// a token in a list and to match a leaked string to a row.
type joinTokenResponse struct {
	ID        string            `json:"id"`
	Prefix    string            `json:"prefix"`
	CreatedBy string            `json:"created_by,omitempty"`
	Capacity  int               `json:"capacity"`
	Labels    map[string]string `json:"labels"`
	CreatedAt time.Time         `json:"created_at"`
	ExpiresAt time.Time         `json:"expires_at"`
	UsedAt    *time.Time        `json:"used_at"`
	UsedByID  string            `json:"used_by_id,omitempty"`
	Usable    bool              `json:"usable"`
}

// createJoinTokenResponse adds the things that exist exactly once: the
// plaintext token, the command to paste on the new host, and -- when the
// command could not be pinned to this controller's build -- what that will
// cost the operator.
type createJoinTokenResponse struct {
	joinTokenResponse
	Token             string `json:"token"`
	Command           string `json:"command"`
	JoinCommand       string `json:"join_command"`
	ControllerVersion string `json:"controller_version"`
	InstallTag        string `json:"install_tag,omitempty"`
	// VersionNote is empty when the command installs a matching agent.
	VersionNote string `json:"version_note,omitempty"`
}

func (s *Server) joinTokenResponse(t *store.JoinToken) joinTokenResponse {
	return joinTokenResponse{
		ID:        t.ID,
		Prefix:    t.Prefix,
		CreatedBy: t.CreatedBy,
		Capacity:  t.Capacity,
		Labels:    emptyMap(t.Labels),
		CreatedAt: t.CreatedAt,
		ExpiresAt: t.ExpiresAt,
		UsedAt:    t.UsedAt,
		UsedByID:  t.UsedByID,
		Usable:    t.Usable(s.ctrl.Now()),
	}
}

// handleListJoinTokens answers GET /api/v1/join-tokens.
func (s *Server) handleListJoinTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.ctrl.Store().ListJoinTokens(r.Context())
	if err != nil {
		s.internal(w, r, "listing join tokens", err)
		return
	}
	out := make([]joinTokenResponse, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, s.joinTokenResponse(t))
	}
	writeJSON(w, http.StatusOK, newList(out))
}

type createJoinTokenRequest struct {
	TTL      string            `json:"ttl"`
	Capacity int               `json:"capacity"`
	Labels   map[string]string `json:"labels"`
	// ControllerURL is the address the new host should join on, when the
	// caller knows better than server.external_url does. The UI always does:
	// the browser reached this controller on some address, and a machine on
	// the same network will usually reach it there too.
	ControllerURL string `json:"controller_url"`
	Connection    string `json:"connection"`
}

// handleCreateJoinToken mints a single-use enrolment credential.
func (s *Server) handleCreateJoinToken(w http.ResponseWriter, r *http.Request) {
	req := createJoinTokenRequest{TTL: "15m", Capacity: 2}
	if !decodeOptional(w, r, &req) {
		return
	}

	var fields []fieldError
	ttl := 15 * time.Minute
	if raw := strings.TrimSpace(req.TTL); raw != "" {
		d, err := time.ParseDuration(raw)
		switch {
		case err != nil:
			fields = append(fields, fieldError{"ttl", fmt.Sprintf("%q is not a duration; write it like 15m, 1h or 24h", raw)})
		case d <= 0:
			fields = append(fields, fieldError{"ttl", "a join token has to be usable for some length of time; try 15m"})
		default:
			ttl = d
		}
	}
	if req.Capacity < 0 {
		fields = append(fields, fieldError{"capacity", "capacity cannot be negative; leave it at 0 to let the agent decide from the host's CPU count"})
	}
	if req.Connection != "" && req.Connection != "direct" && req.Connection != "tailcat" {
		fields = append(fields, fieldError{"connection", "choose direct or tailcat"})
	}
	controllerURL := strings.TrimRight(strings.TrimSpace(req.ControllerURL), "/")
	if controllerURL != "" {
		if msg := checkControllerURL(controllerURL); msg != "" {
			fields = append(fields, fieldError{"controller_url", msg})
		}
	}
	if len(fields) > 0 {
		unprocessable(w, "this join token could not be created", fields)
		return
	}

	if req.Connection == "tailcat" {
		address, err := s.ensureTailcat(r.Context())
		if err != nil {
			unprocessable(w, err.Error(), []fieldError{{"connection", err.Error()}})
			return
		}
		controllerURL = "tailcat://" + address
	}
	id := Identity(r.Context())
	createdBy := ""
	if id != nil {
		createdBy = id.Name
	}
	token, plaintext, err := s.auth.CreateJoinToken(r.Context(), ttl, req.Labels, req.Capacity, createdBy)
	if err != nil {
		s.internal(w, r, "creating the join token", err)
		return
	}
	// The plaintext is deliberately not in the audit row: the row records that
	// a credential was minted, not the credential.
	s.auth.Auditor().Created(r.Context(), id, "join_token", token.ID, token)

	writeJSON(w, http.StatusCreated, createJoinTokenResponse{
		joinTokenResponse: s.joinTokenResponse(token),
		Token:             plaintext,
		Command:           s.joinCommand(plaintext, controllerURL),
		JoinCommand:       "zoomies agent join " + shellArgument(s.joinControllerURL(controllerURL)) + " --token " + shellArgument(plaintext),
		ControllerVersion: version.Short(),
		InstallTag:        version.Channel(version.Version),
		VersionNote:       joinVersionNote(),
	})
}

// checkControllerURL says what is wrong with an address a host is being told
// to join, or nothing. The bar is "an agent could dial it": an absolute
// http(s) URL with a host in it. Loopback is allowed on purpose -- an operator
// enrolling a second agent on the controller's own machine means it -- and
// the UI is what warns about it, since the UI knows which machine it is on.
func checkControllerURL(raw string) string {
	u, err := url.Parse(raw)
	switch {
	case err != nil:
		return fmt.Sprintf("%q is not a URL an agent could join; write it like https://zoomies.example.com", raw)
	case u.Scheme != "http" && u.Scheme != "https":
		return fmt.Sprintf("the controller address has to start with http:// or https://, not %q", u.Scheme+"://")
	case u.Host == "":
		return "the controller address needs a host name or IP address, like https://zoomies.example.com"
	case u.User != nil:
		return "the controller address must not carry a username or password; the join token is the credential"
	}
	return ""
}

// joinCommand renders the one-liner for the new host.
//
// The caller's address wins when it gave one. Otherwise the command names the
// controller's external URL, because that is the address the agent has to
// reach; when that is not configured either the command is still printed,
// with the placeholder in it, since an operator who has not set it yet needs
// to see what is missing rather than a blank field.
func (s *Server) joinControllerURL(controllerURL string) string {
	controller := controllerURL
	if controller == "" {
		controller = s.cfg().Server.ExternalURL
		// A loopback external URL is as unusable here as no URL at all, and
		// worse for being plausible: the default single-VM install makes it
		// http://localhost:8080, so the command told the new machine to join
		// itself, and the operator found out after a download, a system
		// write and a spent single-use token. The placeholder makes the gap
		// visible, and the UI fills it in.
		if controller == "" || s.cfg().ExternalURLIsLocal() {
			controller = "https://<this-controller>"
		}
	}
	return controller
}

func (s *Server) joinCommand(token, controllerURL string) string {
	controller := s.joinControllerURL(controllerURL)
	cmd := fmt.Sprintf("curl -fsSL https://zoomies.sh/install.sh | sh -s -- --mode agent --controller %s --join-token %s",
		shellArgument(controller), shellArgument(token))

	// Pin the agent to this controller's own published channel.
	//
	// Unpinned, the installer resolves "latest", which is the newest *published
	// release* -- not this build. A fleet whose controller was upgraded past the
	// newest release therefore enrolled every new host on an older agent, and
	// the host arrived reading "Different build" for good: re-running the
	// command changed nothing, because the version it asks for is the version
	// already there.
	//
	// It is worse than a cosmetic badge when the older agent predates something.
	// An agent that cannot yet measure its own machine reports no CPUs, memory
	// or disk, so the host shows "Size unknown" and a pool's resource limits
	// have nothing to fit against -- and no argument to the command could have
	// fixed it.
	if tag, ok := version.InstallTag(version.Version); ok {
		return cmd + " --version " + tag
	}

	// No pin, because there is nothing honest to pin to. Local and fork builds
	// have no repository asset; the command still works and the note says what
	// it cannot promise instead.
	return cmd
}

// shellArgument keeps a URL or credential one literal argument when the
// operator pastes the command into a POSIX shell.
func shellArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// joinVersionNote explains a join command that could not be pinned, or is empty
// when it was.
//
// The command above stays runnable either way, so this is the difference
// between an operator who knows their new host will read "Different build" and
// one who finds out afterwards and starts re-running the installer to fix it.
func joinVersionNote() string {
	if _, ok := version.InstallTag(version.Version); ok {
		return ""
	}
	return fmt.Sprintf("This controller is %s, which is a local or unpublished build, "+
		"so there is no matching agent to install. The host will get the newest release and the "+
		"fleet will show it as a different build until this controller runs a released version.",
		version.Version)
}

// handleGetJoinToken answers GET /api/v1/join-tokens/{id}.
//
// It is what the Add-a-host page polls while the operator is over on the new
// machine: the fleet stream deliberately carries nothing about credentials, so
// a token being redeemed is learnt by asking, and used_by_id is the host it
// became.
func (s *Server) handleGetJoinToken(w http.ResponseWriter, r *http.Request) {
	t, err := s.ctrl.Store().GetJoinToken(r.Context(), chiURLParam(r, "id"))
	if err != nil {
		s.fail(w, r, "reading the join token", err)
		return
	}
	writeJSON(w, http.StatusOK, s.joinTokenResponse(t))
}

// handleDeleteJoinToken revokes an unused join token.
func (s *Server) handleDeleteJoinToken(w http.ResponseWriter, r *http.Request) {
	id := chiURLParam(r, "id")
	found, err := s.ctrl.Store().GetJoinToken(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			notFound(w, "there is no join token "+id+"; it may already have been used or revoked")
			return
		}
		s.internal(w, r, "reading the join token", err)
		return
	}
	if err := s.ctrl.Store().DeleteJoinToken(r.Context(), id); err != nil {
		s.fail(w, r, "revoking the join token", err)
		return
	}
	s.auth.Auditor().Deleted(r.Context(), Identity(r.Context()), "join_token", id, found)
	noContent(w)
}
