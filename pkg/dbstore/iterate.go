package dbstore

import (
	"context"
	"fmt"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

func ForEach[Model any](ctx context.Context, store Store, q manifest.SearchQuery, handler func(Model) error, options ...Option) (int64, error) {
	var totalProcessed int64

	for {
		var batch []Model
		total, err := store.Find(ctx, &batch, q, options...)
		if err != nil {
			return totalProcessed, fmt.Errorf("failed to load a batch from the store: %w", err)
		}

		for _, item := range batch {
			if err := handler(item); err != nil {
				return totalProcessed, err
			}
		}

		q.Offset += uint(len(batch))
		if len(batch) == 0 || q.Offset >= uint(total) || len(batch) < int(q.Limit) {
			break
		}
	}

	return totalProcessed, nil
}
