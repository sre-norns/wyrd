package dbstore

import (
	"context"
	"fmt"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

// ForEach provides an easy way to iterate over all entries in the store that match search query provided.
func ForEach[Model any](ctx context.Context, store Store, q manifest.SearchQuery, handler func(Model) error, options ...Option) (processed int64, err error) {
	for {
		var batch []Model
		total, err := store.Find(ctx, &batch, q, options...)
		if err != nil {
			return total, fmt.Errorf("failed to load a batch from the store: %w", err)
		}

		for _, item := range batch {
			if err := handler(item); err != nil {
				return processed, err
			}
			processed += 1
		}

		q.Offset += uint(len(batch))
		if len(batch) == 0 || q.Offset >= uint(total) || len(batch) < int(q.Limit) {
			break
		}

		if err = ctx.Err(); err != nil {
			break
		}
	}

	return
}
