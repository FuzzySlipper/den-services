package timeline

import (
	"context"
	"sort"
	"time"
)

type TimelineStore interface {
	Ping(ctx context.Context) error
	ListItems(ctx context.Context, query ListItemsQuery) ([]TimelineItem, error)
}

type Service struct {
	store  TimelineStore
	clock  func() time.Time
	config *Config
}

func NewService(store TimelineStore, clock func() time.Time, config *Config) *Service {
	return &Service{
		store:  store,
		clock:  clock,
		config: config,
	}
}

func (s *Service) CheckStore(ctx context.Context) error {
	return s.store.Ping(ctx)
}

func (s *Service) ListItems(ctx context.Context, scope TimelineScope, afterRaw string, limit int, includeDebug bool) (TimelineResponse, error) {
	if err := scope.Validate(); err != nil {
		return TimelineResponse{}, badRequest(err)
	}
	if limit == 0 {
		limit = s.config.DefaultLimit
	}
	if limit <= 0 || limit > s.config.MaxLimit {
		return TimelineResponse{}, badRequest(ErrInvalidLimit)
	}
	after, err := DecodeCursor(afterRaw)
	if err != nil {
		return TimelineResponse{}, codedBadRequest("invalid_cursor", err.Error())
	}
	items, err := s.store.ListItems(ctx, ListItemsQuery{
		Scope:        scope,
		After:        after,
		Limit:        limit,
		IncludeDebug: includeDebug,
	})
	if err != nil {
		return TimelineResponse{}, err
	}
	items = orderAndLimitItems(items, after, limit)
	response, err := toTimelineResponse(scope, items, s.clock())
	if err != nil {
		return TimelineResponse{}, err
	}
	return response, nil
}

func orderAndLimitItems(items []TimelineItem, after *TimelineCursor, limit int) []TimelineItem {
	filtered := make([]TimelineItem, 0, len(items))
	for _, item := range items {
		if after != nil && !itemAfterCursor(item, *after) {
			continue
		}
		filtered = append(filtered, item)
	}
	sort.SliceStable(filtered, func(left int, right int) bool {
		return compareTimelineItems(filtered[left], filtered[right]) < 0
	})
	if len(filtered) <= limit {
		return filtered
	}
	if after == nil {
		return append([]TimelineItem(nil), filtered[len(filtered)-limit:]...)
	}
	return append([]TimelineItem(nil), filtered[:limit]...)
}

func itemAfterCursor(item TimelineItem, cursor TimelineCursor) bool {
	if item.OccurredAt.After(cursor.OccurredAt) {
		return true
	}
	if item.OccurredAt.Before(cursor.OccurredAt) {
		return false
	}
	itemRank := sourceRank(item.SourceCursor)
	cursorRank := cursor.SourceRank()
	if itemRank != cursorRank {
		return itemRank > cursorRank
	}
	return item.SourceNumericID > cursor.ID
}

func compareTimelineItems(left TimelineItem, right TimelineItem) int {
	if left.OccurredAt.Before(right.OccurredAt) {
		return -1
	}
	if left.OccurredAt.After(right.OccurredAt) {
		return 1
	}
	leftRank := sourceRank(left.SourceCursor)
	rightRank := sourceRank(right.SourceCursor)
	if leftRank < rightRank {
		return -1
	}
	if leftRank > rightRank {
		return 1
	}
	if left.SourceNumericID < right.SourceNumericID {
		return -1
	}
	if left.SourceNumericID > right.SourceNumericID {
		return 1
	}
	return 0
}
