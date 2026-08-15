package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

type boardFlags struct {
	project   *string
	postID    *int64
	comment   *int64
	parent    *int64
	afterID   *int64
	limit     *int
	title     *string
	body      *string
	author    *string
	actor     *string
	reason    *string
	query     *string
	json      *bool
	parentSet bool
}

func runBoardCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return boardUsageError(stderr, "a Board subcommand is required")
	}
	flags, err := parseBoardFlags(args[0], args[1:])
	if err != nil {
		return boardUsageError(stderr, err.Error())
	}
	var body []byte
	if strings.TrimSpace(os.Getenv("DEN_BOARD_URL")) != "" {
		client, clientErr := BoardClientFromEnv()
		if clientErr != nil {
			return writeRuntimeError(stderr, clientErr)
		}
		body, err = executeBoardCommand(context.Background(), client, args[0], flags)
	} else {
		body, err = executeBoardMCPCommand(context.Background(), args[0], flags)
	}
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	if *flags.json {
		if _, err := stdout.Write(body); err != nil {
			return writeRuntimeError(stderr, fmt.Errorf("write board response: %w", err))
		}
		if len(body) == 0 || body[len(body)-1] != '\n' {
			_, _ = fmt.Fprintln(stdout)
		}
		return 0
	}
	return writeHumanServiceJSON(stdout, body)
}

func executeBoardMCPCommand(ctx context.Context, command string, flags boardFlags) ([]byte, error) {
	operation, arguments, err := boardMCPCall(command, flags)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(arguments)
	if err != nil {
		return nil, fmt.Errorf("encode Board MCP arguments: %w", err)
	}
	client, err := MCPClientFromEnv()
	if err != nil {
		return nil, err
	}
	return client.CallStructured(ctx, operation, payload)
}

func boardMCPCall(command string, flags boardFlags) (string, map[string]any, error) {
	arguments := make(map[string]any)
	putPageArguments(arguments, flags)
	switch command {
	case "create-post":
		if strings.TrimSpace(*flags.project) == "" || strings.TrimSpace(*flags.title) == "" || strings.TrimSpace(*flags.body) == "" || strings.TrimSpace(*flags.author) == "" {
			return "", nil, fmt.Errorf("board create-post requires project, title, body, and author")
		}
		arguments["project_id"], arguments["title"], arguments["body_markdown"], arguments["author_identity"] = *flags.project, *flags.title, *flags.body, *flags.author
		return "create_board_post", arguments, nil
	case "list-posts":
		if strings.TrimSpace(*flags.project) == "" {
			return "", nil, fmt.Errorf("board list-posts requires project")
		}
		arguments["project_id"] = *flags.project
		return "list_board_posts", arguments, nil
	case "get-post":
		if *flags.postID <= 0 {
			return "", nil, fmt.Errorf("board get-post requires positive post-id")
		}
		arguments["post_id"] = *flags.postID
		return "get_board_post", arguments, nil
	case "search":
		if strings.TrimSpace(*flags.project) == "" || strings.TrimSpace(*flags.query) == "" {
			return "", nil, fmt.Errorf("board search requires project and query")
		}
		arguments["project_id"], arguments["query"] = *flags.project, *flags.query
		return "search_board_posts", arguments, nil
	case "create-comment":
		if *flags.postID <= 0 || strings.TrimSpace(*flags.body) == "" || strings.TrimSpace(*flags.author) == "" {
			return "", nil, fmt.Errorf("board create-comment requires positive post-id, body, and author")
		}
		arguments["post_id"], arguments["body_markdown"], arguments["author_identity"] = *flags.postID, *flags.body, *flags.author
		if flags.parentSet {
			arguments["parent_comment_id"] = *flags.parent
		}
		return "create_board_comment", arguments, nil
	case "list-comments":
		if *flags.postID <= 0 {
			return "", nil, fmt.Errorf("board list-comments requires positive post-id")
		}
		arguments["post_id"] = *flags.postID
		if flags.parentSet {
			arguments["parent_comment_id"] = *flags.parent
		}
		return "list_board_comments", arguments, nil
	case "get-comment":
		if *flags.comment <= 0 {
			return "", nil, fmt.Errorf("board get-comment requires positive comment-id")
		}
		arguments["comment_id"] = *flags.comment
		return "get_board_comment", arguments, nil
	case "comment-path":
		if *flags.comment <= 0 {
			return "", nil, fmt.Errorf("board comment-path requires positive comment-id")
		}
		arguments["comment_id"] = *flags.comment
		return "get_board_comment_path", arguments, nil
	case "purge-post":
		if *flags.postID <= 0 || strings.TrimSpace(*flags.actor) == "" || strings.TrimSpace(*flags.reason) == "" {
			return "", nil, fmt.Errorf("board purge-post requires positive post-id, actor, and reason")
		}
		arguments["post_id"], arguments["actor_identity"], arguments["reason"] = *flags.postID, *flags.actor, *flags.reason
		return "purge_board_post", arguments, nil
	case "purge-comment":
		if *flags.comment <= 0 || strings.TrimSpace(*flags.actor) == "" || strings.TrimSpace(*flags.reason) == "" {
			return "", nil, fmt.Errorf("board purge-comment requires positive comment-id, actor, and reason")
		}
		arguments["comment_id"], arguments["actor_identity"], arguments["reason"] = *flags.comment, *flags.actor, *flags.reason
		return "purge_board_comment", arguments, nil
	default:
		return "", nil, fmt.Errorf("unknown Board subcommand %q", command)
	}
}

func putPageArguments(arguments map[string]any, flags boardFlags) {
	if afterID := optionalInt64Flag(flags.afterID); afterID != nil {
		arguments["after_id"] = *afterID
	}
	if limit := optionalIntFlag(flags.limit); limit != nil {
		arguments["limit"] = *limit
	}
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
		if value.Name == "parent-comment-id" {
			flags.parentSet = true
		}
		if value.Name != "json" && !allowed[value.Name] && unexpected == "" {
			unexpected = value.Name
		}
	})
	if unexpected != "" {
		return boardFlags{}, fmt.Errorf("board %s does not accept --%s", command, unexpected)
	}
	if *flags.afterID < -1 || *flags.limit == 0 || *flags.limit < -1 || *flags.limit > 100 || *flags.parent < 0 || (flags.parentSet && *flags.parent == 0) {
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
	_, _ = fmt.Fprintf(stderr, "den-tool: %s\nBoard subcommands: create-post, list-posts, get-post, search, create-comment, list-comments, get-comment, comment-path, purge-post, purge-comment\n", message)
	return 2
}
