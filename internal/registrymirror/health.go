package registrymirror

import "time"

// A mirror is only as good as the registry behind it, and one the registry is
// failing is worse than none, because every pull waits for it to fail before the
// daemon falls back. The mirror therefore times how long the registry has failed
// it without a break, and leaves it to the agent to decide when that is long
// enough to say so. A single failure is a blip; an unbroken run is a fault.

// upstreamError counts one thing the registry failed to give the mirror, and
// starts the unbroken run it is part of when there is not one already. The
// counter and the run are changed together so that they cannot disagree.
func (m *Mirror) upstreamError() {
	m.upstreamErrors.Add(1)
	m.healthMu.Lock()
	defer m.healthMu.Unlock()
	if m.failingSince.IsZero() {
		m.failingSince = m.cfg.Now()
	}
}

// upstreamAnswered ends the run. The registry gave an answer, and "no" is one: a
// private repository, an absent image and a tag that does not exist are the
// registry doing its job, and a mirror that counted them would call itself
// broken for serving a job that named an image wrongly.
func (m *Mirror) upstreamAnswered() {
	m.healthMu.Lock()
	defer m.healthMu.Unlock()
	m.failingSince = time.Time{}
}

// failingSeconds is the length of the run, in whole seconds, and zero when the
// last thing the mirror asked the registry for came back.
func (m *Mirror) failingSeconds() int64 {
	m.healthMu.Lock()
	since := m.failingSince
	m.healthMu.Unlock()
	if since.IsZero() {
		return 0
	}
	return max(int64(m.cfg.Now().Sub(since)/time.Second), 0)
}
