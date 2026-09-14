package provider

import "testing"

// The suite is the definition of done for a provider, so the provider this
// repository ships with it has to pass it -- and pass it with a capability
// taken away, because the "no PowerController" path is one a real provider will
// take and a suite that only ever saw the full-featured fake would never have
// exercised the type assertion at all.
func TestTheFakeObeysTheContractItPolices(t *testing.T) {
	RunContractTests(t, "fake", func(t *testing.T) Provider {
		return NewFake()
	})
	RunContractTests(t, "fake that cannot stop a machine", func(t *testing.T) Provider {
		return NewFake(FakeWithoutPower())
	})
	RunContractTests(t, "fake with no optional capabilities at all", func(t *testing.T) Provider {
		return NewFake(FakeWithoutPower(), FakeWithoutBootstrap(), FakeWithoutDiscovery())
	})
}
