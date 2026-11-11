package backend

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// A runner image that moves (a :dev or :latest tag, or any tag an operator
// republishes) leaves the image it used to name behind with no tag. Nothing
// refers to it again, and at about two gigabytes a time on a host that runs
// several pools it fills a disk in days. The agent pulled those images, so it
// removes the ones its own pulls superseded, and nothing else.
//
// The boundary is a conjunction, and every part of it is checked on the
// daemon's own answer rather than remembered:
//
//   - the image has no tag at all (the daemon's "dangling" filter, checked
//     again here in case a runtime ignores it);
//   - every repository digest it carries belongs to a repository this agent has
//     prepared a runner, a sidecar or a worker from since it started, so a
//     stranger's untagged image is never ours, and neither is one built here;
//   - no digest it carries was handed to a runner in the last imageHandoff,
//     because a create resolves a reference to a digest and then starts a
//     container from it, and an image removed in that gap fails the create;
//   - the daemon agrees it is unused: a delete without force is refused, with a
//     409, for an image any container uses, running or stopped.
//
// It is memory only and starts empty, so an agent that has only just started,
// or has not prepared an image yet, removes nothing.

// imageHandoff is how long a digest handed to a runner is kept out of
// SweepImages, and how long a fresh agent waits before it sweeps at all, so
// that a create in flight when the agent stopped has either finished or died.
const imageHandoff = 10 * time.Minute

// imageLedger is what the agent remembers about the images it has prepared.
type imageLedger struct {
	mu sync.Mutex
	// repos is every repository an image was prepared from, in the daemon's
	// canonical spelling.
	repos map[string]struct{}
	// handed is when each manifest digest was last handed to a create.
	handed map[string]time.Time
}

// noteImage records that a runner, sidecar or worker is about to be created from
// ref: its repository becomes one the sweep may clear superseded images from,
// and its digest is kept out of the sweep for imageHandoff. The digest is best
// effort (the caller has usually just resolved it), because a daemon that
// cannot say only makes the sweep wait for the tag to settle.
func (b *DockerBackend) noteImage(ref, digest string) {
	name, _ := splitImageRef(ref)
	repo := canonicalRepository(name)
	if repo == "" {
		return
	}
	l := &b.images
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.repos == nil {
		l.repos = map[string]struct{}{}
		l.handed = map[string]time.Time{}
	}
	l.repos[repo] = struct{}{}
	if digest = strings.TrimSpace(digest); digest != "" {
		l.handed[digest] = time.Now()
	}
}

// canonicalRepository spells a repository the way the daemon's listings do, so
// "docker.io/library/postgres", "library/postgres" and "postgres" are one.
// Registries other than Docker Hub are left exactly as written.
func canonicalRepository(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, hub := range []string{"index.docker.io/", "registry-1.docker.io/", "docker.io/"} {
		name = strings.TrimPrefix(name, hub)
	}
	return strings.TrimPrefix(name, "library/")
}

// SweepImages removes the untagged images this agent's own pulls superseded and
// reports how many it removed. See the comment above imageHandoff for the
// whole of what it will and will not touch.
func (b *DockerBackend) SweepImages(ctx context.Context) (int, error) {
	l := &b.images
	l.mu.Lock()
	repos := make(map[string]struct{}, len(l.repos))
	for r := range l.repos {
		repos[r] = struct{}{}
	}
	handed := make(map[string]time.Time, len(l.handed))
	for d, at := range l.handed {
		handed[d] = at
	}
	l.mu.Unlock()
	if len(repos) == 0 || (!b.imagesSince.IsZero() && time.Since(b.imagesSince) < imageHandoff) {
		return 0, nil
	}

	images, err := b.api.ImageList(ctx, map[string][]string{"dangling": {"true"}})
	if err != nil {
		return 0, fmt.Errorf("listing untagged images: %w", err)
	}
	removed := 0
	for _, img := range images {
		if ctx.Err() != nil {
			return removed, ctx.Err()
		}
		repo, ok := supersededImageRepository(img, repos, handed)
		if !ok {
			continue
		}
		err := b.api.ImageRemove(ctx, img.ID)
		switch {
		case err == nil:
			removed++
			b.log.Info("removed a superseded runner image", "image", shortID(img.ID), "repository", repo)
		case StatusCode(err) == http.StatusConflict, StatusCode(err) == http.StatusNotFound:
			// In use by a container, or gone already: not ours to force.
		default:
			b.log.Warn("could not remove a superseded runner image; will retry",
				"image", shortID(img.ID), "repository", repo, "error", err)
		}
	}
	return removed, nil
}

// supersededImageRepository reports whether img is one the sweep may remove, and
// the repository it came from.
func supersededImageRepository(img ImageSummary, repos map[string]struct{}, handed map[string]time.Time) (string, bool) {
	if img.ID == "" || len(img.RepoDigests) == 0 {
		return "", false
	}
	for _, tag := range img.RepoTags {
		if tag != "" && tag != "<none>:<none>" {
			return "", false
		}
	}
	repo := ""
	for _, rd := range img.RepoDigests {
		name, digest, ok := strings.Cut(rd, "@")
		if !ok {
			return "", false
		}
		canon := canonicalRepository(name)
		if _, ours := repos[canon]; !ours {
			return "", false
		}
		if at, ok := handed[digest]; ok && time.Since(at) < imageHandoff {
			return "", false
		}
		repo = canon
	}
	return repo, repo != ""
}
