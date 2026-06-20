package importer

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func Run(ctx context.Context, options Options, destination Destination) (*Report, error) {
	sourceName := strings.TrimSpace(options.SourceName)
	if sourceName == "" {
		sourceName = SourceLegacyDenChannels
	}
	data, exclusions, err := LoadSQLite(ctx, options.SourcePath, options.Limit)
	if err != nil {
		return nil, err
	}
	report := &Report{
		SourcePath: options.SourcePath,
		SourceName: sourceName,
		DryRun:     options.DryRun,
		Counts: ImportCounts{
			Channels:     len(data.Channels),
			Messages:     len(data.Messages),
			Memberships:  len(data.Memberships),
			Reactions:    len(data.Reactions),
			ReadCursors:  len(data.ReadCursors),
			ProjectLinks: len(data.ProjectLinks),
		},
		Exclusions: exclusions,
	}
	if options.DryRun {
		return report, nil
	}
	if destination == nil {
		return nil, errors.New("destination is required for real import")
	}

	channelIDs := map[int64]int64{}
	for _, channel := range data.Channels {
		channelID, err := destination.UpsertChannel(ctx, sourceName, channel)
		if err != nil {
			return nil, fmt.Errorf("importing legacy channel %d: %w", channel.ID, err)
		}
		channelIDs[channel.ID] = channelID
	}

	for _, link := range data.ProjectLinks {
		channelID, ok := channelIDs[link.ChannelID]
		if !ok {
			report.Exclusions.UnmappedProjectLinks++
			continue
		}
		if _, err := destination.UpsertProjectLink(ctx, sourceName, link, channelID); err != nil {
			return nil, fmt.Errorf("importing legacy project link %d: %w", link.ID, err)
		}
	}

	messageIDs := map[int64]int64{}
	messageChannels := map[int64]int64{}
	for _, message := range data.Messages {
		channelID, ok := channelIDs[message.ChannelID]
		if !ok {
			continue
		}
		messageID, err := destination.UpsertMessage(ctx, sourceName, message, channelID)
		if err != nil {
			return nil, fmt.Errorf("importing legacy message %d: %w", message.ID, err)
		}
		messageIDs[message.ID] = messageID
		messageChannels[message.ID] = channelID
	}
	for _, message := range data.Messages {
		if _, ok := messageIDs[message.ID]; !ok {
			continue
		}
		if err := destination.UpdateMessageReferences(ctx, sourceName, message); err != nil {
			return nil, fmt.Errorf("updating legacy message references %d: %w", message.ID, err)
		}
	}

	for _, membership := range data.Memberships {
		channelID, ok := channelIDs[membership.ChannelID]
		if !ok {
			continue
		}
		if _, err := destination.UpsertMembership(ctx, sourceName, membership, channelID); err != nil {
			return nil, fmt.Errorf("importing legacy membership %d: %w", membership.ID, err)
		}
	}

	for _, reaction := range data.Reactions {
		messageID, ok := messageIDs[reaction.MessageID]
		if !ok {
			report.Exclusions.UnmappedReactions++
			continue
		}
		channelID := messageChannels[reaction.MessageID]
		if _, err := destination.UpsertReaction(ctx, sourceName, reaction, messageID, channelID); err != nil {
			return nil, fmt.Errorf("importing legacy reaction %d: %w", reaction.ID, err)
		}
	}

	for _, cursor := range data.ReadCursors {
		channelID, ok := channelIDs[cursor.ChannelID]
		if !ok {
			report.Exclusions.UnmappedReadCursors++
			continue
		}
		var messageID *int64
		if cursor.LastReadMessageID != nil {
			if mapped, ok := messageIDs[*cursor.LastReadMessageID]; ok {
				messageID = &mapped
			} else {
				report.Exclusions.UnmappedReadCursors++
			}
		}
		if err := destination.UpsertReadCursor(ctx, sourceName, cursor, channelID, messageID); err != nil {
			return nil, fmt.Errorf("importing legacy read cursor %d: %w", cursor.ID, err)
		}
	}

	counts, err := destination.Counts(ctx)
	if err != nil {
		return nil, fmt.Errorf("counting destination: %w", err)
	}
	report.Destination = counts
	return report, nil
}
