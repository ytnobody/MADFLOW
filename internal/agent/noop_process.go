package agent

import "context"

// noopProcess is a no-op implementation of Process used for testing.
// It returns empty responses immediately without invoking any external process.
type noopProcess struct{}

func (p *noopProcess) Send(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (p *noopProcess) Reset(_ context.Context) error {
	return nil
}

func (p *noopProcess) Close() error {
	return nil
}
