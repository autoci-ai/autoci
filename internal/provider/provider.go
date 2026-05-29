package provider

import (
	"context"

	"github.com/autoci-ai/autoci/internal/profile"
)

type Provider interface {
	Profile(ctx context.Context) (*profile.Profile, error)
}
