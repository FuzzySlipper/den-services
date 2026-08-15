package main

import (
	"context"
	"flag"
	"fmt"
	"io"
)

type boardFlags struct {
	project *string
	postID  *int64
	comment *int64
	parent  *int64
	afterID *int64
	limit   *int
	title   *string
	body    *string
	author  *string
	actor   *string
	reason  *string
	query   *string
	json    *bool
}

func runBoardCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return boardUsageError(stderr, "a Board subcommand is required")
	}
	flags, err := parseBoardFlags(args[0], args[1:])
	if err != nil {
		return boardUsageError(stderr, err.Error())
	}
	client, err := BoardClientFromEnv()
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	body, err := executeBoardCommand(context.Background(), client, args[0], flags)
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	if *flags.json {
		if _, err := stdout.Write(body); err != nil {
			return writeRuntimeError(stderr, fmt.Errorf("write board response: %w", err))
		}
		if len(body) == 0 || body[len(body)-1] != '\n' {
			fmt.Fprintln(stdout)
		}
		return 0
	}
	return writeHumanServiceJSON(stdout, body)
}

func parseBoardFlags(command string, args []string) (boardFlags, error) {
	allowed := allowedBoardFlags(command)
	if allowed == nil {
		return boardFlags{}, fmt.Errorf("unknown Board subcommand %q", command)
	}
	set := flag.NewFlagSet("board "+command, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	flags := boardFlags{
		project: set.String("project", "", "Board project id"),
		postID:  set.Int64("post-id", 0, "Board post id"),
		comment: set.Int64("comment-id", 0, "Board comment id"),
		parent:  set.Int64("parent-comment-id", 0, "Immediate parent comment id"),
		afterID: set.Int64("after-id", -1, "Exclusive page cursor"),
		limit:   set.Int("limit", -1, "Bounded result limit"),
		title:   set.String("title", "", "Post title"),
		body:    set.String("body", "", "Markdown body"),
		author:  set.String("author", "", "Visible author identity"),
		actor:   set.String("actor", "", "Moderation actor identity"),
		reason:  set.String("reason", "", "Moderation reason"),
		query:   set.String("query", "", "Search query"),
		json:    set.Bool("json", false, "Print service JSON"),
	}
	if err := set.Parse(args); err != nil {
		return boardFlags{}, fmt.Errorf("invalid board %s flags: %w", command, err)
	}
	if set.NArg() != 0 {
		return boardFlags{}, fmt.Errorf("board %s does not accept positional arguments", command)
	}
	var unexpected string
	set.Visit(func(value *flag.Flag) {
		if value.Name != "json" && !allowed[value.Name] && unexpected == "" {
			unexpected = value.Name
		}
	})
	if unexpected != "" {
		return boardFlags{}, fmt.Errorf("board %s does not accept --%s", command, unexpected)
	}
	if *flags.afterID < -1 || *flags.limit == 0 || *flags.limit < -1 || *flags.limit > 100 || *flags.parent < 0 {
		return boardFlags{}, fmt.Errorf("board %s has an invalid cursor, limit, or parent id", command)
	}
	return flags, nil
}

func allowedBoardFlags(command string) map[string]bool {
	switch command {
	case "create-post":
		return map[string]bool{"project": true, "title": true, "body": true, "author": true}
	case "list-posts":
		return map[string]bool{"project": true, "after-id": true, "limit": true}
	case "get-post":
		return map[string]bool{"post-id": true}
	case "search":
		return map[string]bool{"project": true, "query": true, "after-id": true, "limit": true}
	case "create-comment":
		return map[string]bool{"post-id": true, "parent-comment-id": true, "body": true, "author": true}
	case "list-comments":
		return map[string]bool{"post-id": true, "parent-comment-id": true, "after-id": true, "limit": true}
	case "get-comment":
		return map[string]bool{"comment-id": true}
	case "comment-path":
		return map[string]bool{"comment-id": true, "limit": true}
	case "purge-post":
		return map[string]bool{"post-id": true, "actor": true, "reason": true}
	case "purge-comment":
		return map[string]bool{"comment-id": true, "actor": true, "reason": true}
	default:
		return nil
	}
}

func executeBoardCommand(ctx context.Context, client *BoardClient, command string, flags boardFlags) ([]byte, error) {
	afterID := optionalInt64Flag(flags.afterID)
	limit := optionalIntFlag(flags.limit)
	parent := positiveInt64Flag(flags.parent)
	switch command {
	case "create-post":
		return client.CreatePost(ctx, BoardCreatePostOptions{ProjectID: *flags.project, Title: *flags.title, BodyMarkdown: *flags.body, AuthorIdentity: *flags.author})
	case "list-posts":
		return client.ListPosts(ctx, BoardListPostsOptions{ProjectID: *flags.project, AfterID: afterID, Limit: limit})
	case "get-post":
		return client.GetPost(ctx, *flags.postID)
	case "search":
		return client.Search(ctx, BoardSearchOptions{ProjectID: *flags.project, Query: *flags.query, AfterID: afterID, Limit: limit})
	case "create-comment":
		return client.CreateComment(ctx, BoardCreateCommentOptions{PostID: *flags.postID, ParentCommentID: parent, BodyMarkdown: *flags.body, AuthorIdentity: *flags.author})
	case "list-comments":
		return client.ListComments(ctx, BoardListCommentsOptions{PostID: *flags.postID, ParentCommentID: parent, AfterID: afterID, Limit: limit})
	case "get-comment":
		return client.GetComment(ctx, *flags.comment)
	case "comment-path":
		return client.GetCommentPath(ctx, *flags.comment, limit)
	case "purge-post":
		return client.PurgePost(ctx, BoardPurgeOptions{ID: *flags.postID, ActorIdentity: *flags.actor, Reason: *flags.reason})
	case "purge-comment":
		return client.PurgeComment(ctx, BoardPurgeOptions{ID: *flags.comment, ActorIdentity: *flags.actor, Reason: *flags.reason})
	default:
		return nil, fmt.Errorf("unknown Board subcommand %q", command)
	}
}

func optionalInt64Flag(value *int64) *int64 {
	if *value < 0 {
		return nil
	}
	return value
}

func positiveInt64Flag(value *int64) *int64 {
	if *value <= 0 {
		return nil
	}
	return value
}

func optionalIntFlag(value *int) *int {
	if *value < 0 {
		return nil
	}
	return value
}

func boardUsageError(stderr io.Writer, message string) int {
	fmt.Fprintf(stderr, "den-tool: %s\nBoard subcommands: create-post, list-posts, get-post, search, create-comment, list-comments, get-comment, comment-path, purge-post, purge-comment\n", message)
	return 2
}
