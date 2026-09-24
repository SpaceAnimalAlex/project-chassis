package mailengine

import (
	"context"

	"github.com/project-chassis/chassis/internal/core"
	"github.com/project-chassis/chassis/internal/db"
)

// Ingestor implements core.InboundIngestor, providing an isolated ingestion engine
// for inbound communications (emails, voicemails, webhooks) that functions independently
// of the background mail polling worker.
type Ingestor struct {
	repo          *db.Repository
	threader      *Threader
	notifier      core.Notifier
	defaultPrefix string
}

// NewIngestor creates an Ingestor instance.
func NewIngestor(repo *db.Repository, threader *Threader, notifier core.Notifier, defaultPrefix string) *Ingestor {
	return &Ingestor{
		repo:          repo,
		threader:      threader,
		notifier:      notifier,
		defaultPrefix: defaultPrefix,
	}
}

// IngestInbound transactionally ingests an inbound message using the shared IngestInbound pipeline.
func (i *Ingestor) IngestInbound(ctx context.Context, msg core.InboundMessage) error {
	return IngestInbound(ctx, i.repo, i.threader, i.notifier, i.defaultPrefix, msg)
}

var _ core.InboundIngestor = (*Ingestor)(nil)
