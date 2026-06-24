package ingest

import "context"

type Processor interface {
	Ingest(ctx context.Context, filePath, text, sourceSHA string) error
}
